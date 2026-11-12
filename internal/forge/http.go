package forge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const maxResponse = 8 << 20

// APIError includes only masked response text. Callers may inspect status to
// recover duplicate opens, authentication failures and stale merge pins.
type RequestError struct{ Message string }

func (e *RequestError) Error() string { return e.Message }

type APIError struct {
	Status  int
	Message string
	RetryAt time.Time
}

func (e *APIError) Error() string { return fmt.Sprintf("forge HTTP %d: %s", e.Status, e.Message) }

// HTTP binds credentials to one HTTPS API origin. Redirects are never followed.
// Its ETag cache is private to this instance and therefore to this token.
type HTTP struct {
	base   *url.URL
	token  Token
	client http.Client
	mask   Masker
	mu     sync.Mutex
	cache  map[string]cached
	until  time.Time
}
type cached struct {
	etag string
	body []byte
}

func NewHTTP(apiURL string, token Token, client *http.Client, otherTokens ...Token) (*HTTP, error) {
	u, err := url.Parse(apiURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("forge API requires an HTTPS URL without credentials, query or fragment")
	}
	c := http.Client{Timeout: 30 * time.Second}
	if client != nil {
		c = *client
		if c.Timeout == 0 {
			c.Timeout = 30 * time.Second
		}
	}
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &HTTP{base: u, token: token, client: c, mask: NewMasker(append(otherTokens, token)...), cache: map[string]cached{}}, nil
}
func (h *HTTP) Mask(text string) string { return h.mask.Text(text) }
func (h *HTTP) RetryAt() time.Time      { h.mu.Lock(); defer h.mu.Unlock(); return h.until }

// Do accepts a relative endpoint, not a URL supplied by a response or repository.
func (h *HTTP) Do(ctx context.Context, method, endpoint string, input, output any) (http.Header, error) {
	if !strings.HasPrefix(endpoint, "/") || strings.HasPrefix(endpoint, "//") || strings.ContainsAny(endpoint, "\\\r\n") {
		return nil, fmt.Errorf("invalid forge endpoint")
	}
	rel, err := url.Parse(endpoint)
	if err != nil || rel.IsAbs() || rel.Host != "" || rel.Fragment != "" {
		return nil, fmt.Errorf("invalid forge endpoint")
	}
	u := *h.base
	// Preserve escaped subgroup/project separators in endpoints.
	u.Path = strings.TrimRight(h.base.Path, "/") + rel.Path
	u.RawPath = strings.TrimRight(h.base.EscapedPath(), "/") + rel.EscapedPath()
	u.RawQuery = rel.RawQuery
	var body io.Reader
	if input != nil {
		b, err := json.Marshal(input)
		if err != nil {
			return nil, fmt.Errorf("encoding forge request")
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, fmt.Errorf("creating forge request")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "tuckerbox")
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if h.token.Available() {
		req.Header.Set("Authorization", "Bearer "+h.token.value)
	}
	h.mu.Lock()
	old := h.cache[endpoint]
	until := h.until
	h.mu.Unlock()
	if time.Now().Before(until) {
		return nil, &APIError{Status: 429, Message: "waiting for forge rate limit reset", RetryAt: until}
	}
	if method == http.MethodGet && old.etag != "" {
		req.Header.Set("If-None-Match", old.etag)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return nil, &RequestError{Message: "forge request failed: " + h.Mask(err.Error())}
	}
	defer resp.Body.Close()
	h.recordLimit(resp.Header)
	if resp.StatusCode >= 300 && resp.StatusCode < 400 && resp.StatusCode != 304 {
		return resp.Header, &APIError{Status: resp.StatusCode, Message: "forge redirect refused"}
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil {
		return resp.Header, fmt.Errorf("reading forge response")
	}
	if len(b) > maxResponse {
		return resp.Header, fmt.Errorf("forge response exceeds size limit")
	}
	b = []byte(h.Mask(string(b)))
	if resp.StatusCode == 304 {
		if old.etag == "" {
			return resp.Header, fmt.Errorf("forge returned 304 without cached response")
		}
		b = old.body
	} else if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.Header, &APIError{Status: resp.StatusCode, Message: string(b), RetryAt: h.RetryAt()}
	}
	if method == http.MethodGet && resp.StatusCode != 304 {
		h.mu.Lock()
		h.cache[endpoint] = cached{resp.Header.Get("ETag"), b}
		h.mu.Unlock()
	}
	if output == nil {
		return resp.Header, nil
	}
	if raw, ok := output.(*string); ok {
		*raw = string(b)
		return resp.Header, nil
	}
	if err := json.Unmarshal(b, output); err != nil {
		return resp.Header, fmt.Errorf("invalid JSON in forge response")
	}
	return resp.Header, nil
}
func (h *HTTP) recordLimit(header http.Header) {
	now := time.Now()
	var until time.Time
	if s := header.Get("Retry-After"); s != "" {
		if seconds, err := strconv.Atoi(s); err == nil && seconds >= 0 {
			until = now.Add(time.Duration(seconds) * time.Second)
		} else if t, err := http.ParseTime(s); err == nil {
			until = t
		}
	}
	for _, prefix := range []string{"X-RateLimit-", "RateLimit-"} {
		limit, _ := strconv.ParseFloat(header.Get(prefix+"Limit"), 64)
		left, err := strconv.ParseFloat(header.Get(prefix+"Remaining"), 64)
		if err == nil && limit > 0 && left/limit < 0.1 {
			if reset, err := strconv.ParseInt(header.Get(prefix+"Reset"), 10, 64); err == nil {
				t := time.Unix(reset, 0)
				if t.After(until) {
					until = t
				}
			}
		}
	}
	h.mu.Lock()
	if until.After(h.until) {
		h.until = until
	}
	h.mu.Unlock()
}

// Pages exhausts numbered REST pages without following an untrusted Link URL.
// Decode one page with Do, then return whether a next page was advertised.
func NextPage(header http.Header, page int) bool {
	if next := header.Get("X-Next-Page"); next != "" {
		n, _ := strconv.Atoi(next)
		return n == page+1
	}
	for _, part := range strings.Split(header.Get("Link"), ",") {
		if strings.Contains(part, `rel="next"`) {
			return true
		}
	}
	return false
}

// GraphQL uses GitHub's fixed GraphQL path on the same authenticated origin.
func (h *HTTP) GraphQL(ctx context.Context, input, output any) (http.Header, error) {
	clone := *h.base
	endpoint := "/graphql"
	if strings.TrimRight(clone.Path, "/") == "/api/v3" {
		clone.Path = "/api"
	}
	// Share token, transport and rate limit with REST, without copying the mutex.
	if clone.Path != h.base.Path {
		peer := &HTTP{base: &clone, token: h.token, client: h.client, mask: h.mask, cache: map[string]cached{}, until: h.RetryAt()}
		header, err := peer.Do(ctx, "POST", endpoint, input, output)
		h.mu.Lock()
		if peer.until.After(h.until) {
			h.until = peer.until
		}
		h.mu.Unlock()
		return header, err
	}
	return h.Do(ctx, "POST", endpoint, input, output)
}

// WebProject builds a same-host project link from an escaped project path.
func (h *HTTP) WebProject(project string) string {
	path, err := url.PathUnescape(project)
	if err != nil {
		return ""
	}
	return "https://" + h.base.Host + "/" + path
}

// JobLog handles GitHub's documented signed log redirect. The API request is
// authenticated; the single HTTPS download has no authorization or cookie jar,
// and cannot redirect again. Ordinary API redirects remain forbidden.
func (h *HTTP) JobLog(ctx context.Context, endpoint string) (string, error) {
	var log string
	header, err := h.Do(ctx, "GET", endpoint, nil, &log)
	if err == nil {
		return log, nil
	}
	e, ok := err.(*APIError)
	if !ok || e.Status != 302 {
		return "", err
	}
	location := header.Get("Location")
	u, parseErr := url.Parse(location)
	if parseErr != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" || h.Mask(location) != location {
		return "", fmt.Errorf("unsafe forge log download URL")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return "", fmt.Errorf("invalid log download")
	}
	client := h.client
	client.Jar = nil
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("forge log download failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("forge log download HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil || len(b) > maxResponse {
		return "", fmt.Errorf("forge log download unreadable or exceeds size limit")
	}
	return h.Mask(string(b)), nil
}
