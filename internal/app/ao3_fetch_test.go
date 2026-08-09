package app

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func newAO3TestApp(t *testing.T) *App {
	t.Helper()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &App{store: store}
}

func setAO3TestServers(t *testing.T, base *httptest.Server, additional ...*httptest.Server) {
	t.Helper()
	roots := x509.NewCertPool()
	roots.AddCert(base.Certificate())
	for _, server := range additional {
		roots.AddCert(server.Certificate())
	}
	transport := newExternalHTTPTransport()
	transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	originalBaseURL := ao3BaseURL
	originalClient := ao3HTTPClient
	client := &http.Client{Transport: transport, CheckRedirect: originalClient.CheckRedirect}
	ao3BaseURL = base.URL
	ao3HTTPClient = client
	t.Cleanup(func() {
		client.CloseIdleConnections()
		ao3HTTPClient = originalClient
		ao3BaseURL = originalBaseURL
	})
}

func TestFetchDownloadHTMLUsesValidatedDownload(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("cookie") == "" {
			t.Error("missing AO3 cookie")
		}
		w.Header().Set("content-type", "text/html; charset=utf-8")
		switch r.URL.Path {
		case "/works/12345":
			_, _ = w.Write([]byte(`<html><a href="/downloads/work.html">download</a></html>`))
		case "/downloads/work.html":
			_, _ = w.Write([]byte("<html>complete work</html>"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	setAO3TestServers(t, server)

	html, err := newAO3TestApp(t).fetchDownloadHTMLContext(context.Background(), "12345")
	if err != nil {
		t.Fatal(err)
	}
	if html != "<html>complete work</html>" {
		t.Fatalf("download HTML = %q", html)
	}
}

func TestFetchDownloadHTMLRejectsOversizedResponses(t *testing.T) {
	for _, stage := range []string{"work", "download"} {
		t.Run(stage, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("content-type", "text/html")
				if stage == "download" && strings.HasPrefix(r.URL.Path, "/works/") {
					_, _ = w.Write([]byte(`<a href="/downloads/work.html">download</a>`))
					return
				}
				w.Header().Set("content-length", strconv.FormatInt(maxAO3HTMLBytes+1, 10))
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()
			setAO3TestServers(t, server)

			_, err := newAO3TestApp(t).fetchDownloadHTMLContext(context.Background(), "12345")
			if err == nil || !strings.Contains(err.Error(), "exceeds") {
				t.Fatalf("oversized %s error = %v", stage, err)
			}
		})
	}
}

func TestFetchDownloadHTMLReportsDownloadFailure(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/works/12345":
			w.Header().Set("content-type", "text/html")
			_, _ = w.Write([]byte(`<a href="/downloads/work.html">download</a>`))
		case "/downloads/work.html":
			w.WriteHeader(http.StatusBadGateway)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	setAO3TestServers(t, server)

	_, err := newAO3TestApp(t).fetchDownloadHTMLContext(context.Background(), "12345")
	if err == nil || !strings.Contains(err.Error(), "AO3 download") || !strings.Contains(err.Error(), "502") {
		t.Fatalf("download failure = %v", err)
	}
}

func TestFetchDownloadHTMLRejectsUnexpectedContentType(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"not":"html"}`))
	}))
	defer server.Close()
	setAO3TestServers(t, server)

	_, err := newAO3TestApp(t).fetchDownloadHTMLContext(context.Background(), "12345")
	if err == nil || !strings.Contains(err.Error(), "content type") {
		t.Fatalf("content-type error = %v", err)
	}
}

func TestAO3CookieIsNotForwardedAcrossRedirect(t *testing.T) {
	received := make(chan string, 1)
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- r.Header.Get("cookie")
		w.Header().Set("content-type", "text/html")
		_, _ = w.Write([]byte("<html>unexpected</html>"))
	}))
	defer target.Close()
	source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/stolen", http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	setAO3TestServers(t, source, target)

	_, err := newAO3TestApp(t).fetchDownloadHTMLContext(context.Background(), "12345")
	if err == nil || !strings.Contains(err.Error(), "redirect changed origin") {
		t.Fatalf("redirect error = %v", err)
	}
	select {
	case cookie := <-received:
		t.Fatalf("redirect target received cookie %q", cookie)
	default:
	}
}

func TestFetchDownloadHTMLHonorsCancellation(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	setAO3TestServers(t, server)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := newAO3TestApp(t).fetchDownloadHTMLContext(ctx, "12345")
	<-started
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancellation error = %v", err)
	}
}
