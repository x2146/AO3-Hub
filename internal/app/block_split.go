package app

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"
	xhtml "golang.org/x/net/html"
)

// maxBlockTextBytes caps the plain text stored in one block (~1250 approxTokens).
// Longer blocks are split at import so a single block can never dominate a
// translate request and the reader keeps paragraph-sized bilingual pairs.
const maxBlockTextBytes = 4000

var (
	wrapperTags = map[string]bool{
		"article": true, "aside": true, "div": true, "footer": true,
		"header": true, "main": true, "section": true,
	}
	blockLevelTags = map[string]bool{
		"article": true, "aside": true, "blockquote": true, "center": true,
		"dd": true, "div": true, "dl": true, "dt": true, "figcaption": true,
		"figure": true, "footer": true, "h1": true, "h2": true, "h3": true,
		"h4": true, "h5": true, "h6": true, "header": true, "hr": true,
		"li": true, "main": true, "ol": true, "p": true, "pre": true,
		"section": true, "table": true, "tbody": true, "td": true,
		"tfoot": true, "th": true, "thead": true, "tr": true, "ul": true,
	}
)

// isFlattenableWrapper reports whether el only groups other blocks (e.g. the
// nested <div>s left by pasting from a word processor). Such wrappers are
// descended into so each inner paragraph becomes its own block. Wrappers that
// carry alignment, or mix bare text with blocks, are kept whole.
func isFlattenableWrapper(el *goquery.Selection) bool {
	if len(el.Nodes) == 0 {
		return false
	}
	node := el.Nodes[0]
	if node.Namespace != "" || !wrapperTags[strings.ToLower(node.Data)] {
		return false
	}
	for _, attr := range node.Attr {
		if attr.Namespace != "" {
			continue
		}
		switch strings.ToLower(attr.Key) {
		case "align":
			if sanitizeAlign(attr.Val) != "" {
				return false
			}
		case "class":
			if sanitizeClass(attr.Val) != "" {
				return false
			}
		case "style":
			if sanitizeStyle(attr.Val) != "" {
				return false
			}
		}
	}
	hasBlockChild := false
	for c := node.FirstChild; c != nil; c = c.NextSibling {
		switch c.Type {
		case xhtml.TextNode:
			if meaningfulText(c.Data) {
				return false
			}
		case xhtml.ElementNode:
			if blockLevelTags[strings.ToLower(c.Data)] {
				hasBlockChild = true
			}
		}
	}
	return hasBlockChild
}

// splitOversizedBlockHTML splits a sanitized block whose text exceeds
// maxBlockTextBytes. Blocks within the limit are returned unchanged so their
// IDs stay stable.
func splitOversizedBlockHTML(raw string) []string {
	if len(htmlToPlainText(raw)) <= maxBlockTextBytes {
		return []string{raw}
	}
	body, err := parseHTMLBody(raw)
	if err != nil {
		return []string{raw}
	}
	pieces := []*xhtml.Node{}
	for c := body.FirstChild; c != nil; {
		next := c.NextSibling
		body.RemoveChild(c)
		pieces = append(pieces, splitNode(c, maxBlockTextBytes)...)
		c = next
	}
	out := make([]string, 0, len(pieces))
	for _, piece := range pieces {
		if !nodeHasContent(piece) {
			continue
		}
		var b strings.Builder
		if err := xhtml.Render(&b, piece); err != nil {
			return []string{raw}
		}
		if html := strings.TrimSpace(b.String()); html != "" {
			out = append(out, html)
		}
	}
	if len(out) == 0 {
		return []string{raw}
	}
	return out
}

type splitUnit struct {
	node       *xhtml.Node
	size       int
	breakAfter bool
}

// splitNode returns detached nodes, each holding at most limit bytes of text.
// Oversized elements are cut into shallow clones of themselves, preferring to
// break after blocks and <br>, then after sentences, and only then anywhere.
func splitNode(n *xhtml.Node, limit int) []*xhtml.Node {
	if nodeTextLen(n) <= limit {
		return []*xhtml.Node{n}
	}
	switch n.Type {
	case xhtml.TextNode:
		units := textUnits(n.Data, limit)
		out := make([]*xhtml.Node, 0, len(units))
		for _, unit := range units {
			out = append(out, unit.node)
		}
		return groupTextNodes(out, limit)
	case xhtml.ElementNode:
	default:
		return []*xhtml.Node{n}
	}

	units := []splitUnit{}
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		n.RemoveChild(c)
		if c.Type == xhtml.TextNode {
			units = append(units, textUnits(c.Data, limit)...)
		} else {
			for _, piece := range splitNode(c, limit) {
				units = append(units, splitUnit{node: piece, size: nodeTextLen(piece), breakAfter: isNaturalBreak(piece)})
			}
		}
		c = next
	}

	groups := [][]*xhtml.Node{}
	current := []splitUnit{}
	size := 0
	flush := func(count int) {
		group := make([]*xhtml.Node, 0, count)
		for _, unit := range current[:count] {
			group = append(group, unit.node)
			size -= unit.size
		}
		groups = append(groups, group)
		current = current[count:]
	}
	for _, unit := range units {
		for len(current) > 0 && size+unit.size > limit {
			cut := len(current)
			for i := len(current) - 1; i >= 0; i-- {
				if current[i].breakAfter {
					cut = i + 1
					break
				}
			}
			flush(cut)
		}
		current = append(current, unit)
		size += unit.size
	}
	if len(current) > 0 {
		flush(len(current))
	}

	out := make([]*xhtml.Node, 0, len(groups))
	listItemsBefore := 0
	for _, group := range groups {
		clone := shallowCloneElement(n)
		if strings.EqualFold(n.Data, "ol") && listItemsBefore > 0 {
			setHTMLAttr(clone, "start", strconv.Itoa(olStart(n)+listItemsBefore))
		}
		for _, child := range group {
			clone.AppendChild(child)
			if child.Type == xhtml.ElementNode && strings.EqualFold(child.Data, "li") {
				listItemsBefore++
			}
		}
		out = append(out, clone)
	}
	return out
}

// groupTextNodes joins sentence-sized text fragments back into runs of at most
// limit bytes, used when an oversized text node sits at the top level.
func groupTextNodes(nodes []*xhtml.Node, limit int) []*xhtml.Node {
	out := []*xhtml.Node{}
	var b strings.Builder
	for _, node := range nodes {
		if b.Len() > 0 && b.Len()+len(node.Data) > limit {
			out = append(out, &xhtml.Node{Type: xhtml.TextNode, Data: b.String()})
			b.Reset()
		}
		b.WriteString(node.Data)
	}
	if b.Len() > 0 {
		out = append(out, &xhtml.Node{Type: xhtml.TextNode, Data: b.String()})
	}
	return out
}

// textUnits cuts text into sentence fragments (breaking after sentence
// punctuation or newlines); fragments still over limit are hard-cut at
// whitespace, or at a rune boundary when there is none.
func textUnits(text string, limit int) []splitUnit {
	units := []splitUnit{}
	for _, sentence := range splitSentences(text) {
		if len(sentence) <= limit {
			units = append(units, splitUnit{
				node:       &xhtml.Node{Type: xhtml.TextNode, Data: sentence},
				size:       len(sentence),
				breakAfter: endsSentence(sentence),
			})
			continue
		}
		for _, part := range hardSplit(sentence, limit) {
			units = append(units, splitUnit{node: &xhtml.Node{Type: xhtml.TextNode, Data: part}, size: len(part)})
		}
	}
	return units
}

func isSentenceEnd(r rune) bool {
	switch r {
	case '.', '!', '?', '…', '。', '！', '？', '；':
		return true
	}
	return false
}

func isClosingMark(r rune) bool {
	switch r {
	case '"', '\'', '”', '’', ')', ']', '）', '」', '』', '》':
		return true
	}
	return false
}

func isCJKSentenceEnd(r rune) bool {
	switch r {
	case '。', '！', '？', '；', '…':
		return true
	}
	return false
}

func splitSentences(text string) []string {
	out := []string{}
	start := 0
	i := 0
	for i < len(text) {
		r, width := utf8.DecodeRuneInString(text[i:])
		i += width
		if r == '\n' {
			out = append(out, text[start:i])
			start = i
			continue
		}
		if !isSentenceEnd(r) {
			continue
		}
		cjk := isCJKSentenceEnd(r)
		for i < len(text) {
			next, w := utf8.DecodeRuneInString(text[i:])
			if !isSentenceEnd(next) && !isClosingMark(next) {
				break
			}
			cjk = cjk || isCJKSentenceEnd(next)
			i += w
		}
		end := i
		for end < len(text) {
			next, w := utf8.DecodeRuneInString(text[end:])
			if !unicode.IsSpace(next) || next == '\n' {
				break
			}
			end += w
		}
		if end > i || end == len(text) || cjk {
			out = append(out, text[start:end])
			start = end
			i = end
		}
	}
	if start < len(text) {
		out = append(out, text[start:])
	}
	return out
}

func endsSentence(text string) bool {
	if strings.HasSuffix(text, "\n") {
		return true
	}
	trimmed := strings.TrimRightFunc(strings.TrimRightFunc(text, unicode.IsSpace), isClosingMark)
	r, _ := utf8.DecodeLastRuneInString(trimmed)
	return isSentenceEnd(r)
}

func hardSplit(text string, limit int) []string {
	out := []string{}
	for len(text) > limit {
		cut := strings.LastIndexFunc(text[:limit], unicode.IsSpace)
		if cut < limit/2 {
			cut = limit
			for cut > 0 && !utf8.RuneStart(text[cut]) {
				cut--
			}
		} else {
			cut++
		}
		out = append(out, text[:cut])
		text = text[cut:]
	}
	if text != "" {
		out = append(out, text)
	}
	return out
}

func isNaturalBreak(n *xhtml.Node) bool {
	if n.Type == xhtml.ElementNode {
		tag := strings.ToLower(n.Data)
		if tag == "br" || blockLevelTags[tag] {
			return true
		}
	}
	return endsSentence(nodeText(n))
}

func shallowCloneElement(n *xhtml.Node) *xhtml.Node {
	return &xhtml.Node{
		Type:      n.Type,
		DataAtom:  n.DataAtom,
		Data:      n.Data,
		Namespace: n.Namespace,
		Attr:      append([]xhtml.Attribute(nil), n.Attr...),
	}
}

func olStart(n *xhtml.Node) int {
	for _, attr := range n.Attr {
		if attr.Namespace == "" && strings.EqualFold(attr.Key, "start") {
			if start, err := strconv.Atoi(strings.TrimSpace(attr.Val)); err == nil {
				return start
			}
		}
	}
	return 1
}

func nodeText(n *xhtml.Node) string {
	if n.Type == xhtml.TextNode {
		return n.Data
	}
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		b.WriteString(nodeText(c))
	}
	return b.String()
}

func nodeTextLen(n *xhtml.Node) int {
	if n.Type == xhtml.TextNode {
		return len(n.Data)
	}
	total := 0
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		total += nodeTextLen(c)
	}
	return total
}

func nodeHasContent(n *xhtml.Node) bool {
	if meaningfulText(nodeText(n)) {
		return true
	}
	var hasImage func(*xhtml.Node) bool
	hasImage = func(node *xhtml.Node) bool {
		if node.Type == xhtml.ElementNode && strings.EqualFold(node.Data, "img") {
			return true
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			if hasImage(c) {
				return true
			}
		}
		return false
	}
	return hasImage(n)
}
