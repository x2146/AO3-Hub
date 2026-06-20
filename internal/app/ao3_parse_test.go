package app

import (
	"strings"
	"testing"
)

func TestParseAO3HTML(t *testing.T) {
	html := `<!doctype html>
<html>
<head><title>Fallback Title - Chapter 1 - Author - Archive of Our Own</title></head>
<body>
  <div id="preface">
    <div class="meta">
      <h1>Example Work</h1>
      <div class="byline">by <a rel="author" href="/users/tester">tester</a></div>
      <dl class="tags">
        <dt>Fandoms:</dt><dd><a>Sample Fandom</a></dd>
        <dt>Rating:</dt><dd><a>Teen And Up Audiences</a></dd>
        <dt>Stats:</dt><dd>Published: 2026-01-02 Words: 1,234 Chapters: 1/1</dd>
        <dt>Language:</dt><dd>English</dd>
      </dl>
    </div>
    <blockquote class="userstuff"><p>Summary <em>text</em>.</p></blockquote>
  </div>
  <div id="chapters">
    <div class="userstuff">
      <p>Hello <em>world</em>.</p>
      <hr/>
      <blockquote><p>Quoted.</p></blockquote>
    </div>
  </div>
</body>
</html>`

	parsed, err := parseAO3HTML(html)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Meta.Title != "Example Work" {
		t.Fatalf("title = %q", parsed.Meta.Title)
	}
	if parsed.Meta.Author != "tester" {
		t.Fatalf("author = %q", parsed.Meta.Author)
	}
	if parsed.Meta.WordCount != 1234 {
		t.Fatalf("word count = %d", parsed.Meta.WordCount)
	}
	if parsed.Meta.Language != "en" {
		t.Fatalf("language = %q", parsed.Meta.Language)
	}
	if len(parsed.Original.Chapters) != 1 {
		t.Fatalf("chapters = %d", len(parsed.Original.Chapters))
	}
	blocks := parsed.Original.Chapters[0].Blocks
	if len(blocks) != 3 {
		t.Fatalf("blocks = %d", len(blocks))
	}
	if blocks[0].Type != BlockP || blocks[0].HTML != "<p>Hello <em>world</em>.</p>" {
		t.Fatalf("first block = %#v", blocks[0])
	}
	if blocks[1].Type != BlockHR || blocks[1].HTML != "<hr/>" {
		t.Fatalf("hr block = %#v", blocks[1])
	}
	if blocks[2].Type != BlockBlockquote || blocks[2].HTML != "<blockquote><p>Quoted.</p></blockquote>" {
		t.Fatalf("blockquote block = %#v", blocks[2])
	}
}

func TestParseAO3HTMLPreservesRichTextBlocks(t *testing.T) {
	html := `<!doctype html>
<html>
<head><title>Rich Text - Archive of Our Own</title></head>
<body>
  <div id="preface">
    <div class="meta">
      <h1>Rich Text</h1>
      <div class="byline">by <a rel="author">tester</a></div>
    </div>
  </div>
  <div id="chapters">
    <div class="userstuff">
      <p style="text-align: right;" onclick="bad()">Right <em>now</em>.</p>
      <p align="center"><br /></p>
      <p>&nbsp;</p>
      <p class="rteleft" style="text-align: left; background-image: url(javascript:bad)">Left</p>
      <ul><li>First</li><li><i>Second</i></li></ul>
    </div>
  </div>
</body>
</html>`

	parsed, err := parseAO3HTML(html)
	if err != nil {
		t.Fatal(err)
	}
	blocks := parsed.Original.Chapters[0].Blocks
	if len(blocks) != 5 {
		t.Fatalf("blocks = %d", len(blocks))
	}
	if got := blocks[0].HTML; got != `<p style="text-align: right">Right <em>now</em>.</p>` {
		t.Fatalf("right aligned block = %q", got)
	}
	if strings.Contains(blocks[0].HTML, "onclick") {
		t.Fatalf("unsafe attr was not removed: %q", blocks[0].HTML)
	}
	if got := blocks[1].HTML; !strings.Contains(got, `align="center"`) || !strings.Contains(got, "<br") {
		t.Fatalf("center blank line = %q", got)
	}
	if got := blocks[2].HTML; got != "<p>\u00a0</p>" {
		t.Fatalf("nbsp blank line = %q", got)
	}
	if got := blocks[3].HTML; got != `<p class="rteleft" style="text-align: left">Left</p>` {
		t.Fatalf("left aligned block = %q", got)
	}
	if blocks[4].Type != BlockUL || blocks[4].HTML != `<ul><li>First</li><li><i>Second</i></li></ul>` {
		t.Fatalf("list block = %#v", blocks[4])
	}
}

func TestSanitizeHTMLFragment(t *testing.T) {
	raw := `<p onclick="bad()" style="text-align: right; background-image: url(javascript:bad)">Go <em>now</em><script>bad()</script></p><a href="javascript:bad">x</a>`
	got := sanitizeHTMLFragment(raw)
	want := `<p style="text-align: right">Go <em>now</em></p><a>x</a>`
	if got != want {
		t.Fatalf("sanitizeHTMLFragment() = %q, want %q", got, want)
	}
}
