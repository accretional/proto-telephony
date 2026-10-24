package itu

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/accretional/proto-telephony/pkg/carriers"
)

// BaseURL is ITU's web root.
const BaseURL = "https://www.itu.int"

// Client fetches ITU publications. Published bulletins and annexes never
// change, so they are cached for good; index pages are always refetched.
type Client struct {
	*carriers.Client
}

// NewClient returns an ITU client caching under cacheDir ("" disables).
func NewClient(cacheDir string) *Client {
	return &Client{carriers.NewClient(BaseURL, cacheDir)}
}

var (
	e212bEditionRE = regexp.MustCompile(`T-SP-E\.212B-(\d{4})`)
	obListedRE     = regexp.MustCompile(`T-SP-OB\.(\d+)-(\d{4})`)
)

// LatestBaseEdition finds the newest E.212B (MNC list) edition, e.g. "T-SP-E.212B-2023".
func (c *Client) LatestBaseEdition(ctx context.Context) (string, error) {
	page, err := c.Get(ctx, c.Base+"/pub/T-SP-E.212B")
	if err != nil {
		if c.Offline {
			return c.LatestCached("e212b", e212bEditionRE)
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
		return "", fmt.Errorf("no T-SP-E.212B editions listed at %s/pub/T-SP-E.212B", c.Base)
	}
	return fmt.Sprintf("T-SP-E.212B-%d", best), nil
}

// FetchBaseList downloads (or reads from cache) the English .docx of an E.212B edition.
func (c *Client) FetchBaseList(ctx context.Context, edition string) (*Doc, carriers.Source, error) {
	u := fmt.Sprintf("%s/dms_pub/itu-t/opb/sp/%s-MSW-E.docx", c.Base, edition)
	data, src, err := c.Cached(ctx, "e212b", edition+".docx", u)
	if err != nil {
		return nil, src, err
	}
	doc, err := ReadDocx(data)
	return doc, src, err
}

// LatestOB returns the highest Operational Bulletin number ITU lists, and its year.
func (c *Client) LatestOB(ctx context.Context) (no, year int, err error) {
	page, err := c.Get(ctx, c.Base+"/pub/T-SP-OB")
	if err != nil {
		if !c.Offline {
			return 0, 0, err
		}
		name, cerr := c.LatestCached("ob", obListedRE)
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
		return 0, 0, fmt.Errorf("no Operational Bulletins listed at %s/pub/T-SP-OB", c.Base)
	}
	return no, year, nil
}

var obFileRE = regexp.MustCompile(`href="(/dms_pub/itu-t/opb/sp/T-SP-OB\.\d+-\d{4}-OAS-(?:MSW-E\.docx|ZIP-E\.zip))"`)

// FetchOB downloads Operational Bulletin no (English .docx, unpacking the zip
// edition when that's all there is). ITU's URLs carry the publication year,
// which isn't derivable from the number, so the years in yearHint order are
// tried.
func (c *Client) FetchOB(ctx context.Context, no int, yearHint []int) (*Doc, carriers.Source, error) {
	// Cached under its full name; any year will do.
	if dir := c.CachePath("ob", ""); dir != "" {
		if matches, _ := filepath.Glob(filepath.Join(dir, fmt.Sprintf("T-SP-OB.%d-*.docx", no))); len(matches) > 0 {
			start := time.Now()
			name := strings.TrimSuffix(filepath.Base(matches[0]), ".docx")
			if data, ok := c.ReadCache("ob", name+".docx"); ok {
				src := carriers.Finish(carriers.Source{Kind: "ob", Name: name, FromCache: true,
					URL: fmt.Sprintf("%s/pub/%s", c.Base, name)}, data, start)
				doc, err := ReadDocx(data)
				return doc, src, err
			}
		}
	}
	for _, y := range yearHint {
		name := fmt.Sprintf("T-SP-OB.%d-%d", no, y)
		page, err := c.Get(ctx, fmt.Sprintf("%s/pub/%s", c.Base, name))
		if err != nil {
			if errors.Is(err, carriers.ErrNotFound) {
				continue
			}
			return nil, carriers.Source{}, err
		}
		m := obFileRE.FindSubmatch(page)
		if m == nil {
			continue // ITU answers unknown publications with a 200 shell page
		}
		href := string(m[1])
		start := time.Now()
		data, err := c.Get(ctx, c.Base+href)
		if err != nil {
			return nil, carriers.Source{}, err
		}
		if strings.HasSuffix(href, ".zip") {
			if data, err = docxFromZip(data); err != nil {
				return nil, carriers.Source{}, fmt.Errorf("%s: %w", name, err)
			}
		}
		c.WriteCache("ob", name+".docx", data)
		src := carriers.Finish(carriers.Source{Kind: "ob", Name: name, URL: c.Base + href}, data, start)
		doc, err := ReadDocx(data)
		return doc, src, err
	}
	return nil, carriers.Source{}, fmt.Errorf("OB %d: %w", no, carriers.ErrNotFound)
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
