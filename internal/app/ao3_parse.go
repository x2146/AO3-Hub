package app

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

type parsedMeta struct {
	Meta
	WorkIDGuess  string
	WorkURLGuess string
}

type parseResult struct {
	Meta     parsedMeta
	Original ChapterFile
}

func stableID(input string) string {
	sum := sha256.Sum256([]byte(input))
	return hex.EncodeToString(sum[:16])
}

func blockID(chapterIndex int, html string) string {
	return stableID(strconv.Itoa(chapterIndex) + "::" + html)
}

var whitespaceRE = regexp.MustCompile(`\s+`)

func selectionText(sel *goquery.Selection) string {
	return strings.TrimSpace(whitespaceRE.ReplaceAllString(sel.Text(), " "))
}

var (
	blockTagRE         = regexp.MustCompile(`^(p|div|blockquote|pre|h[1-6]|ul|ol|li|center|figure|figcaption|table|thead|tbody|tr|td|th)$`)
	classTokenRE       = regexp.MustCompile(`^[A-Za-z0-9_:-]+$`)
	safeCSSValueRE     = regexp.MustCompile(`^[A-Za-z0-9\s.,#%()+/_-]+$`)
	unsafeCSSValueRE   = regexp.MustCompile(`(?i)(url\s*\(|expression\s*\(|javascript:|data:)`)
	allowedClassTokens = map[string]bool{
		"center": true, "justify": true, "left": true, "right": true,
		"rtecenter": true, "rtejustify": true, "rteleft": true, "rteright": true,
	}
	allowedHTMLTags = map[string]bool{
		"a": true, "abbr": true, "acronym": true, "b": true, "bdi": true, "bdo": true,
		"big": true, "blockquote": true, "br": true, "center": true, "cite": true,
		"code": true, "dd": true, "del": true, "div": true, "dl": true, "dt": true,
		"em": true, "figcaption": true, "figure": true, "font": true, "h1": true,
		"h2": true, "h3": true, "h4": true, "h5": true, "h6": true, "hr": true,
		"i": true, "img": true, "ins": true, "kbd": true, "li": true, "mark": true,
		"ol": true, "p": true, "pre": true, "q": true, "rp": true, "rt": true,
		"ruby": true, "s": true, "samp": true, "small": true, "span": true,
		"strike": true, "strong": true, "sub": true, "sup": true, "table": true,
		"tbody": true, "td": true, "tfoot": true, "th": true, "thead": true,
		"time": true, "tr": true, "tt": true, "u": true, "ul": true, "var": true,
		"wbr": true,
	}
	allowedStyleProps = map[string]bool{
		"direction":       true,
		"font-style":      true,
		"font-weight":     true,
		"list-style-type": true,
		"margin-left":     true,
		"margin-right":    true,
		"padding-left":    true,
		"padding-right":   true,
		"text-align":      true,
		"text-decoration": true,
		"unicode-bidi":    true,
		"vertical-align":  true,
		"white-space":     true,
	}
	allowedGlobalAttrs = map[string]bool{
		"align": true,
		"class": true,
		"dir":   true,
		"lang":  true,
		"style": true,
		"title": true,
	}
	allowedAttrsByTag = map[string]map[string]bool{
		"a": {
			"href":   true,
			"name":   true,
			"rel":    true,
			"target": true,
		},
		"img": {
			"alt":    true,
			"height": true,
			"src":    true,
			"width":  true,
		},
		"font": {
			"color": true,
			"face":  true,
			"size":  true,
		},
		"ol": {
			"start": true,
			"type":  true,
		},
		"ul": {
			"type": true,
		},
		"li": {
			"type":  true,
			"value": true,
		},
		"td": {
			"colspan": true,
			"headers": true,
			"rowspan": true,
		},
		"th": {
			"colspan": true,
			"headers": true,
			"rowspan": true,
			"scope":   true,
		},
		"time": {
			"datetime": true,
		},
	}
)

func isRemovedHTMLTag(tag string) bool {
	switch tag {
	case "script", "style", "iframe", "object", "embed", "form", "input", "button", "select", "option", "textarea", "svg", "math", "template", "canvas", "portal":
		return true
	default:
		return false
	}
}

func sanitizeClass(value string) string {
	tokens := []string{}
	for _, token := range strings.Fields(value) {
		if classTokenRE.MatchString(token) && allowedClassTokens[token] {
			tokens = append(tokens, token)
		}
	}
	return strings.Join(tokens, " ")
}

func sanitizeTokenList(value string) string {
	tokens := []string{}
	for _, token := range strings.Fields(value) {
		if classTokenRE.MatchString(token) {
			tokens = append(tokens, token)
		}
	}
	return strings.Join(tokens, " ")
}

func sanitizeAlign(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "left", "right", "center", "justify":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}

func hasUnsafeURLChars(value string) bool {
	for _, r := range value {
		if unicode.IsControl(r) || unicode.IsSpace(r) || r == '\\' {
			return true
		}
	}
	decoded, err := url.PathUnescape(value)
	if err != nil {
		return true
	}
	for _, r := range decoded {
		if unicode.IsControl(r) || r == '\\' {
			return true
		}
	}
	return false
}

func sanitizeURL(value string, allowMailto bool) string {
	v := strings.TrimSpace(value)
	if v == "" || hasUnsafeURLChars(v) || strings.HasPrefix(v, "//") {
		return ""
	}
	parsed, err := url.Parse(v)
	if err != nil || parsed.User != nil {
		return ""
	}
	if parsed.IsAbs() {
		switch strings.ToLower(parsed.Scheme) {
		case "http", "https":
			if parsed.Host == "" {
				return ""
			}
		case "mailto":
			if !allowMailto || parsed.Opaque == "" {
				return ""
			}
		default:
			return ""
		}
		return v
	}
	if parsed.Host != "" || parsed.Opaque != "" {
		return ""
	}
	decoded, err := url.PathUnescape(parsed.Path)
	if err != nil {
		return ""
	}
	head := strings.FieldsFunc(decoded, func(r rune) bool {
		return r == '/' || r == '?' || r == '#'
	})
	if len(head) > 0 && strings.Contains(head[0], ":") {
		return ""
	}
	return v
}

func sanitizeStyle(value string) string {
	parts := []string{}
	for _, raw := range strings.Split(value, ";") {
		prop, val, ok := strings.Cut(raw, ":")
		if !ok {
			continue
		}
		prop = strings.ToLower(strings.TrimSpace(prop))
		val = strings.TrimSpace(val)
		if !allowedStyleProps[prop] || val == "" {
			continue
		}
		if unsafeCSSValueRE.MatchString(val) || !safeCSSValueRE.MatchString(val) {
			continue
		}
		if prop == "text-align" {
			if align := sanitizeAlign(val); align != "" {
				parts = append(parts, prop+": "+align)
			}
			continue
		}
		parts = append(parts, prop+": "+val)
	}
	return strings.Join(parts, "; ")
}

func sanitizeAttr(tag, name, value string) (string, string, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if strings.HasPrefix(name, "on") || strings.HasPrefix(name, "data-") || name == "" {
		return "", "", false
	}
	allowed := allowedGlobalAttrs[name]
	if tagAttrs := allowedAttrsByTag[tag]; tagAttrs != nil && tagAttrs[name] {
		allowed = true
	}
	if !allowed {
		return "", "", false
	}
	switch name {
	case "align":
		value = sanitizeAlign(value)
	case "class":
		value = sanitizeClass(value)
	case "dir":
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "ltr" && value != "rtl" && value != "auto" {
			value = ""
		}
	case "href":
		value = sanitizeURL(value, true)
	case "src":
		value = sanitizeURL(value, false)
	case "rel":
		value = sanitizeTokenList(value)
	case "style":
		value = sanitizeStyle(value)
	case "target":
		value = strings.TrimSpace(value)
		if value != "_blank" && value != "_self" {
			value = ""
		}
	default:
		value = strings.TrimSpace(value)
	}
	return name, value, value != ""
}

func sanitizeElement(node *html.Node) {
	tag := strings.ToLower(node.Data)
	attrs := node.Attr[:0:0]
	for _, attr := range node.Attr {
		if attr.Namespace != "" {
			continue
		}
		name, value, ok := sanitizeAttr(tag, attr.Key, attr.Val)
		if ok {
			attr.Key = name
			attr.Val = value
			attrs = append(attrs, attr)
		}
	}
	node.Attr = attrs
	if tag == "a" {
		for _, attr := range node.Attr {
			if attr.Key != "href" {
				continue
			}
			href := attr.Val
			lower := strings.ToLower(href)
			if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
				setHTMLAttr(node, "rel", "nofollow noopener noreferrer")
			}
			break
		}
	}
}

func setHTMLAttr(node *html.Node, name, value string) {
	for i := range node.Attr {
		if node.Attr[i].Key == name && node.Attr[i].Namespace == "" {
			node.Attr[i].Val = value
			return
		}
	}
	node.Attr = append(node.Attr, html.Attribute{Key: name, Val: value})
}

func removeHTMLNode(node *html.Node) {
	if node.Parent != nil {
		node.Parent.RemoveChild(node)
	}
}

func sanitizeNode(node *html.Node, preserveRoot bool) {
	if node.Type == html.CommentNode {
		removeHTMLNode(node)
		return
	}
	if node.Type != html.ElementNode {
		for child := node.FirstChild; child != nil; {
			next := child.NextSibling
			sanitizeNode(child, false)
			child = next
		}
		return
	}

	tag := strings.ToLower(node.Data)
	if node.Namespace != "" || isRemovedHTMLTag(tag) {
		removeHTMLNode(node)
		return
	}
	if !allowedHTMLTags[tag] {
		if preserveRoot {
			node.Data = "p"
			node.Namespace = ""
			sanitizeElement(node)
			for child := node.FirstChild; child != nil; {
				next := child.NextSibling
				sanitizeNode(child, false)
				child = next
			}
			return
		}
		for child := node.FirstChild; child != nil; {
			next := child.NextSibling
			sanitizeNode(child, false)
			child = next
		}
		parent := node.Parent
		if parent != nil {
			for child := node.FirstChild; child != nil; {
				next := child.NextSibling
				node.RemoveChild(child)
				parent.InsertBefore(child, node)
				child = next
			}
			parent.RemoveChild(node)
		}
		return
	}

	sanitizeElement(node)
	for child := node.FirstChild; child != nil; {
		next := child.NextSibling
		sanitizeNode(child, false)
		child = next
	}
}

func sanitizeSelection(sel *goquery.Selection) {
	sel.Each(func(_ int, item *goquery.Selection) {
		if len(item.Nodes) > 0 {
			sanitizeNode(item.Nodes[0], false)
		}
	})
}

func sanitizeBlock(sel *goquery.Selection) string {
	if len(sel.Nodes) == 0 || sel.Nodes[0].Namespace != "" || isRemovedHTMLTag(strings.ToLower(goquery.NodeName(sel))) {
		return ""
	}
	clone := sel.Clone()
	if len(clone.Nodes) == 0 {
		return ""
	}
	sanitizeNode(clone.Nodes[0], true)
	var b strings.Builder
	if err := goquery.Render(&b, clone); err != nil {
		return ""
	}
	return b.String()
}

func sanitizeHTMLFragment(raw string) string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader("<!doctype html><html><body>" + raw + "</body></html>"))
	if err != nil {
		return ""
	}
	contents := doc.Find("body").First().Contents()
	sanitizeSelection(contents)
	contents = doc.Find("body").First().Contents()
	var b strings.Builder
	contents.Each(func(_ int, el *goquery.Selection) {
		_ = goquery.Render(&b, el)
	})
	return b.String()
}

func stripImmersiveCruft(doc *goquery.Document) {
	doc.Find(".immersive-translate-target-wrapper, .immersive-translate-target-inner, .immersive-translate-target-translation-block-wrapper, [data-immersive-translate-translation-element-mark]").Remove()
	doc.Find(`font[lang="zh-CN"]`).Remove()
	doc.Find("[data-imt-p]").RemoveAttr("data-imt-p")
	doc.Find("#x2146-reader-style, #x2146-reader-script, body > .reader-topbar, main.reader-shell > header.reader-title").Remove()
}

func findChapterUserstuff(root *goquery.Selection) *goquery.Selection {
	candidates := []*goquery.Selection{root}
	root.Find(".userstuff").Each(func(_ int, sel *goquery.Selection) {
		candidates = append(candidates, sel)
	})
	var best *goquery.Selection
	bestScore := 0
	for _, cand := range candidates {
		if cand.Find(".userstuff").Length() > 0 {
			continue
		}
		score := cand.ChildrenFiltered("p, div, blockquote, pre, ul, ol, center, figure, table, hr, h1, h2, h3, h4, h5, h6").Length()
		if score > bestScore {
			best = cand
			bestScore = score
		}
	}
	return best
}

type parsedTags struct {
	Tags
	Stats struct {
		Words         int
		ChaptersDone  int
		ChaptersTotal int
		Published     string
		Updated       string
	}
	Language string
}

func parseTags(root *goquery.Selection) parsedTags {
	result := parsedTags{
		Tags: Tags{
			Fandom:       []string{},
			Relationship: []string{},
			Character:    []string{},
			Additional:   []string{},
			Warnings:     []string{},
			Categories:   []string{},
		},
	}
	dl := root.Find("dl.tags").First()
	if dl.Length() == 0 {
		return result
	}
	dts := dl.ChildrenFiltered("dt")
	dds := dl.ChildrenFiltered("dd")
	dts.Each(func(i int, dt *goquery.Selection) {
		label := strings.ToLower(strings.TrimSuffix(selectionText(dt), ":"))
		dd := dds.Eq(i)
		if dd.Length() == 0 {
			return
		}
		links := []string{}
		dd.Find("a").Each(func(_ int, a *goquery.Selection) {
			if text := selectionText(a); text != "" {
				links = append(links, text)
			}
		})
		text := selectionText(dd)
		switch {
		case strings.HasPrefix(label, "rating"):
			if len(links) > 0 {
				result.Rating = links[0]
			} else {
				result.Rating = text
			}
		case strings.Contains(label, "archive warning") || label == "warnings":
			if len(links) > 0 {
				result.Warnings = links
			} else if text != "" {
				result.Warnings = []string{text}
			}
		case strings.HasPrefix(label, "categor"):
			if len(links) > 0 {
				result.Categories = links
			} else {
				result.Categories = splitComma(text)
			}
		case strings.HasPrefix(label, "fandom"):
			if len(links) > 0 {
				result.Fandom = links
			} else if text != "" {
				result.Fandom = []string{text}
			}
		case strings.HasPrefix(label, "relationship"):
			result.Relationship = links
		case strings.HasPrefix(label, "character"):
			result.Character = links
		case strings.HasPrefix(label, "additional tag"):
			result.Additional = links
		case strings.HasPrefix(label, "language"):
			result.Language = text
		case strings.HasPrefix(label, "stats"):
			result.Stats = parseStats(text)
		}
	})
	return result
}

func splitComma(text string) []string {
	if text == "" {
		return []string{}
	}
	parts := strings.Split(text, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

var (
	wordsRE     = regexp.MustCompile(`(?i)Words:\s*([\d,]+)`)
	chaptersRE  = regexp.MustCompile(`(?i)Chapters:\s*(\d+)/(\d+|\?)`)
	publishedRE = regexp.MustCompile(`(?i)Published:\s*(\d{4}-\d{2}-\d{2})`)
	updatedRE   = regexp.MustCompile(`(?i)Updated:\s*(\d{4}-\d{2}-\d{2})`)
)

func parseStats(text string) struct {
	Words         int
	ChaptersDone  int
	ChaptersTotal int
	Published     string
	Updated       string
} {
	var out struct {
		Words         int
		ChaptersDone  int
		ChaptersTotal int
		Published     string
		Updated       string
	}
	if match := wordsRE.FindStringSubmatch(text); len(match) == 2 {
		out.Words, _ = strconv.Atoi(strings.ReplaceAll(match[1], ",", ""))
	}
	if match := chaptersRE.FindStringSubmatch(text); len(match) == 3 {
		out.ChaptersDone, _ = strconv.Atoi(match[1])
		if match[2] != "?" {
			out.ChaptersTotal, _ = strconv.Atoi(match[2])
		}
	}
	if match := publishedRE.FindStringSubmatch(text); len(match) == 2 {
		out.Published = match[1]
	}
	if match := updatedRE.FindStringSubmatch(text); len(match) == 2 {
		out.Updated = match[1]
	}
	return out
}

func inferLanguage(text string) string {
	t := strings.ToLower(strings.TrimSpace(text))
	if t == "" || strings.HasPrefix(t, "english") {
		return "en"
	}
	if strings.HasPrefix(t, "中文") || strings.Contains(t, "chinese") {
		return "zh"
	}
	return t
}

func pickBlockType(tag string) BlockType {
	switch tag {
	case "h2":
		return BlockH2
	case "h3":
		return BlockH3
	case "blockquote":
		return BlockBlockquote
	case "hr":
		return BlockHR
	case "pre":
		return BlockPre
	case "ul":
		return BlockUL
	case "ol":
		return BlockOL
	default:
		return BlockP
	}
}

func extractBlocks(root *goquery.Selection, chapterIndex int) []Block {
	blocks := []Block{}
	root.Children().Each(func(_ int, el *goquery.Selection) {
		node := goquery.NodeName(el)
		tag := strings.ToLower(node)
		if tag == "" || tag == "#text" {
			return
		}
		if tag == "hr" {
			html := "<hr/>"
			blocks = append(blocks, Block{
				ID:     blockID(chapterIndex, "hr-"+strconv.Itoa(len(blocks))),
				Type:   BlockHR,
				HTML:   html,
				Status: BlockPending,
			})
			return
		}
		if !blockTagRE.MatchString(tag) {
			tag = "p"
		}
		html := strings.TrimSpace(sanitizeBlock(el))
		if html == "" {
			return
		}
		blocks = append(blocks, Block{
			ID:     blockID(chapterIndex, html),
			Type:   pickBlockType(tag),
			HTML:   html,
			Status: BlockPending,
		})
	})
	return blocks
}

func ensureUniqueIDs(blocks []Block) []Block {
	seen := map[string]bool{}
	out := make([]Block, len(blocks))
	for i, block := range blocks {
		if !seen[block.ID] {
			seen[block.ID] = true
			out[i] = block
			continue
		}
		n := 1
		id := stableID(block.ID + "::duplicate::" + strconv.Itoa(n))
		for seen[id] {
			n++
			id = stableID(block.ID + "::duplicate::" + strconv.Itoa(n))
		}
		seen[id] = true
		block.ID = id
		out[i] = block
	}
	return out
}

type ao3URLKind int

const (
	ao3AuthorURL ao3URLKind = iota
	ao3WorkURL
)

var workPathRE = regexp.MustCompile(`^/works/(\d+)(?:/chapters/\d+)?/?$`)

func sanitizeAO3MetadataURL(raw string, kind ao3URLKind) (string, string, bool) {
	value := strings.TrimSpace(raw)
	lowerValue := strings.ToLower(value)
	if value == "" || hasUnsafeURLChars(value) || strings.HasPrefix(value, "//") || strings.Contains(lowerValue, "%2f") || strings.Contains(lowerValue, "%5c") {
		return "", "", false
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.User != nil || parsed.Fragment != "" {
		return "", "", false
	}
	absolute := parsed.IsAbs()
	if absolute {
		if !strings.EqualFold(parsed.Scheme, "https") || !strings.EqualFold(parsed.Hostname(), "archiveofourown.org") || parsed.Port() != "" {
			return "", "", false
		}
	} else if parsed.Scheme != "" || parsed.Host != "" || parsed.Opaque != "" || !strings.HasPrefix(value, "/") {
		return "", "", false
	}

	prefix := ""
	if absolute {
		prefix = "https://archiveofourown.org"
	}
	switch kind {
	case ao3WorkURL:
		match := workPathRE.FindStringSubmatch(parsed.Path)
		if len(match) != 2 {
			return "", "", false
		}
		path := "/works/" + match[1]
		return prefix + path, match[1], true
	case ao3AuthorURL:
		parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		if len(parts) != 2 && len(parts) != 4 || len(parts) >= 2 && parts[0] != "users" || len(parts) == 4 && parts[2] != "pseuds" {
			return "", "", false
		}
		for _, part := range parts {
			if part == "" || part == "." || part == ".." || strings.Contains(part, "/") {
				return "", "", false
			}
		}
		path := "/users/" + url.PathEscape(parts[1])
		if len(parts) == 4 {
			path += "/pseuds/" + url.PathEscape(parts[3])
		}
		return prefix + path, "", true
	default:
		return "", "", false
	}
}

func firstAO3WorkURL(root *goquery.Selection) (string, string) {
	workURL := ""
	workID := ""
	root.EachWithBreak(func(_ int, candidate *goquery.Selection) bool {
		href, _ := candidate.Attr("href")
		clean, id, ok := sanitizeAO3MetadataURL(href, ao3WorkURL)
		if !ok {
			return true
		}
		workURL = clean
		workID = id
		return false
	})
	return workURL, workID
}

func findAO3WorkURL(doc *goquery.Document, preface *goquery.Selection) (string, string) {
	if workURL, workID := firstAO3WorkURL(doc.Find(`head link[rel~="canonical"][href]`)); workID != "" {
		return workURL, workID
	}
	workURL := ""
	workID := ""
	preface.ChildrenFiltered("p.message").EachWithBreak(func(_ int, message *goquery.Selection) bool {
		if !strings.Contains(strings.ToLower(selectionText(message)), "archive of our own") {
			return true
		}
		workURL, workID = firstAO3WorkURL(message.Find("a[href]"))
		return workID == ""
	})
	return workURL, workID
}

func recognizedAO3Document(doc *goquery.Document, preface *goquery.Selection) bool {
	title := strings.ToLower(selectionText(doc.Find("title").First()))
	title = strings.TrimSuffix(strings.TrimSpace(title), "]")
	if strings.HasSuffix(title, "archive of our own") {
		return true
	}
	_, workID := findAO3WorkURL(doc, preface)
	return workID != ""
}

func knownAO3Error(doc *goquery.Document) error {
	title := strings.ToLower(selectionText(doc.Find("title").First()))
	if doc.Find("#preface").Length() > 0 && doc.Find("#chapters").Length() > 0 {
		return nil
	}
	if doc.Find(`#challenge-form, .cf-challenge, [name="cf-turnstile-response"], script[src*="challenges.cloudflare.com"]`).Length() > 0 || strings.Contains(title, "just a moment") {
		return errors.New("AO3 returned an anti-bot challenge page")
	}
	if doc.Find(`form#new_user, form[action*="/users/login"], #login form`).Length() > 0 || strings.HasPrefix(title, "log in") {
		return errors.New("AO3 returned a login page; authenticated access may be required")
	}
	pageText := strings.ToLower(selectionText(doc.Find("body").First()))
	for _, marker := range []string{
		"this work has been deleted",
		"work you were looking for was deleted",
		"couldn't find the work you were looking for",
		"could not find the work you were looking for",
	} {
		if strings.Contains(pageText, marker) {
			return errors.New("AO3 reports that the work was deleted or does not exist")
		}
	}
	for _, marker := range []string{
		"you don't have permission",
		"you do not have permission",
		"only available to registered users",
		"restricted to registered users",
		"you need to log in",
		"you must be logged in",
	} {
		if strings.Contains(pageText, marker) {
			return errors.New("AO3 denied permission to access this work")
		}
	}
	return nil
}

func meaningfulText(value string) bool {
	for _, r := range value {
		if !unicode.IsSpace(r) && r != '\u200b' && r != '\u200c' && r != '\u200d' && r != '\ufeff' {
			return true
		}
	}
	return false
}

func meaningfulBlock(block Block) bool {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader("<!doctype html><html><body>" + block.HTML + "</body></html>"))
	if err != nil {
		return false
	}
	if meaningfulText(doc.Find("body").Text()) {
		return true
	}
	return doc.Find("body img[src]").Length() > 0
}

func parseAO3HTML(html string) (parseResult, error) {
	if !meaningfulText(html) {
		return parseResult{}, errors.New("AO3 HTML is empty")
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return parseResult{}, fmt.Errorf("parse AO3 HTML: %w", err)
	}
	stripImmersiveCruft(doc)
	if err := knownAO3Error(doc); err != nil {
		return parseResult{}, err
	}

	preface := doc.Find("#preface").First()
	chaptersEl := doc.Find("#chapters").First()
	afterword := doc.Find("#afterword").First()
	if preface.Length() == 0 {
		return parseResult{}, errors.New("AO3 work document is missing required #preface")
	}
	if chaptersEl.Length() == 0 {
		return parseResult{}, errors.New("AO3 work document is missing required #chapters")
	}
	if !recognizedAO3Document(doc, preface) {
		return parseResult{}, errors.New("HTML is not a recognized AO3 work document")
	}

	titleFromMeta := selectionText(preface.Find(".meta h1").First())
	titleFromHTMLTitle := strings.TrimSpace(strings.Split(selectionText(doc.Find("title").First()), " - ")[0])
	title := titleFromMeta
	if title == "" {
		title = titleFromHTMLTitle
	}
	if title == "" {
		title = "Untitled"
	}

	authorEl := preface.Find(".byline a[rel=author]").First()
	if authorEl.Length() == 0 {
		authorEl = preface.Find(".byline a").First()
	}
	author := selectionText(authorEl)
	if author == "" {
		author = regexp.MustCompile(`(?i)^by\s+`).ReplaceAllString(selectionText(preface.Find(".byline").First()), "")
	}
	authorURL := ""
	if href, ok := authorEl.Attr("href"); ok {
		authorURL, _, _ = sanitizeAO3MetadataURL(href, ao3AuthorURL)
	}

	userstuffs := preface.Find("blockquote.userstuff")
	summary := strings.TrimSpace(sanitizeHTMLFragment(htmlOf(userstuffs.Eq(0))))
	notes := ""
	if userstuffs.Length() > 1 {
		notes = strings.TrimSpace(sanitizeHTMLFragment(htmlOf(userstuffs.Eq(1))))
	}
	endnotes := strings.TrimSpace(sanitizeHTMLFragment(htmlOf(afterword.Find("blockquote").First())))

	tags := parseTags(preface)

	workURLGuess, workIDGuess := findAO3WorkURL(doc, preface)

	chapters := []Chapter{}
	groups := chaptersEl.ChildrenFiltered(".meta.group, .userstuff")
	hasMetaGroup := groups.Filter(".meta.group").Length() > 0
	if hasMetaGroup {
		currentTitle := ""
		idx := 0
		groups.Each(func(_ int, el *goquery.Selection) {
			if el.Is(".meta.group") {
				currentTitle = selectionText(el.Find(".heading").First())
				return
			}
			if el.Is(".userstuff") {
				blocks := ensureUniqueIDs(extractBlocks(el, idx))
				chapters = append(chapters, Chapter{
					Index:  idx,
					Title:  currentTitle,
					Blocks: blocks,
				})
				currentTitle = ""
				idx++
			}
		})
	} else if chaptersEl.Length() > 0 {
		if userstuff := findChapterUserstuff(chaptersEl); userstuff != nil {
			chapterTitle := selectionText(chaptersEl.Find("> h2.toc-heading").First())
			if chapterTitle == "" {
				chapterTitle = title
			}
			blocks := ensureUniqueIDs(extractBlocks(userstuff, 0))
			chapters = append(chapters, Chapter{
				Index:  0,
				Title:  chapterTitle,
				Blocks: blocks,
			})
		}
	}

	chapterCount := len(chapters)
	meaningfulChapters := 0
	for _, chapter := range chapters {
		for _, block := range chapter.Blocks {
			if meaningfulBlock(block) {
				meaningfulChapters++
				break
			}
		}
	}
	if meaningfulChapters == 0 {
		return parseResult{}, errors.New("AO3 work document has no chapter with meaningful body blocks")
	}
	if chapterCount == 0 && tags.Stats.ChaptersDone > 0 {
		chapterCount = tags.Stats.ChaptersDone
	}
	if chapterCount == 0 {
		chapterCount = 1
	}

	meta := parsedMeta{
		Meta: Meta{
			Title:        title,
			Author:       author,
			AuthorURL:    authorURL,
			Summary:      summary,
			Notes:        notes,
			Endnotes:     endnotes,
			Tags:         normalizeTags(tags.Tags),
			Language:     inferLanguage(tags.Language),
			PublishedAt:  tags.Stats.Published,
			UpdatedAt:    tags.Stats.Updated,
			WordCount:    tags.Stats.Words,
			ChapterCount: chapterCount,
		},
		WorkIDGuess:  workIDGuess,
		WorkURLGuess: workURLGuess,
	}

	return parseResult{Meta: meta, Original: ChapterFile{Chapters: chapters}}, nil
}

func htmlOf(sel *goquery.Selection) string {
	if sel == nil || sel.Length() == 0 {
		return ""
	}
	html, _ := sel.Html()
	return html
}
