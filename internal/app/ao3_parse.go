package app

import (
	"crypto/sha1"
	"encoding/hex"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
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

func sha1Hex(input string) string {
	sum := sha1.Sum([]byte(input))
	return hex.EncodeToString(sum[:])
}

func blockID(chapterIndex int, html string) string {
	return sha1Hex(strconv.Itoa(chapterIndex) + "::" + html)[:8]
}

var whitespaceRE = regexp.MustCompile(`\s+`)

func selectionText(sel *goquery.Selection) string {
	return strings.TrimSpace(whitespaceRE.ReplaceAllString(sel.Text(), " "))
}

func sanitizeInline(sel *goquery.Selection) string {
	clone := sel.Clone()
	clone.Find("script, style").Remove()
	html, _ := clone.Html()
	return html
}

func stripImmersiveCruft(doc *goquery.Document) {
	doc.Find(".immersive-translate-target-wrapper, .immersive-translate-target-inner, .immersive-translate-target-translation-block-wrapper, [data-immersive-translate-translation-element-mark]").Remove()
	doc.Find(`font[lang="zh-CN"]`).Remove()
	doc.Find("[data-imt-p]").RemoveAttr("data-imt-p")
	doc.Find("#x2146-reader-style, #x2146-reader-script, .reader-topbar, .reader-progress, .reader-controls, header.reader-title").Remove()
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
		score := cand.ChildrenFiltered("p, blockquote").Length()
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
		html := strings.TrimSpace(sanitizeInline(el))
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
		id := block.ID + "-" + strconv.Itoa(n)
		for seen[id] {
			n++
			id = block.ID + "-" + strconv.Itoa(n)
		}
		seen[id] = true
		block.ID = id
		out[i] = block
	}
	return out
}

var workHrefRE = regexp.MustCompile(`/works/(\d+)`)

func parseAO3HTML(html string) (parseResult, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return parseResult{}, err
	}
	stripImmersiveCruft(doc)

	preface := doc.Find("#preface").First()
	chaptersEl := doc.Find("#chapters").First()
	afterword := doc.Find("#afterword").First()

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
	authorURL, _ := authorEl.Attr("href")

	userstuffs := preface.Find("blockquote.userstuff")
	summary := strings.TrimSpace(htmlOf(userstuffs.Eq(0)))
	notes := ""
	if userstuffs.Length() > 1 {
		notes = strings.TrimSpace(htmlOf(userstuffs.Eq(1)))
	}
	endnotes := strings.TrimSpace(htmlOf(afterword.Find("blockquote").First()))

	tags := parseTags(preface)

	workURLGuess := ""
	preface.Find(`a[href*="archiveofourown.org/works/"]`).EachWithBreak(func(_ int, a *goquery.Selection) bool {
		workURLGuess, _ = a.Attr("href")
		return false
	})
	workIDGuess := ""
	if match := workHrefRE.FindStringSubmatch(workURLGuess); len(match) == 2 {
		workIDGuess = match[1]
	}

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
