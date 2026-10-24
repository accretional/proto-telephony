// Package carriers has the pieces shared by the carrier-list downloaders in
// its subpackages (itu, imsiadmin): an HTTP client with retries, metrics and
// an on-disk cache, and console progress reporting. Output to SQLite or CSV
// lives in the store subpackage.
package carriers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

// UserAgent identifies these tools. The "Mozilla/5.0 (compatible; ...)" form
// matters: some sites (ITU's ASP.NET pages) render fields only for
// browser-like agents.
const UserAgent = "Mozilla/5.0 (compatible; proto-telephony/1; +https://github.com/accretional/proto-telephony)"

// ErrNotFound is returned for HTTP 404.
var ErrNotFound = errors.New("not found")

// Stats are cumulative HTTP/cache metrics, safe for concurrent use.
type Stats struct {
	Requests   atomic.Int64
	Bytes      atomic.Int64
	CacheHits  atomic.Int64
	CacheBytes atomic.Int64
	Retries    atomic.Int64
	NotFound   atomic.Int64
}

// Client fetches documents with retries, keeping an optional disk cache.
type Client struct {
	HTTP     *http.Client
	Base     string // site root, e.g. https://www.itu.int
	CacheDir string // "" disables the cache
	Offline  bool   // serve only from cache
	Stats    Stats
}

// NewClient returns a client for the site at base, caching under cacheDir.
func NewClient(base, cacheDir string) *Client {
	jar, _ := cookiejar.New(nil)
	return &Client{
		HTTP:     &http.Client{Timeout: 2 * time.Minute, Jar: jar},
		Base:     base,
		CacheDir: cacheDir,
	}
}

// Source records where a document came from.
type Source struct {
	Kind      string // e.g. "e212b", "ob", "hni"
	Name      string // e.g. "T-SP-OB.1305-2024"
	URL       string
	Bytes     int64
	SHA256    string
	FromCache bool
	Elapsed   time.Duration
}

// Finish fills in a source's size, hash and elapsed time.
func Finish(s Source, data []byte, start time.Time) Source {
	sum := sha256.Sum256(data)
	s.Bytes, s.SHA256, s.Elapsed = int64(len(data)), hex.EncodeToString(sum[:]), time.Since(start)
	return s
}

// Get fetches a URL with retries (5xx, 429, network errors). 404 → ErrNotFound.
func (c *Client) Get(ctx context.Context, u string) ([]byte, error) {
	return c.GetHeader(ctx, u, nil)
}

// GetHeader is Get that also returns selected response headers in hdr.
func (c *Client) GetHeader(ctx context.Context, u string, hdr http.Header) ([]byte, error) {
	if c.Offline {
		return nil, fmt.Errorf("offline: cannot fetch %s", u)
	}
	var last error
	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			c.Stats.Retries.Add(1)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(1<<attempt) * time.Second):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", UserAgent)
		body, resp, err := c.do(req)
		if err != nil {
			last = err
			continue
		}
		switch status := resp.StatusCode; {
		case status == http.StatusNotFound:
			c.Stats.NotFound.Add(1)
			return nil, fmt.Errorf("%s: %w", u, ErrNotFound)
		case status >= 500 || status == http.StatusTooManyRequests:
			last = fmt.Errorf("%s: HTTP %d", u, status)
			continue
		case status != http.StatusOK:
			return nil, fmt.Errorf("%s: HTTP %d", u, status)
		}
		for k := range hdr {
			hdr[k] = resp.Header.Values(k)
		}
		return body, nil
	}
	return nil, last
}

// PostForm posts a form once (no retries: posts may not be idempotent).
func (c *Client) PostForm(ctx context.Context, u string, form url.Values) ([]byte, error) {
	if c.Offline {
		return nil, fmt.Errorf("offline: cannot post to %s", u)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	body, resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", u, resp.StatusCode)
	}
	return body, nil
}

func (c *Client) do(req *http.Request) ([]byte, *http.Response, error) {
	c.Stats.Requests.Add(1)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	c.Stats.Bytes.Add(int64(len(body)))
	return body, resp, err
}

// CachePath is where a cached document of kind/name lives ("" without a cache).
func (c *Client) CachePath(kind, name string) string {
	if c.CacheDir == "" {
		return ""
	}
	return filepath.Join(c.CacheDir, kind, name)
}

// ReadCache returns a cached document, counting the hit.
func (c *Client) ReadCache(kind, name string) ([]byte, bool) {
	path := c.CachePath(kind, name)
	if path == "" {
		return nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	c.Stats.CacheHits.Add(1)
	c.Stats.CacheBytes.Add(int64(len(data)))
	return data, true
}

// ReadCacheQuiet returns a cached file without counting it as a hit (for
// bookkeeping files stored next to documents).
func (c *Client) ReadCacheQuiet(kind, name string) ([]byte, bool) {
	path := c.CachePath(kind, name)
	if path == "" {
		return nil, false
	}
	data, err := os.ReadFile(path)
	return data, err == nil
}

// WriteCache stores a document (atomically; errors are ignored, the cache is
// only an optimization).
func (c *Client) WriteCache(kind, name string, data []byte) {
	path := c.CachePath(kind, name)
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	if os.WriteFile(path+".tmp", data, 0o644) == nil {
		_ = os.Rename(path+".tmp", path)
	}
}

// Cached returns kind/name from the cache, or fetches u and caches it. Use
// only for documents that never change once published.
func (c *Client) Cached(ctx context.Context, kind, name, u string) ([]byte, Source, error) {
	start := time.Now()
	src := Source{Kind: kind, Name: name, URL: u}
	if data, ok := c.ReadCache(kind, name); ok {
		src.FromCache = true
		return data, Finish(src, data, start), nil
	}
	data, err := c.Get(ctx, u)
	if err != nil {
		return nil, src, err
	}
	c.WriteCache(kind, name, data)
	return data, Finish(src, data, start), nil
}

// Fresh fetches u and refreshes the cached copy; offline, it returns the
// cached copy. Use for documents that can change (index pages, listings).
func (c *Client) Fresh(ctx context.Context, kind, name, u string) ([]byte, Source, error) {
	start := time.Now()
	src := Source{Kind: kind, Name: name, URL: u}
	if c.Offline {
		data, ok := c.ReadCache(kind, name)
		if !ok {
			return nil, src, fmt.Errorf("offline: %s/%s not cached", kind, name)
		}
		src.FromCache = true
		return data, Finish(src, data, start), nil
	}
	data, err := c.Get(ctx, u)
	if err != nil {
		return nil, src, err
	}
	c.WriteCache(kind, name, data)
	return data, Finish(src, data, start), nil
}

// LatestCached returns the lexically greatest re match among cached file
// names of kind.
func (c *Client) LatestCached(kind string, re *regexp.Regexp) (string, error) {
	ents, _ := os.ReadDir(filepath.Join(c.CacheDir, kind))
	var names []string
	for _, e := range ents {
		if m := re.FindString(e.Name()); m != "" {
			names = append(names, m)
		}
	}
	if len(names) == 0 {
		return "", fmt.Errorf("offline: nothing cached under %s", kind)
	}
	sort.Strings(names)
	return names[len(names)-1], nil
}

// FmtBytes formats a byte count for display.
func FmtBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

// NormSpace collapses runs of whitespace (including NBSP) to one space.
func NormSpace(s string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(s, " ", " ")), " ")
}
