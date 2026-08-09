package app

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	ao3CallTimeout        = 2 * time.Minute
	maxAO3HTMLBytes int64 = 64 << 20
	maxAO3Redirects       = 10
)

var (
	workIDDirectRE = regexp.MustCompile(`^(\d{5,12})$`)
	downloadHrefRE = regexp.MustCompile(`(?i)href="(/downloads/[^"]+\.html)"`)
	ao3BaseURL     = "https://archiveofourown.org"
	ao3HTTPClient  = &http.Client{
		Transport: newExternalHTTPTransport(),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxAO3Redirects {
				return errors.New("too many AO3 redirects")
			}
			if len(via) == 0 || !sameURLOrigin(req.URL, via[0].URL) {
				return errors.New("AO3 redirect changed origin or transport")
			}
			return nil
		},
	}
	errInvalidAO3WorkURL = errors.New("invalid AO3 work URL")
)

func normalizeAO3WorkURL(input string) (string, string, error) {
	value := strings.TrimSpace(input)
	base, err := parseAO3BaseURL()
	if err != nil {
		return "", "", err
	}
	if workIDDirectRE.MatchString(value) {
		workURL := *base
		workURL.Path = "/works/" + value
		return workURL.String(), value, nil
	}
	if value == "" || hasUnsafeURLChars(value) {
		return "", "", errInvalidAO3WorkURL
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.User != nil || parsed.Fragment != "" {
		return "", "", errInvalidAO3WorkURL
	}
	if !parsed.IsAbs() {
		if parsed.Host != "" || parsed.Opaque != "" || !strings.HasPrefix(value, "/") {
			return "", "", errInvalidAO3WorkURL
		}
		parsed = base.ResolveReference(parsed)
	}
	if !sameURLOrigin(parsed, base) || strings.Contains(parsed.EscapedPath(), "%") {
		return "", "", errInvalidAO3WorkURL
	}
	match := workPathRE.FindStringSubmatch(parsed.Path)
	if len(match) != 2 || !workIDDirectRE.MatchString(match[1]) {
		return "", "", errInvalidAO3WorkURL
	}
	workURL := *base
	workURL.Path = "/works/" + match[1]
	return workURL.String(), match[1], nil
}

func fetchWith(ctx context.Context, rawURL, cookie, userAgent string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("user-agent", userAgent)
	req.Header.Set("cookie", cookie)
	req.Header.Set("accept", "text/html,application/xhtml+xml,*/*;q=0.8")
	req.Header.Set("accept-language", "en-US,en;q=0.8")
	return ao3HTTPClient.Do(req)
}

func (a *App) fetchDownloadHTMLContext(parent context.Context, workID string) (string, error) {
	if !workIDDirectRE.MatchString(workID) {
		return "", errors.New("invalid AO3 work id")
	}
	ctx, cancel := context.WithTimeout(parent, ao3CallTimeout)
	defer cancel()

	cfg, err := a.store.LoadConfig()
	if err != nil {
		return "", err
	}
	cookie := cfg.AO3.Cookie
	if cookie == "" {
		cookie = "view_adults=true;"
	}
	ua := cfg.AO3.UserAgent
	base, err := parseAO3BaseURL()
	if err != nil {
		return "", err
	}
	workURL := *base
	workURL.Path = "/works/" + workID
	query := workURL.Query()
	query.Set("view_adult", "true")
	query.Set("view_full_work", "true")
	workURL.RawQuery = query.Encode()

	res, err := fetchWith(ctx, workURL.String(), cookie, ua)
	if err != nil {
		return "", fmt.Errorf("fetch AO3 work page %s: %w", workID, err)
	}
	defer res.Body.Close()
	body, err := readAO3HTMLResponse(res, base, fmt.Sprintf("AO3 work page %s", workID))
	if err != nil {
		return "", err
	}
	workHTML := string(body)
	if match := downloadHrefRE.FindStringSubmatch(workHTML); len(match) == 2 {
		downloadRef, err := url.Parse(match[1])
		if err != nil {
			return "", fmt.Errorf("parse AO3 download URL: %w", err)
		}
		downloadURL := base.ResolveReference(downloadRef)
		if !sameURLOrigin(downloadURL, base) || !strings.HasPrefix(downloadURL.Path, "/downloads/") {
			return "", errors.New("AO3 download URL escaped the expected origin")
		}
		res2, err := fetchWith(ctx, downloadURL.String(), cookie, ua)
		if err != nil {
			return "", fmt.Errorf("fetch AO3 download for work %s: %w", workID, err)
		}
		defer res2.Body.Close()
		body2, err := readAO3HTMLResponse(res2, base, fmt.Sprintf("AO3 download for work %s", workID))
		if err != nil {
			return "", err
		}
		return string(body2), nil
	}
	return workHTML, nil
}

func parseAO3BaseURL() (*url.URL, error) {
	base, err := url.Parse(ao3BaseURL)
	if err != nil || base.Scheme != "https" || base.Host == "" || base.User != nil {
		return nil, errors.New("invalid AO3 HTTPS base URL")
	}
	return &url.URL{Scheme: base.Scheme, Host: base.Host}, nil
}

func readAO3HTMLResponse(res *http.Response, base *url.URL, label string) ([]byte, error) {
	if res.Request == nil || !sameURLOrigin(res.Request.URL, base) {
		return nil, fmt.Errorf("%s ended at an unexpected origin", label)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("%s returned %d", label, res.StatusCode)
	}
	mediaType, _, err := mime.ParseMediaType(res.Header.Get("content-type"))
	if err != nil || mediaType != "text/html" && mediaType != "application/xhtml+xml" {
		return nil, fmt.Errorf("%s returned unexpected content type %q", label, res.Header.Get("content-type"))
	}
	return readBoundedResponse(res, maxAO3HTMLBytes, label)
}
