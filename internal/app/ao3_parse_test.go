package app

import (
	"encoding/hex"
	"os"
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
      <p class="rteleft fixed inset-0" style="text-align: left; background-image: url(javascript:bad)">Left</p>
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

func TestSanitizeHTMLFragmentRemovesDangerousContainers(t *testing.T) {
	raw := `<svg><script>alert(1)</script><a xlink:href="javascript:bad">svg</a></svg>` +
		`<math><mtext>math</mtext></math><template><img src=x onerror=bad()>template</template>` +
		`<form action="https://evil.example"><p>form</p></form>` +
		`<marquee onclick="bad()">kept <em>text</em></marquee>` +
		`<p><a href="https://example.com" target="_top">external</a></p>`
	got := sanitizeHTMLFragment(raw)
	for _, forbidden := range []string{"svg", "script", "math", "template", "form", "marquee", "onclick", "target="} {
		if strings.Contains(strings.ToLower(got), forbidden) {
			t.Fatalf("sanitized fragment retained %q: %q", forbidden, got)
		}
	}
	if !strings.Contains(got, `kept <em>text</em>`) {
		t.Fatalf("unknown ordinary element was not safely unwrapped: %q", got)
	}
	if !strings.Contains(got, `href="https://example.com" rel="nofollow noopener noreferrer"`) {
		t.Fatalf("external link was not hardened: %q", got)
	}
}

func TestSanitizeHTMLFragmentRejectsConfusedProtocolsAndMalformedHTML(t *testing.T) {
	raw := `<p>` +
		`<a href="java&#x0a;script:alert(1)">newline</a>` +
		`<a href="java%73cript:alert(1)">encoded</a>` +
		`<a href="//evil.example/path">network</a>` +
		`<a href="https://user@evil.example/path">userinfo</a>` +
		`<img src="data:text/html,bad" onerror="bad()">` +
		`<strong>still safe<script>bad()</strong></p>`
	got := sanitizeHTMLFragment(raw)
	for _, forbidden := range []string{"javascript", "java%73cript", "//evil.example", "user@evil", "data:text", "onerror", "script"} {
		if strings.Contains(strings.ToLower(got), forbidden) {
			t.Fatalf("sanitized malformed fragment retained %q: %q", forbidden, got)
		}
	}
	if !strings.Contains(got, "still safe") {
		t.Fatalf("safe malformed content was lost: %q", got)
	}
}

func TestParseAO3HTMLSanitizesMetadataAndCanonicalizesURLs(t *testing.T) {
	html := `<!doctype html><html><head><title>Metadata - Archive of Our Own</title><link rel="canonical" href="/works/12345/chapters/67890?view_full_work=true"></head><body>
<div id="preface">
  <p class="message"><a href="https://evil.example/?next=https://archiveofourown.org/works/999">fake</a></p>
  <div class="meta"><h1>Metadata</h1><div class="byline">by <a rel="author" href="https://evil.example/?u=/users/fake">author</a></div></div>
  <blockquote class="userstuff"><p onclick="bad()">Summary <strong>safe</strong><svg><script>bad()</script></svg><template>bad</template></p></blockquote>
  <blockquote class="userstuff"><form><p>bad</p></form><p><a href="javascript:bad">Notes</a></p></blockquote>
</div>
<div id="chapters" class="userstuff"><p>Body text.</p></div>
<div id="afterword"><blockquote><math><mtext>bad</mtext></math><p style="text-align: right" onload="bad()">End.</p></blockquote></div>
</body></html>`

	parsed, err := parseAO3HTML(html)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Meta.AuthorURL != "" {
		t.Fatalf("external author URL = %q", parsed.Meta.AuthorURL)
	}
	if parsed.Meta.WorkIDGuess != "12345" || parsed.Meta.WorkURLGuess != "/works/12345" {
		t.Fatalf("work guess = %q, %q", parsed.Meta.WorkIDGuess, parsed.Meta.WorkURLGuess)
	}
	metadata := parsed.Meta.Summary + parsed.Meta.Notes + parsed.Meta.Endnotes
	for _, forbidden := range []string{"onclick", "onload", "script", "svg", "template", "form", "math", "javascript:"} {
		if strings.Contains(strings.ToLower(metadata), forbidden) {
			t.Fatalf("metadata retained %q: %q", forbidden, metadata)
		}
	}
	if parsed.Meta.Summary != `<p>Summary <strong>safe</strong></p>` {
		t.Fatalf("summary = %q", parsed.Meta.Summary)
	}
	if parsed.Meta.Notes != `<p><a>Notes</a></p>` {
		t.Fatalf("notes = %q", parsed.Meta.Notes)
	}
	if parsed.Meta.Endnotes != `<p style="text-align: right">End.</p>` {
		t.Fatalf("endnotes = %q", parsed.Meta.Endnotes)
	}
}

func TestParseAO3HTMLDoesNotTrustSummaryWorkLinks(t *testing.T) {
	html := `<!doctype html><html><head>
<title>Linked Works - Archive of Our Own</title>
<link rel="canonical" href="https://archiveofourown.org/works/12345">
</head><body>
<div id="preface">
  <div class="meta"><h1>Linked Works</h1></div>
  <blockquote class="userstuff"><p>Inspired by <a href="/works/99999">another work</a>.</p></blockquote>
  <p class="message">Posted originally on the <a href="https://archiveofourown.org/">Archive of Our Own</a> at <a href="/works/88888">an untrusted conflicting message</a>.</p>
</div>
<div id="chapters" class="userstuff"><p>Body text.</p></div>
</body></html>`

	parsed, err := parseAO3HTML(html)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Meta.WorkIDGuess != "12345" || parsed.Meta.WorkURLGuess != "https://archiveofourown.org/works/12345" {
		t.Fatalf("trusted work identity was hijacked: %q, %q", parsed.Meta.WorkIDGuess, parsed.Meta.WorkURLGuess)
	}
}

func TestSanitizeAO3MetadataURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		kind ao3URLKind
		want string
		id   string
		ok   bool
	}{
		{name: "absolute author", raw: "https://archiveofourown.org/users/writer/pseuds/Pen%20Name", kind: ao3AuthorURL, want: "https://archiveofourown.org/users/writer/pseuds/Pen%20Name", ok: true},
		{name: "relative author", raw: "/users/writer", kind: ao3AuthorURL, want: "/users/writer", ok: true},
		{name: "work chapter canonicalized", raw: "https://archiveofourown.org/works/123/chapters/456?view_adult=true", kind: ao3WorkURL, want: "https://archiveofourown.org/works/123", id: "123", ok: true},
		{name: "query forgery", raw: "https://evil.example/?next=https://archiveofourown.org/works/123", kind: ao3WorkURL},
		{name: "host suffix", raw: "https://archiveofourown.org.evil.example/works/123", kind: ao3WorkURL},
		{name: "userinfo", raw: "https://archiveofourown.org@evil.example/works/123", kind: ao3WorkURL},
		{name: "network relative", raw: "//evil.example/works/123", kind: ao3WorkURL},
		{name: "insecure scheme", raw: "http://archiveofourown.org/works/123", kind: ao3WorkURL},
		{name: "encoded slash", raw: "/users/writer%2fpseuds/fake", kind: ao3AuthorURL},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, id, ok := sanitizeAO3MetadataURL(tt.raw, tt.kind)
			if got != tt.want || id != tt.id || ok != tt.ok {
				t.Fatalf("sanitizeAO3MetadataURL() = %q, %q, %v; want %q, %q, %v", got, id, ok, tt.want, tt.id, tt.ok)
			}
		})
	}
}

func TestParseAO3HTMLRejectsInvalidDocuments(t *testing.T) {
	tests := []struct {
		name    string
		html    string
		message string
	}{
		{name: "empty", html: " \n\t", message: "empty"},
		{name: "login", html: `<html><head><title>Log In | Archive of Our Own</title></head><body><form id="new_user"></form></body></html>`, message: "login"},
		{name: "challenge", html: `<html><head><title>Just a moment...</title></head><body><form id="challenge-form"></form></body></html>`, message: "challenge"},
		{name: "deleted", html: `<html><head><title>Error | Archive of Our Own</title></head><body><p>This work has been deleted.</p></body></html>`, message: "deleted"},
		{name: "permission", html: `<html><head><title>Restricted | Archive of Our Own</title></head><body><p>This work is only available to registered users.</p></body></html>`, message: "permission"},
		{name: "non AO3", html: `<html><head><title>Personal export</title></head><body><div id="preface"><h1>Fake</h1></div><div id="chapters" class="userstuff"><p>Body.</p></div></body></html>`, message: "not a recognized AO3"},
		{name: "missing preface", html: `<html><head><title>Work - Archive of Our Own</title></head><body><div id="chapters" class="userstuff"><p>Body.</p></div></body></html>`, message: "#preface"},
		{name: "missing chapters", html: `<html><head><title>Work - Archive of Our Own</title></head><body><div id="preface"><div class="meta"><h1>Work</h1></div></div></body></html>`, message: "#chapters"},
		{name: "empty chapter", html: `<html><head><title>Work - Archive of Our Own</title></head><body><div id="preface"><div class="meta"><h1>Work</h1></div></div><div id="chapters" class="userstuff"><p>&nbsp;&#x200b;</p><hr><svg><text>hidden</text></svg></div></body></html>`, message: "meaningful body"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseAO3HTML(tt.html)
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tt.message)) {
				t.Fatalf("parseAO3HTML() error = %v, want containing %q", err, tt.message)
			}
		})
	}
}

func TestParseAO3HTMLBlockIDsAreStableAndUnique(t *testing.T) {
	html := `<!doctype html><html><head><title>IDs - Archive of Our Own</title></head><body>
<div id="preface"><div class="meta"><h1>IDs</h1></div></div>
<div id="chapters" class="userstuff"><p>Repeated.</p><p>Repeated.</p><p>Different.</p></div>
</body></html>`
	first, err := parseAO3HTML(html)
	if err != nil {
		t.Fatal(err)
	}
	second, err := parseAO3HTML(html)
	if err != nil {
		t.Fatal(err)
	}
	firstBlocks := first.Original.Chapters[0].Blocks
	secondBlocks := second.Original.Chapters[0].Blocks
	seen := map[string]bool{}
	for i, block := range firstBlocks {
		if len(block.ID) != 32 {
			t.Fatalf("block %d ID length = %d: %q", i, len(block.ID), block.ID)
		}
		if _, err := hex.DecodeString(block.ID); err != nil {
			t.Fatalf("block %d ID is not hex: %q", i, block.ID)
		}
		if seen[block.ID] {
			t.Fatalf("duplicate block ID %q", block.ID)
		}
		seen[block.ID] = true
		if secondBlocks[i].ID != block.ID {
			t.Fatalf("block %d ID changed: %q != %q", i, block.ID, secondBlocks[i].ID)
		}
	}
}

func TestParseAO3ReferenceHTML(t *testing.T) {
	raw, err := os.ReadFile("../../reference/Broken_Boy-zh-CN-dual-reader.html")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parseAO3HTML(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Meta.WorkIDGuess != "81921681" || len(parsed.Original.Chapters) == 0 || len(parsed.Original.Chapters[0].Blocks) == 0 {
		t.Fatalf("reference parse result is incomplete: work=%q chapters=%d", parsed.Meta.WorkIDGuess, len(parsed.Original.Chapters))
	}
}
