package app

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

var (
	workIDFromURLRE = regexp.MustCompile(`/works/(\d+)`)
	workIDDirectRE  = regexp.MustCompile(`^(\d{5,12})$`)
	downloadHrefRE  = regexp.MustCompile(`(?i)href="(/downloads/[^"]+\.html)"`)
)

func extractWorkID(input string) string {
	if match := workIDFromURLRE.FindStringSubmatch(input); len(match) == 2 {
		return match[1]
	}
	input = strings.TrimSpace(input)
	if match := workIDDirectRE.FindStringSubmatch(input); len(match) == 2 {
		return match[1]
	}
	return ""
}

func fetchWith(url, cookie, userAgent string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("user-agent", userAgent)
	req.Header.Set("cookie", cookie)
	req.Header.Set("accept", "text/html,application/xhtml+xml,*/*;q=0.8")
	req.Header.Set("accept-language", "en-US,en;q=0.8")
	return http.DefaultClient.Do(req)
}

func (a *App) fetchDownloadHTML(workID string) (string, error) {
	cfg, err := a.store.LoadConfig()
	if err != nil {
		return "", err
	}
	cookie := cfg.AO3.Cookie
	if cookie == "" {
		cookie = "view_adults=true;"
	}
	ua := cfg.AO3.UserAgent
	workURL := fmt.Sprintf("https://archiveofourown.org/works/%s?view_adult=true&view_full_work=true", workID)
	res, err := fetchWith(workURL, cookie, ua)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", fmt.Errorf("AO3 work page %s returned %d", workID, res.StatusCode)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return "", err
	}
	workHTML := string(body)
	if match := downloadHrefRE.FindStringSubmatch(workHTML); len(match) == 2 {
		downloadURL := "https://archiveofourown.org" + match[1]
		res2, err := fetchWith(downloadURL, cookie, ua)
		if err == nil {
			defer res2.Body.Close()
			if res2.StatusCode >= 200 && res2.StatusCode < 300 {
				body2, err := io.ReadAll(res2.Body)
				if err == nil {
					return string(body2), nil
				}
			}
		}
	}
	return workHTML, nil
}
