package app

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
)

const testIndexHTML = `<!doctype html><script type="module" src="/assets/index-Ab12Cd34.js"></script>`

// testBundleJS is large and repetitive enough for gzip to pay off.
var testBundleJS = strings.Repeat("console.log(1);\n", 64)

func newTestAssetServer(index string) *assetServer {
	return newAssetServer(fstest.MapFS{
		"web-dist/index.html":                {Data: []byte(index)},
		"web-dist/favicon.svg":               {Data: []byte("<svg/>")},
		"web-dist/assets/index-Ab12Cd34.js":  {Data: []byte(testBundleJS)},
		"web-dist/assets/cover-Ab12Cd34.png": {Data: make([]byte, 1024)},
	}, "web-dist")
}

func requestAsset(handler http.Handler, target string, header http.Header) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, target, nil)
	for name, values := range header {
		request.Header[name] = values
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestAssetServerCachesFingerprintedFilesAndRevalidatesTheRest(t *testing.T) {
	server := newTestAssetServer(testIndexHTML)
	for _, test := range []struct {
		name         string
		target       string
		status       int
		cacheControl string
		body         string
	}{
		{name: "fingerprinted asset", target: "/assets/index-Ab12Cd34.js", status: http.StatusOK, cacheControl: "public, max-age=31536000, immutable", body: testBundleJS},
		{name: "index", target: "/", status: http.StatusOK, cacheControl: "no-cache", body: testIndexHTML},
		{name: "SPA route", target: "/r/story/0", status: http.StatusOK, cacheControl: "no-cache", body: testIndexHTML},
		// The fallback serves index.html, which must not inherit the
		// year-long policy of the directory the URL points into.
		{name: "SPA route under assets", target: "/assets/deep-link", status: http.StatusOK, cacheControl: "no-cache", body: testIndexHTML},
		{name: "public file", target: "/favicon.svg", status: http.StatusOK, cacheControl: "no-cache", body: "<svg/>"},
		// A file from the previous release, requested after an update.
		{name: "asset from a previous release", target: "/assets/index-Old00000.js", status: http.StatusNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := requestAsset(server, test.target, nil)
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d", response.Code, test.status)
			}
			if got := response.Header().Get("Cache-Control"); got != test.cacheControl {
				t.Fatalf("cache control = %q, want %q", got, test.cacheControl)
			}
			if test.status != http.StatusOK {
				return
			}
			if got := response.Body.String(); got != test.body {
				t.Fatalf("body = %q, want %q", got, test.body)
			}
			if response.Header().Get("ETag") == "" {
				t.Fatal("missing ETag")
			}
		})
	}
}

func TestAssetServerRevalidatesIndexByContent(t *testing.T) {
	server := newTestAssetServer(testIndexHTML)
	etag := requestAsset(server, "/", nil).Header().Get("ETag")
	if etag == "" {
		t.Fatal("index.html has no ETag")
	}

	unchanged := requestAsset(server, "/r/story/0", http.Header{"If-None-Match": {etag}})
	if unchanged.Code != http.StatusNotModified || unchanged.Body.Len() != 0 {
		t.Fatalf("revalidation = %d with %d body bytes, want an empty 304", unchanged.Code, unchanged.Body.Len())
	}
	if got := unchanged.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("revalidation cache control = %q, want no-cache", got)
	}

	// An update ships an index.html that points at new fingerprints, so the
	// validator a browser kept from the old release must stop matching.
	updated := newTestAssetServer(strings.ReplaceAll(testIndexHTML, "Ab12Cd34", "Ef56Gh78"))
	response := requestAsset(updated, "/", http.Header{"If-None-Match": {etag}})
	if response.Code != http.StatusOK {
		t.Fatalf("post-update status = %d, want 200", response.Code)
	}
	if got := response.Header().Get("ETag"); got == etag {
		t.Fatalf("post-update ETag = %q, want it to change", got)
	}
}

func TestAssetServerNegotiatesGzip(t *testing.T) {
	server := newTestAssetServer(testIndexHTML)
	const target = "/assets/index-Ab12Cd34.js"

	gzipped := requestAsset(server, target, http.Header{"Accept-Encoding": {"gzip, deflate, br, zstd"}})
	if got := gzipped.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("content encoding = %q, want gzip", got)
	}
	if got := gzipped.Header().Get("Vary"); got != "Accept-Encoding" {
		t.Fatalf("vary = %q, want Accept-Encoding", got)
	}
	if got, want := gzipped.Header().Get("Content-Length"), strconv.Itoa(gzipped.Body.Len()); got != want {
		t.Fatalf("content length = %q, want %q", got, want)
	}
	reader, err := gzip.NewReader(gzipped.Body)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != testBundleJS {
		t.Fatalf("decompressed body = %q, want the original file", body)
	}

	// Caches key on Vary, so the plain reply must carry it too, and the two
	// encodings need distinct validators.
	plain := requestAsset(server, target, nil)
	if got := plain.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("plain content encoding = %q, want none", got)
	}
	if got := plain.Header().Get("Vary"); got != "Accept-Encoding" {
		t.Fatalf("plain vary = %q, want Accept-Encoding", got)
	}
	if plain.Body.String() != testBundleJS {
		t.Fatal("plain body differs from the original file")
	}
	if plain.Header().Get("ETag") == gzipped.Header().Get("ETag") {
		t.Fatal("gzip and plain responses share an ETag")
	}

	refused := requestAsset(server, target, http.Header{"Accept-Encoding": {"gzip;q=0, br"}})
	if got := refused.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("content encoding with gzip;q=0 = %q, want none", got)
	}

	revalidated := requestAsset(server, target, http.Header{
		"Accept-Encoding": {"gzip"},
		"If-None-Match":   {gzipped.Header().Get("ETag")},
	})
	if revalidated.Code != http.StatusNotModified {
		t.Fatalf("gzip revalidation status = %d, want 304", revalidated.Code)
	}
}

func TestAssetServerSkipsGzipWhereItDoesNotHelp(t *testing.T) {
	server := newTestAssetServer(testIndexHTML)
	for _, test := range []struct {
		name   string
		target string
	}{
		// Images ship compressed already, however well these particular
		// bytes would shrink.
		{name: "image", target: "/assets/cover-Ab12Cd34.png"},
		{name: "file smaller than its gzip form", target: "/favicon.svg"},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := requestAsset(server, test.target, http.Header{"Accept-Encoding": {"gzip"}})
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", response.Code)
			}
			if got := response.Header().Get("Content-Encoding"); got != "" {
				t.Fatalf("content encoding = %q, want none", got)
			}
			if got := response.Header().Get("Vary"); got != "" {
				t.Fatalf("vary = %q, want none", got)
			}
		})
	}
}

func TestAcceptsGzip(t *testing.T) {
	for _, test := range []struct {
		name   string
		header string
		want   bool
	}{
		{name: "browser", header: "gzip, deflate, br, zstd", want: true},
		{name: "weighted", header: "br;q=1.0, gzip;q=0.8", want: true},
		{name: "case and alias", header: "X-GZIP", want: true},
		{name: "refused", header: "gzip;q=0", want: false},
		{name: "refused with decimals", header: "br, gzip; q=0.000", want: false},
		{name: "unparseable weight", header: "gzip;q=high", want: false},
		{name: "other codings only", header: "br, zstd", want: false},
		{name: "absent", header: "", want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			if test.header != "" {
				request.Header.Set("Accept-Encoding", test.header)
			}
			if got := acceptsGzip(request); got != test.want {
				t.Fatalf("acceptsGzip(%q) = %v, want %v", test.header, got, test.want)
			}
		})
	}
}
