package app

import "testing"

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
	if blocks[0].Type != BlockP || blocks[0].HTML != "Hello <em>world</em>." {
		t.Fatalf("first block = %#v", blocks[0])
	}
	if blocks[1].Type != BlockHR || blocks[1].HTML != "<hr/>" {
		t.Fatalf("hr block = %#v", blocks[1])
	}
}
