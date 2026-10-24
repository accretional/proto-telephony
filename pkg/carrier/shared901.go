package carrier

import (
	"context"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// SharedCode is a row of ITU's live database of shared MCC 901 codes, which
// (unlike the bulletin list) also tracks reservations and reclaimed codes.
type SharedCode struct {
	MCC, MNC         string
	Assignee         string
	Status           string // ASSIGNED, RESERVED, RECLAIMED, ...
	ReservationStart string // YYYY-MM-DD or ""
	ReservationEnd   string
	Assigned         string
	Reclaimed        string
}

// Shared901Path is the public 901 database page.
const Shared901Path = "/net/ITU-T/inrdb/e212_901.aspx"

var (
	gridSpanRE = regexp.MustCompile(`(?s)id="ContentPlaceHolder1_GridView1_(lbl\w+?)_(\d+)"[^>]*>(.*?)</span>`)
	hiddenRE   = regexp.MustCompile(`<input type="hidden" name="([^"]+)" id="[^"]*" value="([^"]*)"`)
	totalRE    = regexp.MustCompile(`id="ContentPlaceHolder1_lbl_total"[^>]*>(\d+)<`)
	pageLinkRE = regexp.MustCompile(`Page\$(\d+)`)
	tagRE      = regexp.MustCompile(`<[^>]+>`)
)

// FetchShared901 reads every page of the 901 database. progress, if set, is
// called after each page.
func (c *Client) FetchShared901(ctx context.Context, progress func(page, rows, total int)) ([]SharedCode, Source, error) {
	start := time.Now()
	u := c.base() + Shared901Path
	src := Source{Kind: "shared901", Name: "e212_901", URL: u}
	page, err := c.get(ctx, u)
	if err != nil {
		return nil, src, err
	}
	total := 0
	if m := totalRE.FindSubmatch(page); m != nil {
		total, _ = strconv.Atoi(string(m[1]))
	}
	var (
		out   []SharedCode
		size  = len(page)
		seen  = map[int]bool{1: true}
		pageN = 1
	)
	for {
		rows := parseGrid(string(page))
		out = append(out, rows...)
		if progress != nil {
			progress(pageN, len(out), total)
		}
		next := 0
		for _, m := range pageLinkRE.FindAllSubmatch(page, -1) {
			n, _ := strconv.Atoi(string(m[1]))
			if !seen[n] && (next == 0 || n < next) {
				next = n
			}
		}
		if next == 0 || (total > 0 && len(out) >= total) {
			break
		}
		form := url.Values{}
		for _, m := range hiddenRE.FindAllSubmatch(page, -1) {
			form.Set(string(m[1]), html.UnescapeString(string(m[2])))
		}
		form.Set("__EVENTTARGET", "ctl00$ContentPlaceHolder1$GridView1")
		form.Set("__EVENTARGUMENT", fmt.Sprintf("Page$%d", next))
		if page, err = c.postForm(ctx, u, form); err != nil {
			return out, src, err
		}
		size += len(page)
		seen[next], pageN = true, next
	}
	if total > 0 && len(out) != total {
		return out, src, fmt.Errorf("901 database: got %d rows, page says %d", len(out), total)
	}
	src.Bytes, src.Elapsed = int64(size), time.Since(start)
	return out, src, nil
}

func parseGrid(page string) []SharedCode {
	rows := map[int]map[string]string{}
	for _, m := range gridSpanRE.FindAllStringSubmatch(page, -1) {
		i, _ := strconv.Atoi(m[2])
		if rows[i] == nil {
			rows[i] = map[string]string{}
		}
		rows[i][m[1]] = normSpace(html.UnescapeString(tagRE.ReplaceAllString(m[3], "")))
	}
	idx := make([]int, 0, len(rows))
	for i := range rows {
		idx = append(idx, i)
	}
	sort.Ints(idx)
	dash := func(s string) string {
		if s == "-" {
			return ""
		}
		return s
	}
	var out []SharedCode
	for _, i := range idx {
		r := rows[i]
		mcc, mnc, err := SplitCode(r["lblMCCMNC"])
		if err != nil {
			continue
		}
		sc := SharedCode{MCC: mcc, MNC: mnc, Assignee: r["lblSPOperator"], Status: r["lblStatut"],
			ReservationStart: dash(r["lblStartReservation"]), ReservationEnd: dash(r["lblEndReservation"])}
		for k, v := range r {
			switch {
			case strings.HasPrefix(k, "lblAssign"):
				sc.Assigned = dash(v)
			case strings.HasPrefix(k, "lblRecla"), strings.HasPrefix(k, "lblReturn"):
				sc.Reclaimed = dash(v)
			}
		}
		out = append(out, sc)
	}
	return out
}
