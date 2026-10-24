package imsiadmin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/accretional/proto-telephony/pkg/carriers"
)

// BaseURL is IMSI Admin's site.
const BaseURL = "https://imsiadmin.com"

// Client fetches IMSI Admin pages.
//
// Directories and the modification index are refetched every run. Detail
// pages are cached along with the post's last-modified time from the
// site's WordPress REST API, and refetched only when that changes.
type Client struct {
	*carriers.Client
}

// NewClient returns a client caching under cacheDir ("" disables).
func NewClient(cacheDir string) *Client {
	return &Client{carriers.NewClient(BaseURL, cacheDir)}
}

// postType is the WordPress post type behind each directory.
func postType(k Kind) string { return string(k) + "-codes" }

// Directory fetches and parses the HNI or CBRS directory.
func (c *Client) Directory(ctx context.Context, k Kind) ([]Entry, carriers.Source, error) {
	page, src, err := c.Fresh(ctx, "directory", string(k)+".html", fmt.Sprintf("%s/assignments/%s/", c.Base, k))
	if err != nil {
		return nil, src, err
	}
	src.Kind, src.Name = "directory", string(k)
	entries, err := ParseDirectory(k, page)
	return entries, src, err
}

const perPage = 100

// Modified returns each detail page's last-modified time (keyed by page
// URL) from the REST API, which lists posts 100 at a time.
func (c *Client) Modified(ctx context.Context, k Kind) (map[string]string, []carriers.Source, error) {
	out := map[string]string{}
	var srcs []carriers.Source
	for p := 1; ; p++ {
		u := fmt.Sprintf("%s/wp-json/wp/v2/%s?per_page=%d&page=%d&orderby=id&order=asc&_fields=link,modified_gmt",
			c.Base, postType(k), perPage, p)
		data, src, err := c.Fresh(ctx, "api", fmt.Sprintf("%s-%d.json", postType(k), p), u)
		if err != nil {
			return out, srcs, err
		}
		src.Kind, src.Name = "api", fmt.Sprintf("%s page %d", postType(k), p)
		srcs = append(srcs, src)
		var posts []struct {
			Link     string `json:"link"`
			Modified string `json:"modified_gmt"`
		}
		if err := json.Unmarshal(data, &posts); err != nil {
			return out, srcs, fmt.Errorf("%s: %w", u, err)
		}
		for _, p := range posts {
			out[p.Link] = p.Modified
		}
		if len(posts) < perPage {
			return out, srcs, nil
		}
	}
}

// slug is a detail page's cache name: the last path segment of its URL.
func slug(u string) string {
	pu, err := url.Parse(u)
	if err != nil {
		return strings.NewReplacer("/", "_", ":", "_").Replace(u)
	}
	return path.Base(strings.TrimSuffix(pu.Path, "/"))
}

// Detail fetches a code's detail page. modified is the post's last-modified
// time ("" if unknown, which always refetches online); a cached copy saved
// with the same time is reused.
func (c *Client) Detail(ctx context.Context, e Entry, modified string) ([]byte, carriers.Source, error) {
	start := time.Now()
	kind := "detail-" + string(e.Kind)
	name := slug(e.URL)
	src := carriers.Source{Kind: kind, Name: e.Code(), URL: e.URL}
	if c.Offline || modified != "" {
		stamp, _ := c.ReadCacheQuiet(kind, name+".modified")
		if c.Offline || string(stamp) == modified {
			if data, ok := c.ReadCache(kind, name+".html"); ok {
				src.FromCache = true
				return data, carriers.Finish(src, data, start), nil
			}
		}
		if c.Offline {
			return nil, src, fmt.Errorf("offline: %s not cached", e.URL)
		}
	}
	data, err := c.Get(ctx, e.URL)
	if err != nil {
		return nil, src, err
	}
	c.WriteCache(kind, name+".html", data)
	c.WriteCache(kind, name+".modified", []byte(modified))
	return data, carriers.Finish(src, data, start), nil
}
