package carrier

import (
	"archive/zip"
	"bytes"
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
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// BaseURL is ITU's web root.
const BaseURL = "https://www.itu.int"

// UserAgent identifies this tool to ITU. The "Mozilla/5.0 (compatible; ...)"
// form matters: ITU's ASP.NET pages render some fields (e.g. the 901
// database's row total) only for browser-like agents.
const UserAgent = "Mozilla/5.0 (compatible; proto-telephony-itu-e212/1; +https://github.com/accretional/proto-telephony)"

// ErrNotFound is returned when ITU has no such publication.
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

// Client fetches ITU publications, caching immutable ones on disk.
type Client struct {
	HTTP     *http.Client
	Base     string // defaults to BaseURL
	CacheDir string // "" disables the cache
	Offline  bool   // serve only from cache
	Stats    Stats
}

// NewClient returns a client caching under cacheDir.
func NewClient(cacheDir string) *Client {
	jar, _ := cookiejar.New(nil)
	return &Client{
		HTTP:     &http.Client{Timeout: 2 * time.Minute, Jar: jar},
		Base:     BaseURL,
		CacheDir: cacheDir,
	}
}

// Source records where a document came from.
type Source struct {
	Kind      string // "e212b", "ob", "shared901"
	Name      string // e.g. "T-SP-E.212B-2023", "T-SP-OB.1305-2024"
	URL       string
	Bytes     int64
	SHA256    string
	FromCache bool
	Elapsed   time.Duration
}

func (c *Client) base() string {
	if c.Base == "" {
		return BaseURL
	}
	return c.Base
}

// get fetches a URL with retries (5xx, network errors). 404 → ErrNotFound.
func (c *Client) get(ctx context.Context, u string) ([]byte, error) {
	if c.Offline {
		return nil, fmt.Errorf("offline: %s not cached", u)
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
		body, status, err := c.do(req)
		if err != nil {
			last = err
			continue
		}
		switch {
		case status == http.StatusNotFound:
			c.Stats.NotFound.Add(1)
			return nil, fmt.Errorf("%s: %w", u, ErrNotFound)
		case status >= 500 || status == http.StatusTooManyRequests:
			last = fmt.Errorf("%s: HTTP %d", u, status)
			continue
		case status != http.StatusOK:
			return nil, fmt.Errorf("%s: HTTP %d", u, status)
		}
		return body, nil
	}
	return nil, last
}

func (c *Client) do(req *http.Request) ([]byte, int, error) {
	c.Stats.Requests.Add(1)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	c.Stats.Bytes.Add(int64(len(body)))
	return body, resp.StatusCode, err
}

// cached returns the cached file name, or fetches u and stores it. Use only
// for immutable documents.
func (c *Client) cached(ctx context.Context, kind, name, u string) ([]byte, Source, error) {
	start := time.Now()
	src := Source{Kind: kind, Name: name, URL: u}
	path := ""
	if c.CacheDir != "" {
		path = filepath.Join(c.CacheDir, kind, name+filepath.Ext(u))
		if data, err := os.ReadFile(path); err == nil {
			c.Stats.CacheHits.Add(1)
			c.Stats.CacheBytes.Add(int64(len(data)))
			src.FromCache = true
			return data, finish(src, data, start), nil
		}
	}
	data, err := c.get(ctx, u)
	if err != nil {
		return nil, src, err
	}
	if path != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err == nil {
			tmp := path + ".tmp"
			if os.WriteFile(tmp, data, 0o644) == nil {
				_ = os.Rename(tmp, path)
			}
		}
	}
	return data, finish(src, data, start), nil
}

func finish(s Source, data []byte, start time.Time) Source {
	sum := sha256.Sum256(data)
	s.Bytes, s.SHA256, s.Elapsed = int64(len(data)), hex.EncodeToString(sum[:]), time.Since(start)
	return s
}

var (
	e212bEditionRE = regexp.MustCompile(`T-SP-E\.212B-(\d{4})`)
	obListedRE     = regexp.MustCompile(`T-SP-OB\.(\d+)-(\d{4})`)
)

// LatestBaseEdition finds the newest E.212B (MNC list) edition, e.g. "T-SP-E.212B-2023".
func (c *Client) LatestBaseEdition(ctx context.Context) (string, error) {
	page, err := c.get(ctx, c.base()+"/pub/T-SP-E.212B")
	if err != nil {
		if c.Offline {
			return c.latestCached("e212b", e212bEditionRE)
		}
		return "", err
	}
	best := 0
	for _, m := range e212bEditionRE.FindAllSubmatch(page, -1) {
		if y, _ := strconv.Atoi(string(m[1])); y > best {
			best = y
		}
	}
	if best == 0 {
		return "", fmt.Errorf("no T-SP-E.212B editions listed at %s/pub/T-SP-E.212B", c.base())
	}
	return fmt.Sprintf("T-SP-E.212B-%d", best), nil
}

func (c *Client) latestCached(kind string, re *regexp.Regexp) (string, error) {
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

// FetchBaseList downloads (or reads from cache) the English .docx of an E.212B edition.
func (c *Client) FetchBaseList(ctx context.Context, edition string) (*Doc, Source, error) {
	u := fmt.Sprintf("%s/dms_pub/itu-t/opb/sp/%s-MSW-E.docx", c.base(), edition)
	data, src, err := c.cached(ctx, "e212b", edition, u)
	if err != nil {
		return nil, src, err
	}
	doc, err := ReadDocx(data)
	return doc, src, err
}

// LatestOB returns the highest Operational Bulletin number ITU lists, and its year.
func (c *Client) LatestOB(ctx context.Context) (no, year int, err error) {
	page, err := c.get(ctx, c.base()+"/pub/T-SP-OB")
	if err != nil {
		if !c.Offline {
			return 0, 0, err
		}
		name, cerr := c.latestCached("ob", obListedRE)
		if cerr != nil {
			return 0, 0, cerr
		}
		page = []byte(name)
	}
	for _, m := range obListedRE.FindAllSubmatch(page, -1) {
		n, _ := strconv.Atoi(string(m[1]))
		y, _ := strconv.Atoi(string(m[2]))
		if n > no {
			no, year = n, y
		}
	}
	if no == 0 {
		return 0, 0, fmt.Errorf("no Operational Bulletins listed at %s/pub/T-SP-OB", c.base())
	}
	return no, year, nil
}

var obFileRE = regexp.MustCompile(`href="(/dms_pub/itu-t/opb/sp/T-SP-OB\.\d+-\d{4}-OAS-(?:MSW-E\.docx|ZIP-E\.zip))"`)

// FetchOB downloads Operational Bulletin no (English .docx, unpacking the zip
// edition when that's all there is). ITU's URLs carry the publication year,
// which isn't derivable from the number, so the years in yearHint order are
// tried.
func (c *Client) FetchOB(ctx context.Context, no int, yearHint []int) (*Doc, Source, error) {
	// Cached under its full name; any year will do.
	if c.CacheDir != "" {
		matches, _ := filepath.Glob(filepath.Join(c.CacheDir, "ob", fmt.Sprintf("T-SP-OB.%d-*.docx", no)))
		if len(matches) > 0 {
			start := time.Now()
			data, err := os.ReadFile(matches[0])
			if err == nil {
				c.Stats.CacheHits.Add(1)
				c.Stats.CacheBytes.Add(int64(len(data)))
				name := strings.TrimSuffix(filepath.Base(matches[0]), ".docx")
				src := finish(Source{Kind: "ob", Name: name, FromCache: true,
					URL: fmt.Sprintf("%s/pub/%s", c.base(), name)}, data, start)
				doc, err := ReadDocx(data)
				return doc, src, err
			}
		}
	}
	for _, y := range yearHint {
		name := fmt.Sprintf("T-SP-OB.%d-%d", no, y)
		page, err := c.get(ctx, fmt.Sprintf("%s/pub/%s", c.base(), name))
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				continue
			}
			return nil, Source{}, err
		}
		m := obFileRE.FindSubmatch(page)
		if m == nil {
			continue // ITU answers unknown publications with a 200 shell page
		}
		href := string(m[1])
		start := time.Now()
		data, err := c.get(ctx, c.base()+href)
		if err != nil {
			return nil, Source{}, err
		}
		if strings.HasSuffix(href, ".zip") {
			if data, err = docxFromZip(data); err != nil {
				return nil, Source{}, fmt.Errorf("%s: %w", name, err)
			}
		}
		if c.CacheDir != "" {
			path := filepath.Join(c.CacheDir, "ob", name+".docx")
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err == nil {
				_ = os.WriteFile(path+".tmp", data, 0o644)
				_ = os.Rename(path+".tmp", path)
			}
		}
		src := finish(Source{Kind: "ob", Name: name, URL: c.base() + href}, data, start)
		doc, err := ReadDocx(data)
		return doc, src, err
	}
	return nil, Source{}, fmt.Errorf("OB %d: %w", no, ErrNotFound)
}

// docxFromZip picks the bulletin itself out of a zip edition (which also
// carries annexes named "Annex-…").
func docxFromZip(data []byte) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	var pick *zip.File
	for _, f := range zr.File {
		n := strings.ToLower(filepath.Base(f.Name))
		if !strings.HasSuffix(n, ".docx") || strings.Contains(n, "annex") {
			continue
		}
		if pick == nil || f.UncompressedSize64 > pick.UncompressedSize64 {
			pick = f
		}
	}
	if pick == nil {
		return nil, errors.New("zip has no bulletin .docx")
	}
	rc, err := pick.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

// postForm posts an ASP.NET form (used for paging the 901 database).
func (c *Client) postForm(ctx context.Context, u string, form url.Values) ([]byte, error) {
	if c.Offline {
		return nil, fmt.Errorf("offline: cannot post to %s", u)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	body, status, err := c.do(req)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", u, status)
	}
	return body, nil
}
