// Package rss is the live source: reddit.com's public Atom feeds. No
// credentials, but reddit.com allows about one anonymous feed request per
// clock minute per IP, so every call goes through a pacer that remembers the
// window across processes.
package rss

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/iamnikolie/reddit-cli/internal/archive"
	"github.com/iamnikolie/reddit-cli/internal/client"
)

// DefaultBaseURL is where the feeds live.
const DefaultBaseURL = "https://www.reddit.com"

// Pacer serialises feed requests to reddit.com's anonymous window. The next
// allowed time is persisted in StatePath so separate CLI invocations share
// the budget.
type Pacer struct {
	StatePath string
	// Wait lets Before sleep until the window opens (up to MaxWait); when
	// false a closed window is an error.
	Wait    bool
	MaxWait time.Duration
	// Note receives one-line status messages (nil = silent).
	Note func(string)
	now  func() time.Time
}

type state struct {
	NextOK int64 `json:"next_ok"`
}

func (p *Pacer) clock() time.Time {
	if p.now != nil {
		return p.now()
	}
	return time.Now()
}

func (p *Pacer) load() state {
	var st state
	if p.StatePath == "" {
		return st
	}
	b, err := os.ReadFile(p.StatePath)
	if err == nil {
		_ = json.Unmarshal(b, &st)
	}
	return st
}

func (p *Pacer) save(st state) {
	if p.StatePath == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(p.StatePath), 0o700)
	b, _ := json.Marshal(st)
	_ = os.WriteFile(p.StatePath, b, 0o600)
}

// ErrWindowClosed is returned when the rate window is closed and waiting is
// disabled or too long.
type ErrWindowClosed struct {
	In time.Duration
}

func (e *ErrWindowClosed) Error() string {
	return fmt.Sprintf("reddit.com feed window is closed: next anonymous request allowed in %ds (reddit.com allows ~1 feed request per minute per IP; use the archive source or wait)", int(e.In.Seconds()+0.999))
}

// Before blocks until the window is open, or errors.
func (p *Pacer) Before(ctx context.Context) error {
	st := p.load()
	next := time.Unix(st.NextOK, 0)
	wait := next.Sub(p.clock())
	if wait <= 0 {
		return nil
	}
	if !p.Wait || wait > p.MaxWait {
		return &ErrWindowClosed{In: wait}
	}
	if p.Note != nil {
		p.Note(fmt.Sprintf("(reddit.com allows ~1 feed request/minute from this IP; waiting %ds for the next window)", int(wait.Seconds()+0.999)))
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(wait):
	}
	return nil
}

// After records the window reddit.com reported in its rate-limit headers.
func (p *Pacer) After(h http.Header, status int) {
	remaining := h.Get("X-Ratelimit-Remaining")
	reset := h.Get("X-Ratelimit-Reset")
	secs, err := strconv.ParseFloat(reset, 64)
	if err != nil || secs <= 0 {
		return
	}
	rem, _ := strconv.ParseFloat(remaining, 64)
	if status == http.StatusTooManyRequests || rem < 1 {
		p.save(state{NextOK: p.clock().Add(time.Duration(secs*float64(time.Second)) + time.Second).Unix()})
	}
}

// Client fetches and parses reddit.com Atom feeds.
type Client struct {
	c     *client.Client
	Pacer *Pacer
}

// New returns a feed client over c; c.MaxRetries should be 0 because the
// pacer handles 429 itself.
func New(c *client.Client, p *Pacer) *Client {
	c.MaxRetries = 0
	return &Client{c: c, Pacer: p}
}

// Entry is one parsed feed entry.
type Entry struct {
	ID        string // t3_xxx or t1_xxx (t5_ for subreddits)
	Title     string
	Author    string
	Link      string // reddit permalink
	Subreddit string
	Updated   time.Time
	HTML      string // raw content HTML
}

type atomFeed struct {
	Title   string      `xml:"title"`
	Entries []atomEntry `xml:"entry"`
}

type atomEntry struct {
	ID      string `xml:"id"`
	Title   string `xml:"title"`
	Updated string `xml:"updated"`
	Author  struct {
		Name string `xml:"name"`
	} `xml:"author"`
	Link struct {
		Href string `xml:"href,attr"`
	} `xml:"link"`
	Category struct {
		Term string `xml:"term,attr"`
	} `xml:"category"`
	Content string `xml:"content"`
}

// Parse decodes an Atom feed.
func Parse(b []byte) ([]Entry, error) {
	var f atomFeed
	if err := xml.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("decode feed: %w", err)
	}
	out := make([]Entry, 0, len(f.Entries))
	for _, e := range f.Entries {
		t, _ := time.Parse(time.RFC3339, e.Updated)
		out = append(out, Entry{
			ID:        e.ID,
			Title:     strings.TrimSpace(e.Title),
			Author:    strings.TrimPrefix(strings.TrimSpace(e.Author.Name), "/u/"),
			Link:      e.Link.Href,
			Subreddit: e.Category.Term,
			Updated:   t,
			HTML:      e.Content,
		})
	}
	return out, nil
}

// Feed fetches a feed path (e.g. /r/golang/hot/.rss) and parses it.
func (r *Client) Feed(ctx context.Context, path string, q url.Values) ([]Entry, error) {
	b, err := r.Raw(ctx, path, q)
	if err != nil {
		return nil, err
	}
	return Parse(b)
}

// Raw fetches a feed path through the pacer and returns the body.
func (r *Client) Raw(ctx context.Context, path string, q url.Values) ([]byte, error) {
	if r.Pacer != nil {
		if err := r.Pacer.Before(ctx); err != nil {
			return nil, err
		}
	}
	req := client.Request{Method: http.MethodGet, Path: path, Query: q, Headers: map[string]string{"Accept": "application/atom+xml, application/xml;q=0.9, */*;q=0.5"}}
	b, h, err := r.c.Do(ctx, req)
	var ae *client.APIError
	if errors.As(err, &ae) {
		if r.Pacer != nil {
			r.Pacer.After(ae.Header, ae.Status)
		}
		switch ae.Status {
		case http.StatusTooManyRequests:
			// One retry after the window reopens, if allowed.
			if r.Pacer != nil && r.Pacer.Wait && ae.RetryIn > 0 && time.Duration(ae.RetryIn)*time.Second <= r.Pacer.MaxWait {
				if err := r.Pacer.Before(ctx); err != nil {
					return nil, err
				}
				b, h, err = r.c.Do(ctx, req)
				if errors.As(err, &ae) {
					r.Pacer.After(ae.Header, ae.Status)
					return nil, &ErrWindowClosed{In: time.Duration(ae.RetryIn) * time.Second}
				}
			} else {
				return nil, &ErrWindowClosed{In: time.Duration(ae.RetryIn) * time.Second}
			}
		case http.StatusForbidden:
			return nil, fmt.Errorf("%w\nreddit.com blocks this client (\"network security\"); feeds may work from another network, or use the archive source", err)
		}
	}
	if err != nil {
		return nil, err
	}
	if r.Pacer != nil {
		r.Pacer.After(h, http.StatusOK)
	}
	if strings.Contains(strings.ToLower(string(b[:min(len(b), 512)])), "<html") {
		return nil, fmt.Errorf("reddit.com answered with an HTML page instead of a feed (login wall or block)")
	}
	return b, nil
}

var (
	scBlockRe  = regexp.MustCompile(`(?s)<!-- SC_OFF -->(.*?)<!-- SC_ON -->`)
	linkHrefRe = regexp.MustCompile(`<a href="([^"]+)">\[link\]</a>`)
	tagRe      = regexp.MustCompile(`(?s)<[^>]+>`)
	lineRe     = regexp.MustCompile(`(?i)<br\s*/?>|</li>|</tr>`)
	paraRe     = regexp.MustCompile(`(?i)</p>|</div>|</h[1-6]>|</blockquote>|</pre>|</table>`)
	blankRe    = regexp.MustCompile(`\n{3,}`)
)

// Text converts the entry's body HTML to plain text: the self-text / comment
// markdown block when present, otherwise nothing (link posts carry only the
// "submitted by" boilerplate).
func (e Entry) Text() string {
	m := scBlockRe.FindStringSubmatch(e.HTML)
	if m == nil {
		return ""
	}
	return HTMLToText(m[1])
}

// ExternalURL is the post's target link for link posts ("" for self posts).
func (e Entry) ExternalURL() string {
	m := linkHrefRe.FindStringSubmatch(e.HTML)
	if m == nil {
		return ""
	}
	u := html.UnescapeString(m[1])
	if strings.Contains(u, "/comments/") && strings.Contains(u, "reddit.com") {
		return ""
	}
	return u
}

// HTMLToText strips tags, keeping paragraph breaks.
func HTMLToText(s string) string {
	s = lineRe.ReplaceAllString(s, "\n")
	s = paraRe.ReplaceAllString(s, "\n\n")
	s = tagRe.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(strings.Join(strings.Fields(l), " "))
	}
	s = strings.Join(lines, "\n")
	s = blankRe.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

var (
	commentsRe = regexp.MustCompile(`/comments/([a-z0-9]+)`)
	subPathRe  = regexp.MustCompile(`/r/([^/]+)/?`)
	linkTagRe  = regexp.MustCompile(`<a href="[^"]*">\[link\]</a>`)
)

// Kind is the thing type from the id prefix: post, comment, subreddit or "".
func (e Entry) Kind() string {
	switch {
	case strings.HasPrefix(e.ID, "t3_"):
		return "post"
	case strings.HasPrefix(e.ID, "t1_"):
		return "comment"
	case strings.HasPrefix(e.ID, "t5_"):
		return "subreddit"
	}
	return ""
}

// Row converts an entry to the archive-shaped record the commands render.
// Feeds carry no score or comment count, so those keys are absent.
func (e Entry) Row() archive.Row {
	r := archive.Row{
		"id":          strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(e.ID, "t3_"), "t1_"), "t5_"),
		"kind":        e.Kind(),
		"author":      e.Author,
		"subreddit":   e.Subreddit,
		"created_utc": json.Number(strconv.FormatInt(e.Updated.Unix(), 10)),
		"permalink":   e.Link,
		"source":      "rss",
	}
	switch e.Kind() {
	case "post":
		r["title"] = e.Title
		r["selftext"] = e.Text()
		if u := e.ExternalURL(); u != "" {
			r["url"] = u
		} else {
			r["url"] = e.Link
		}
	case "comment":
		r["body"] = e.Text()
		r["link_title"] = strings.TrimPrefix(e.Title, "/u/"+e.Author+" on ")
		if m := commentsRe.FindStringSubmatch(e.Link); m != nil {
			r["link_id"] = "t3_" + m[1]
		}
	case "subreddit":
		r["title"] = e.Title
		if m := subPathRe.FindStringSubmatch(e.Link); m != nil {
			r["display_name"] = m[1]
			r["subreddit"] = m[1]
		}
		r["description"] = HTMLToText(linkTagRe.ReplaceAllString(e.HTML, ""))
	default:
		r["title"] = e.Title
	}
	return r
}

// Rows converts entries.
func Rows(es []Entry) []archive.Row {
	out := make([]archive.Row, 0, len(es))
	for _, e := range es {
		out = append(out, e.Row())
	}
	return out
}

// TimeFilter maps an --after offset to reddit's t= search window.
func TimeFilter(after string) string {
	if after == "" {
		return ""
	}
	d, ok := parseOffset(after)
	if !ok {
		return "all"
	}
	switch {
	case d <= time.Hour:
		return "hour"
	case d <= 24*time.Hour:
		return "day"
	case d <= 7*24*time.Hour:
		return "week"
	case d <= 31*24*time.Hour:
		return "month"
	case d <= 366*24*time.Hour:
		return "year"
	}
	return "all"
}

var offsetRe = regexp.MustCompile(`^(\d+)\s*(s|sec|m|min|h|hour|d|day|w|week|mo|month|y|year)s?$`)

func parseOffset(s string) (time.Duration, bool) {
	m := offsetRe.FindStringSubmatch(strings.ToLower(strings.TrimSpace(s)))
	if m == nil {
		return 0, false
	}
	n, _ := strconv.Atoi(m[1])
	unit := map[string]time.Duration{"s": time.Second, "sec": time.Second, "m": 30 * 24 * time.Hour, "min": time.Minute, "h": time.Hour, "hour": time.Hour, "d": 24 * time.Hour, "day": 24 * time.Hour, "w": 7 * 24 * time.Hour, "week": 7 * 24 * time.Hour, "mo": 30 * 24 * time.Hour, "month": 30 * 24 * time.Hour, "y": 365 * 24 * time.Hour, "year": 365 * 24 * time.Hour}[m[2]]
	return time.Duration(n) * unit, true
}
