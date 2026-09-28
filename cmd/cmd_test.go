package cmd

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/iamnikolie/reddit-cli/internal/archive"
)

func TestParsePostRef(t *testing.T) {
	cases := []struct {
		in            string
		post, comment string
		short         string
		err           bool
	}{
		{in: "1wro60n", post: "1wro60n"},
		{in: "t3_1wro60n", post: "1wro60n"},
		{in: "https://www.reddit.com/r/golang/comments/1wro60n/what_do_you_use/", post: "1wro60n"},
		{in: "reddit.com/r/golang/comments/1wro60n", post: "1wro60n"},
		{in: "https://old.reddit.com/r/golang/comments/1wro60n/slug/kx0abc1/", post: "1wro60n", comment: "kx0abc1"},
		{in: "https://www.reddit.com/r/golang/comments/1wro60n/slug/?comment=kx0abc1", post: "1wro60n", comment: "kx0abc1"},
		{in: "https://redd.it/1wro60n", post: "1wro60n"},
		{in: "https://www.reddit.com/r/running/s/3TzXiyxaMD", short: "/r/running/s/3TzXiyxaMD"},
		{in: "https://www.reddit.com/r/golang/", err: true},
		{in: "not a ref!", err: true},
	}
	for _, c := range cases {
		got, err := parsePostRef(c.in)
		if c.err {
			if err == nil {
				t.Errorf("%q: want error, got %+v", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: %v", c.in, err)
			continue
		}
		if got.post != c.post || got.comment != c.comment || got.short != c.short {
			t.Errorf("%q: got %+v", c.in, got)
		}
	}
}

func TestAge(t *testing.T) {
	base := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	now = func() time.Time { return base }
	defer func() { now = time.Now }()
	cases := map[int64]string{
		base.Add(-30 * time.Second).Unix():     "now",
		base.Add(-5 * time.Minute).Unix():      "5m",
		base.Add(-3 * time.Hour).Unix():        "3h",
		base.Add(-40 * time.Hour).Unix():       "40h",
		base.Add(-5 * 24 * time.Hour).Unix():   "5d",
		base.Add(-90 * 24 * time.Hour).Unix():  "3mo",
		base.Add(-800 * 24 * time.Hour).Unix(): "2y",
		0:                                      "?",
	}
	for in, want := range cases {
		if got := age(in); got != want {
			t.Errorf("age(%d) = %q, want %q", in, got, want)
		}
	}
}

func node(id, author, body string, score int, created int64, children ...archive.Node) archive.Node {
	return archive.Node{Kind: "t1", Data: archive.Row{
		"id": id, "author": author, "body": body,
		"score": json.Number(strconv.Itoa(score)), "created_utc": json.Number(strconv.FormatInt(created, 10)),
	}, Children: children}
}

func TestRenderThread(t *testing.T) {
	now = func() time.Time { return time.Unix(2000, 0) }
	defer func() { now = time.Now }()
	tree := []archive.Node{
		node("a", "low", "low scored", 1, 100),
		node("b", "high", "top comment", 50, 100,
			node("b1", "r1", "reply one", 5, 200,
				node("b11", "r11", "deep reply", 2, 300)),
			node("b2", "gone", "[deleted]", 0, 200),
		),
		{Kind: "more", More: []string{"x", "y", "z"}},
	}
	var buf bytes.Buffer
	st := renderThread(&buf, tree, threadOpts{Depth: 2, Top: 0, MaxChars: 0, Sort: "score", ShowIDs: true})
	out := buf.String()
	if st.Total != 4 || st.Shown != 3 || st.Hidden != 1 || st.More != 3 {
		t.Fatalf("stats %+v\n%s", st, out)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if !strings.HasPrefix(lines[0], "- [50] u/high") || !strings.HasPrefix(lines[1], "  - [5] u/r1") || !strings.HasPrefix(lines[2], "- [1] u/low") {
		t.Fatalf("unexpected order/indent:\n%s", out)
	}
	if strings.Contains(out, "deep reply") || strings.Contains(out, "gone") {
		t.Fatalf("depth cap or deleted-leaf drop failed:\n%s", out)
	}
	if !strings.Contains(lines[0], "#b") {
		t.Fatalf("ids missing:\n%s", out)
	}

	buf.Reset()
	st = renderThread(&buf, tree, threadOpts{Top: 1, Sort: "new"})
	if st.Shown != 1 || st.Hidden != 3 {
		t.Fatalf("top cap: %+v\n%s", st, buf.String())
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("  a  b\n\nc ", 0); got != "a b c" {
		t.Errorf("collapse: %q", got)
	}
	if got := truncate("hello world", 6); got != "hello…" {
		t.Errorf("cut: %q", got)
	}
}
