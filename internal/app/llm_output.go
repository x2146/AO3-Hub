package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// outputFormatError marks a reply that arrived intact over the wire but could
// not be used: malformed JSON, missing segments, an empty summary. Unlike an
// auth or quota failure, simply asking again usually fixes it, so the retry
// policy treats it as retryable.
type outputFormatError struct {
	err error
}

func (e *outputFormatError) Error() string { return "模型输出格式无效: " + e.err.Error() }
func (e *outputFormatError) Unwrap() error { return e.err }

func asOutputFormatError(err error) error {
	if err == nil {
		return nil
	}
	var formatErr *outputFormatError
	if errors.As(err, &formatErr) {
		return err
	}
	return &outputFormatError{err: err}
}

func isOutputFormatError(err error) bool {
	var formatErr *outputFormatError
	return errors.As(err, &formatErr)
}

// outputWarning is returned by an accept callback for a reply that was used
// but not fully: the call still counts as a success, and the warning is kept
// with the request sample so partial failures stay visible in stats.
type outputWarning struct {
	msg string
}

func (w *outputWarning) Error() string { return w.msg }

const maxFeedbackEchoBytes = 12000

// withFormatFeedback replays a rejected reply and tells the model why it was
// rejected, so the retry fixes the problem instead of rolling the dice again.
func withFormatFeedback(base []ChatMessage, previous string, err error, instruction string) []ChatMessage {
	out := make([]ChatMessage, 0, len(base)+2)
	out = append(out, base...)
	out = append(out,
		ChatMessage{Role: "assistant", Content: truncateUTF8(previous, maxFeedbackEchoBytes)},
		ChatMessage{Role: "user", Content: fmt.Sprintf("上面的输出无法被程序解析：%s\n%s", err.Error(), instruction)},
	)
	return out
}

func truncateUTF8(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

func stripJSONFences(content string) string {
	s := strings.TrimSpace(content)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimSpace(strings.TrimPrefix(s, "```json"))
		s = strings.TrimSpace(strings.TrimPrefix(s, "```"))
		s = strings.TrimSpace(strings.TrimSuffix(s, "```"))
	}
	return s
}

// decodeLenientJSON reads the first JSON object in a model reply. It tolerates
// what models commonly wrap around the object (fences, a lead-in sentence,
// trailing notes) and, when the strict parse fails, retries once on a copy
// with unescaped inner quotes and trailing commas repaired.
func decodeLenientJSON[T any](content string) (T, error) {
	var zero T
	s := stripJSONFences(content)
	// Start at the first brace that opens an object with a key, so a brace in
	// a lead-in sentence ("以下是结果 {注意}:") is skipped; the first such
	// brace is the outer object, never a nested one.
	first := strings.IndexByte(s, '{')
	if first < 0 {
		first = 0
	}
	starts := []int{first}
	for i := first; i < len(s); i++ {
		if s[i] == '{' {
			if j := skipJSONSpace(s, i+1); j < len(s) && s[j] == '"' {
				if i != first {
					// Try the keyed brace first so its error is the one reported.
					starts = []int{i, first}
				}
				break
			}
		}
	}
	var firstErr error
	var firstText string
	for _, start := range starts {
		out, _, err := decodeFirstJSONValue[T](s[start:])
		if err == nil {
			return out, nil
		}
		if firstErr == nil {
			firstErr, firstText = err, s[start:]
		}
	}
	for _, start := range starts {
		candidate := s[start:]
		repaired := repairModelJSON(candidate)
		if repaired == candidate {
			continue
		}
		out, end, err := decodeFirstJSONValue[T](repaired)
		// A repair that guessed a string end wrong yields a valid but cut-off
		// object followed by more JSON-looking text; reject that rather than
		// silently losing fields.
		if err == nil && !strings.ContainsAny(repaired[end:], `":`) {
			return out, nil
		}
	}
	return zero, describeJSONError(firstText, firstErr)
}

func decodeFirstJSONValue[T any](s string) (T, int, error) {
	var out T
	dec := json.NewDecoder(strings.NewReader(s))
	err := dec.Decode(&out)
	return out, int(dec.InputOffset()), err
}

// describeJSONError replaces encoding/json's syntax message, which prints a
// lone byte of a multi-byte character (invalid character 'ç'), with the
// readable text around the failure; the message is fed back to the model.
func describeJSONError(s string, err error) error {
	var syntaxErr *json.SyntaxError
	if !errors.As(err, &syntaxErr) {
		if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
			return errors.New("JSON 不完整（输出被截断或缺少结尾括号）")
		}
		return err
	}
	at := int(syntaxErr.Offset)
	if at > len(s) {
		at = len(s)
	}
	from := at - 60
	if from < 0 {
		from = 0
	}
	for from > 0 && !utf8.RuneStart(s[from]) {
		from--
	}
	to := at + 20
	if to > len(s) {
		to = len(s)
	}
	for to < len(s) && !utf8.RuneStart(s[to]) {
		to++
	}
	return fmt.Errorf("JSON 语法错误，出错位置在「%s」附近（第 %d 字节）", s[from:to], at)
}

// repairModelJSON fixes the mistakes models make most often when writing JSON
// by hand: an ASCII double quote inside a string value (common when Chinese
// text quotes dialogue, e.g. "他叫他"甜心""), a raw line break inside a
// string, and a trailing comma before a closing bracket. A quote counts as the end of a string only when
// what follows could legally follow a string; anything else is escaped.
func repairModelJSON(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 16)
	inString := false
	escaped := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inString {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				if closesJSONString(s, i+1) {
					inString = false
				} else {
					b.WriteByte('\\')
				}
			case c < 0x20:
				// Raw line breaks and tabs inside a string (common in long
				// summaries) are invalid JSON; write them escaped.
				switch c {
				case '\n':
					b.WriteString(`\n`)
				case '\r':
					b.WriteString(`\r`)
				case '\t':
					b.WriteString(`\t`)
				default:
					fmt.Fprintf(&b, `\u%04x`, c)
				}
				continue
			}
			b.WriteByte(c)
			continue
		}
		switch c {
		case '"':
			inString = true
		case ',':
			if j := skipJSONSpace(s, i+1); j < len(s) && (s[j] == '}' || s[j] == ']') {
				continue
			}
		}
		b.WriteByte(c)
	}
	return b.String()
}

func closesJSONString(s string, from int) bool {
	j := skipJSONSpace(s, from)
	if j >= len(s) {
		return true
	}
	switch s[j] {
	case ':', '}', ']':
		return true
	case ',':
		k := skipJSONSpace(s, j+1)
		if k >= len(s) {
			return true
		}
		switch c := s[k]; {
		case c == '"' || c == '{' || c == '[' || c == '}' || c == ']' || c == '-':
			return true
		case c >= '0' && c <= '9':
			return true
		case strings.HasPrefix(s[k:], "true") || strings.HasPrefix(s[k:], "false") || strings.HasPrefix(s[k:], "null"):
			return true
		}
	}
	return false
}

func skipJSONSpace(s string, i int) int {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
		i++
	}
	return i
}
