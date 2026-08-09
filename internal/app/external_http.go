package app

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	externalResponseHeaderTimeout  = 30 * time.Second
	maxExternalResponseHeaderBytes = 1 << 20
	maxExternalConnectionsPerHost  = 32
)

func newExternalHTTPTransport() *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = externalResponseHeaderTimeout
	transport.TLSHandshakeTimeout = 10 * time.Second
	transport.ExpectContinueTimeout = time.Second
	transport.MaxIdleConnsPerHost = 8
	transport.MaxConnsPerHost = maxExternalConnectionsPerHost
	transport.MaxResponseHeaderBytes = maxExternalResponseHeaderBytes
	return transport
}

func readBoundedResponse(res *http.Response, limit int64, label string) ([]byte, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("%s has invalid response limit", label)
	}
	if res.ContentLength > limit {
		return nil, fmt.Errorf("%s exceeds %d bytes", label, limit)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", label, err)
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("%s exceeds %d bytes", label, limit)
	}
	return body, nil
}

func sameURLOrigin(left, right *url.URL) bool {
	if left == nil || right == nil || !strings.EqualFold(left.Scheme, right.Scheme) {
		return false
	}
	return strings.EqualFold(left.Hostname(), right.Hostname()) && normalizedURLPort(left) == normalizedURLPort(right)
}

func normalizedURLPort(value *url.URL) string {
	if port := value.Port(); port != "" {
		return port
	}
	switch strings.ToLower(value.Scheme) {
	case "http":
		return "80"
	case "https":
		return "443"
	default:
		return ""
	}
}
