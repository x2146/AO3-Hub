package app

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
)

// assetServer serves the embedded Vite bundle. Vite fingerprints every file
// it emits under assets/, so browsers may keep those for a year without
// asking again. index.html and the files copied from web/public (which must
// stay out of assets/) keep their names across releases, so they are
// revalidated on every use; their content ETag turns that check into a 304
// until an update actually changes them. Text files are gzipped once at
// startup rather than per request.
type assetServer struct {
	fsys  fs.FS
	root  string
	files map[string]bundleFile
}

type bundleFile struct {
	etag     string
	gzip     []byte // nil when gzip does not make the file smaller
	gzipETag string
}

// gzipExtensions are the bundle file types gzip shrinks; images and fonts
// arrive compressed already.
var gzipExtensions = map[string]bool{
	".css": true, ".html": true, ".js": true, ".json": true, ".mjs": true, ".svg": true,
}

func newAssetServer(fsys fs.FS, root string) *assetServer {
	files := map[string]bundleFile{}
	// A file that could not be read here is still served, only without an
	// ETag or a gzip variant.
	_ = fs.WalkDir(fsys, root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return err
		}
		file := bundleFile{etag: contentETag(data)}
		if gzipExtensions[path.Ext(name)] {
			if packed, err := gzipBytes(data); err == nil && len(packed) < len(data) {
				file.gzip = packed
				file.gzipETag = contentETag(packed)
			}
		}
		files[strings.TrimPrefix(name, root+"/")] = file
		return nil
	})
	return &assetServer{fsys: fsys, root: root, files: files}
}

func (s *assetServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	clean := path.Clean("/" + r.URL.Path)
	key := strings.TrimPrefix(clean, "/")
	if key == "" {
		key = "index.html"
	}
	if s.serveFile(w, r, key) {
		return
	}
	if !strings.Contains(path.Base(key), ".") || strings.HasSuffix(key, ".html") {
		if s.serveFile(w, r, "index.html") {
			return
		}
	}
	if _, err := fs.Stat(s.fsys, path.Join(s.root, "index.html")); err != nil {
		http.Error(w, "AO3-Hub server is running. Web bundle is not embedded; run `npm run dev:web` or build the server.", http.StatusNotFound)
		return
	}
	http.Error(w, "not found", http.StatusNotFound)
}

// serveFile serves name, a path relative to the bundle root.
func (s *assetServer) serveFile(w http.ResponseWriter, r *http.Request, name string) bool {
	file, err := s.fsys.Open(path.Join(s.root, name))
	if err != nil {
		return false
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.IsDir() {
		return false
	}
	ext := path.Ext(name)
	if mt := mime.TypeByExtension(ext); mt != "" {
		w.Header().Set("content-type", mt)
	}
	// Decided by the file actually served, so an SPA route under assets/ that
	// falls back to index.html is still revalidated.
	if strings.HasPrefix(name, "assets/") {
		w.Header().Set("cache-control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("cache-control", "no-cache")
	}
	bundled := s.files[name]
	content, etag := file.(io.ReadSeeker), bundled.etag
	if bundled.gzip != nil {
		w.Header().Add("vary", "Accept-Encoding")
		if acceptsGzip(r) {
			w.Header().Set("content-encoding", "gzip")
			// ServeContent leaves the length to the caller once an encoding is
			// set; without it the response would fall back to chunked transfer.
			w.Header().Set("content-length", strconv.Itoa(len(bundled.gzip)))
			content, etag = bytes.NewReader(bundled.gzip), bundled.gzipETag
		}
	}
	if etag != "" {
		w.Header().Set("etag", etag)
	}
	http.ServeContent(w, r, path.Base(name), info.ModTime(), content)
	return true
}

func contentETag(data []byte) string {
	sum := sha256.Sum256(data)
	return `"` + hex.EncodeToString(sum[:16]) + `"`
}

func gzipBytes(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	if _, err := zw.Write(data); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// acceptsGzip reports whether the request's Accept-Encoding lists gzip with
// a non-zero weight. Anything unparseable falls back to the plain file,
// which every client can read.
func acceptsGzip(r *http.Request) bool {
	for _, value := range r.Header.Values("Accept-Encoding") {
		for _, part := range strings.Split(value, ",") {
			coding, params, _ := strings.Cut(part, ";")
			coding = strings.TrimSpace(coding)
			if !strings.EqualFold(coding, "gzip") && !strings.EqualFold(coding, "x-gzip") {
				continue
			}
			name, q, ok := strings.Cut(params, "=")
			if !ok || !strings.EqualFold(strings.TrimSpace(name), "q") {
				return true
			}
			weight, err := strconv.ParseFloat(strings.TrimSpace(q), 64)
			return err == nil && weight > 0
		}
	}
	return false
}
