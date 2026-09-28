// Package archive is the Arctic Shift source: a public, no-auth archive of
// Reddit posts and comments (https://arctic-shift.photon-reddit.com).
package archive

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/iamnikolie/reddit-cli/internal/client"
)

// Client wraps the shared HTTP client with archive-specific calls.
type Client struct {
	c *client.Client
}

// New returns an archive client over c.
func New(c *client.Client) *Client { return &Client{c: c} }

// Row is one decoded JSON object; numbers are json.Number.
type Row = map[string]any

// PostFields is the lean projection requested for post listings.
var PostFields = []string{"id", "title", "author", "subreddit", "created_utc", "score", "num_comments", "url", "selftext", "link_flair_text", "over_18", "retrieved_on"}

// CommentFields is the lean projection requested for comment listings.
var CommentFields = []string{"id", "author", "subreddit", "created_utc", "score", "body", "link_id", "parent_id", "retrieved_on"}

// Raw calls an endpoint and returns the response body.
func (a *Client) Raw(ctx context.Context, path string, q url.Values) ([]byte, error) {
	return a.get(ctx, path, q)
}

// get is Client.Get with one retry on the archive's "Timeout" answer (HTTP
// 422): its database often needs a warm-up and a second try succeeds.
func (a *Client) get(ctx context.Context, path string, q url.Values) ([]byte, error) {
	b, err := a.c.Get(ctx, path, q)
	if !isTimeout(err) {
		return b, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(1500 * time.Millisecond):
	}
	b, err = a.c.Get(ctx, path, q)
	if isTimeout(err) {
		return nil, fmt.Errorf("%w\nthe archive query timed out twice: narrow it with --after/--before or a smaller -n, then retry", err)
	}
	return b, err
}

func isTimeout(err error) bool {
	var ae *client.APIError
	return errors.As(err, &ae) && ae.Status == 422 && strings.Contains(strings.ToLower(ae.Message), "timeout")
}

// Partial reports a fan-out where some subreddits failed; the rows that did
// arrive are still returned alongside it.
type Partial struct {
	Failed []string
}

func (p *Partial) Error() string {
	return "partial results, failed: " + strings.Join(p.Failed, "; ")
}

// envelope is the archive's {"data": ..., "error": "..."} answer.
type envelope struct {
	Data  json.RawMessage `json:"data"`
	Error string          `json:"error"`
}

func decodeRows(b []byte) ([]Row, error) {
	var env envelope
	if err := json.Unmarshal(b, &env); err != nil {
		return nil, fmt.Errorf("decode archive response: %w", err)
	}
	if env.Error != "" {
		return nil, fmt.Errorf("archive error: %s", env.Error)
	}
	if len(env.Data) == 0 || string(env.Data) == "null" {
		return nil, nil
	}
	dec := json.NewDecoder(bytes.NewReader(env.Data))
	dec.UseNumber()
	var rows []Row
	if err := dec.Decode(&rows); err != nil {
		return nil, fmt.Errorf("decode archive data: %w", err)
	}
	return rows, nil
}

// Rows calls an endpoint and returns its data array.
func (a *Client) Rows(ctx context.Context, path string, q url.Values) ([]Row, error) {
	b, err := a.get(ctx, path, q)
	if err != nil {
		return nil, err
	}
	return decodeRows(b)
}

// Num reads a numeric field as int64 (0 when absent).
func Num(r Row, key string) int64 {
	switch v := r[key].(type) {
	case json.Number:
		n, _ := v.Int64()
		if n == 0 {
			f, _ := v.Float64()
			n = int64(f)
		}
		return n
	case float64:
		return int64(v)
	}
	return 0
}

// Str reads a string field ("" when absent or null).
func Str(r Row, key string) string {
	s, _ := r[key].(string)
	return s
}

// PostQuery filters /api/posts/search. Subreddits are queried in parallel
// and merged; Query needs a subreddit or an author (archive rule).
type PostQuery struct {
	Subreddits []string
	Author     string
	Query      string // title + selftext keyword search
	Title      string // title-only keyword search
	Flair      string
	URL        string
	After      string // date: 2026-01-01, epoch, or offset like 7d
	Before     string
	Limit      int
	Sort       string // asc | desc (by created_utc)
	Fields     []string
	NSFW       *bool
}

func (q PostQuery) values() url.Values {
	v := url.Values{}
	set := func(k, s string) {
		if s != "" {
			v.Set(k, s)
		}
	}
	set("author", strings.TrimPrefix(q.Author, "u/"))
	set("query", q.Query)
	set("title", q.Title)
	set("link_flair_text", q.Flair)
	set("url", q.URL)
	set("after", q.After)
	set("before", q.Before)
	set("sort", q.Sort)
	if q.Limit > 0 {
		v.Set("limit", strconv.Itoa(q.Limit))
	}
	if len(q.Fields) > 0 {
		v.Set("fields", strings.Join(q.Fields, ","))
	}
	if q.NSFW != nil {
		v.Set("over_18", strconv.FormatBool(*q.NSFW))
	}
	return v
}

// Posts searches posts. With several subreddits the calls run in parallel
// and the union is sorted by date (Sort) and cut to Limit.
func (a *Client) Posts(ctx context.Context, q PostQuery) ([]Row, error) {
	if len(q.Subreddits) <= 1 {
		v := q.values()
		if len(q.Subreddits) == 1 {
			v.Set("subreddit", cleanSub(q.Subreddits[0]))
		}
		return a.Rows(ctx, "/api/posts/search", v)
	}
	rows, err := a.fanOut(ctx, "/api/posts/search", q.values(), q.Subreddits)
	if err != nil && rows == nil {
		return nil, err
	}
	SortByDate(rows, q.Sort != "asc")
	if q.Limit > 0 && len(rows) > q.Limit {
		rows = rows[:q.Limit]
	}
	return rows, err
}

// CommentQuery filters /api/comments/search. Body needs a subreddit, author,
// link or parent (archive rule).
type CommentQuery struct {
	Subreddits []string
	Author     string
	Body       string
	LinkID     string
	ParentID   string
	After      string
	Before     string
	Limit      int
	Sort       string
	Fields     []string
}

func (q CommentQuery) values() url.Values {
	v := url.Values{}
	set := func(k, s string) {
		if s != "" {
			v.Set(k, s)
		}
	}
	set("author", strings.TrimPrefix(q.Author, "u/"))
	set("body", q.Body)
	set("link_id", q.LinkID)
	set("parent_id", q.ParentID)
	set("after", q.After)
	set("before", q.Before)
	set("sort", q.Sort)
	if q.Limit > 0 {
		v.Set("limit", strconv.Itoa(q.Limit))
	}
	if len(q.Fields) > 0 {
		v.Set("fields", strings.Join(q.Fields, ","))
	}
	return v
}

// Comments searches comments, fanning out over subreddits like Posts.
func (a *Client) Comments(ctx context.Context, q CommentQuery) ([]Row, error) {
	if len(q.Subreddits) <= 1 {
		v := q.values()
		if len(q.Subreddits) == 1 {
			v.Set("subreddit", cleanSub(q.Subreddits[0]))
		}
		return a.Rows(ctx, "/api/comments/search", v)
	}
	rows, err := a.fanOut(ctx, "/api/comments/search", q.values(), q.Subreddits)
	if err != nil && rows == nil {
		return nil, err
	}
	SortByDate(rows, q.Sort != "asc")
	if q.Limit > 0 && len(rows) > q.Limit {
		rows = rows[:q.Limit]
	}
	return rows, err
}

func (a *Client) fanOut(ctx context.Context, path string, base url.Values, subs []string) ([]Row, error) {
	type res struct {
		rows []Row
		err  error
	}
	out := make([]res, len(subs))
	sem := make(chan struct{}, 3) // be considerate: the archive is a free service
	var wg sync.WaitGroup
	for i, s := range subs {
		wg.Add(1)
		go func(i int, s string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			v := url.Values{}
			for k, vals := range base {
				v[k] = append([]string(nil), vals...)
			}
			v.Set("subreddit", cleanSub(s))
			rows, err := a.Rows(ctx, path, v)
			out[i] = res{rows, err}
		}(i, s)
	}
	wg.Wait()
	var rows []Row
	var failed []string
	for i, r := range out {
		if r.err != nil {
			failed = append(failed, fmt.Sprintf("r/%s: %v", subs[i], r.err))
			continue
		}
		rows = append(rows, r.rows...)
	}
	if len(failed) == len(subs) {
		return nil, errors.New(strings.Join(failed, "\n"))
	}
	if len(failed) > 0 {
		return rows, &Partial{Failed: failed}
	}
	return rows, nil
}

// SortByDate orders rows by created_utc, newest first when desc.
func SortByDate(rows []Row, desc bool) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := Num(rows[i], "created_utc"), Num(rows[j], "created_utc")
		if desc {
			return a > b
		}
		return a < b
	})
}

// SortByScore orders rows by score, highest first; ties keep date order.
func SortByScore(rows []Row) {
	sort.SliceStable(rows, func(i, j int) bool {
		return Num(rows[i], "score") > Num(rows[j], "score")
	})
}

// PostsByID fetches posts by id (t3_ prefix optional, up to 500).
func (a *Client) PostsByID(ctx context.Context, ids []string, fields []string) ([]Row, error) {
	v := url.Values{"ids": {strings.Join(ids, ",")}}
	if len(fields) > 0 {
		v.Set("fields", strings.Join(fields, ","))
	}
	return a.Rows(ctx, "/api/posts/ids", v)
}

// CommentsByID fetches comments by id (t1_ prefix optional, up to 500).
func (a *Client) CommentsByID(ctx context.Context, ids []string, fields []string) ([]Row, error) {
	v := url.Values{"ids": {strings.Join(ids, ",")}}
	if len(fields) > 0 {
		v.Set("fields", strings.Join(fields, ","))
	}
	return a.Rows(ctx, "/api/comments/ids", v)
}

// Node is one entry of a comment tree: a comment with its replies, or a
// "more" placeholder listing collapsed comment ids.
type Node struct {
	Kind     string
	Data     Row
	Children []Node
	More     []string
}

// Count returns the number of comments in the subtree (excluding "more").
func (n Node) Count() int {
	if n.Kind != "t1" {
		return 0
	}
	c := 1
	for _, ch := range n.Children {
		c += ch.Count()
	}
	return c
}

// Tree fetches the comment tree of a post (/api/comments/tree). limit caps
// how many comments the server expands; 9999 means everything.
func (a *Client) Tree(ctx context.Context, linkID, parentID string, limit int) ([]Node, error) {
	v := url.Values{"link_id": {"t3_" + strings.TrimPrefix(linkID, "t3_")}}
	if parentID != "" {
		v.Set("parent_id", "t1_"+strings.TrimPrefix(parentID, "t1_"))
	}
	if limit > 0 {
		v.Set("limit", strconv.Itoa(limit))
	}
	b, err := a.get(ctx, "/api/comments/tree", v)
	if err != nil {
		return nil, err
	}
	var env envelope
	if err := json.Unmarshal(b, &env); err != nil {
		return nil, fmt.Errorf("decode archive response: %w", err)
	}
	if env.Error != "" {
		return nil, fmt.Errorf("archive error: %s", env.Error)
	}
	if len(env.Data) == 0 || string(env.Data) == "null" {
		return nil, nil
	}
	dec := json.NewDecoder(bytes.NewReader(env.Data))
	dec.UseNumber()
	var raw []any
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode archive tree: %w", err)
	}
	return parseNodes(raw), nil
}

func parseNodes(items []any) []Node {
	var out []Node
	for _, it := range items {
		m, ok := it.(Row)
		if !ok {
			continue
		}
		kind, _ := m["kind"].(string)
		data, _ := m["data"].(Row)
		n := Node{Kind: kind, Data: data}
		switch kind {
		case "more":
			for _, c := range asList(data["children"]) {
				if s, ok := c.(string); ok {
					n.More = append(n.More, s)
				}
			}
		case "t1":
			if rep, ok := data["replies"].(Row); ok {
				if rd, ok := rep["data"].(Row); ok {
					n.Children = parseNodes(asList(rd["children"]))
				}
			}
			delete(data, "replies")
		}
		out = append(out, n)
	}
	return out
}

func asList(v any) []any {
	l, _ := v.([]any)
	return l
}

// SubredditQuery filters /api/subreddits/search.
type SubredditQuery struct {
	Name           string // exact
	Prefix         string
	MinSubscribers int
	Limit          int
	SortType       string // subscribers | created_utc | subreddit
	Sort           string
}

// Subreddits searches the subreddit index (updated infrequently).
func (a *Client) Subreddits(ctx context.Context, q SubredditQuery) ([]Row, error) {
	v := url.Values{}
	if q.Name != "" {
		v.Set("subreddit", cleanSub(q.Name))
	}
	if q.Prefix != "" {
		v.Set("subreddit_prefix", cleanSub(q.Prefix))
	}
	if q.MinSubscribers > 0 {
		v.Set("min_subscribers", strconv.Itoa(q.MinSubscribers))
	}
	if q.Limit > 0 {
		v.Set("limit", strconv.Itoa(q.Limit))
	}
	if q.SortType != "" {
		v.Set("sort_type", q.SortType)
	}
	if q.Sort != "" {
		v.Set("sort", q.Sort)
	}
	v.Set("fields", "display_name,public_description,subscribers,created_utc,over18")
	return a.Rows(ctx, "/api/subreddits/search", v)
}

// UserSubreddits lists where a user is active, most active first.
func (a *Client) UserSubreddits(ctx context.Context, author string, limit int) ([]Row, error) {
	v := url.Values{"author": {strings.TrimPrefix(author, "u/")}}
	if limit > 0 {
		v.Set("limit", strconv.Itoa(limit))
	}
	return a.Rows(ctx, "/api/users/interactions/subreddits", v)
}

// ResolveShortLink expands a /r/<sub>/s/<code> share path to its full path.
func (a *Client) ResolveShortLink(ctx context.Context, path string) (string, error) {
	rows, err := a.Rows(ctx, "/api/short_links", url.Values{"paths": {path}})
	if err != nil {
		return "", err
	}
	if len(rows) == 0 || Str(rows[0], "resolved_path") == "" {
		return "", fmt.Errorf("short link %s is not in the archive", path)
	}
	return Str(rows[0], "resolved_path"), nil
}

func cleanSub(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "/")
	s = strings.TrimPrefix(s, "r/")
	return strings.TrimSuffix(s, "/")
}
