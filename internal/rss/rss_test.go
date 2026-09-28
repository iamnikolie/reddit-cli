package rss

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const feedFixture = `<?xml version="1.0" encoding="UTF-8"?><feed xmlns="http://www.w3.org/2005/Atom"><title>newest submissions : golang</title>
<entry><author><name>/u/alice</name><uri>https://www.reddit.com/user/alice</uri></author><category term="golang" label="r/golang"/><content type="html">&lt;!-- SC_OFF --&gt;&lt;div class=&quot;md&quot;&gt;&lt;p&gt;Do you use the Standard library?&lt;/p&gt; &lt;p&gt;Or &lt;a href=&quot;https://example.com&quot;&gt;something&lt;/a&gt; else?&lt;/p&gt; &lt;/div&gt;&lt;!-- SC_ON --&gt; &amp;#32; submitted by &amp;#32; &lt;a href=&quot;https://www.reddit.com/user/alice&quot;&gt; /u/alice &lt;/a&gt; &lt;br/&gt; &lt;span&gt;&lt;a href=&quot;https://www.reddit.com/r/golang/comments/1wro60n/what_do_you_use/&quot;&gt;[link]&lt;/a&gt;&lt;/span&gt; &amp;#32; &lt;span&gt;&lt;a href=&quot;https://www.reddit.com/r/golang/comments/1wro60n/what_do_you_use/&quot;&gt;[comments]&lt;/a&gt;&lt;/span&gt;</content><id>t3_1wro60n</id><link href="https://www.reddit.com/r/golang/comments/1wro60n/what_do_you_use/" /><updated>2026-09-27T16:11:26+00:00</updated><published>2026-09-27T16:11:26+00:00</published><title>What do you use?</title></entry>
<entry><author><name>/u/bob</name></author><category term="golang" label="r/golang" /><content type="html">&lt;!-- SC_OFF --&gt;&lt;div class=&quot;md&quot;&gt;&lt;p&gt;I like &amp;quot;urfave&amp;quot;.&lt;/p&gt; &lt;/div&gt;&lt;!-- SC_ON --&gt;</content><id>t1_pce3a3f</id><link href="https://www.reddit.com/r/golang/comments/1wro60n/what_do_you_use/pce3a3f/"/><updated>2026-09-27T16:13:34+00:00</updated><title>/u/bob on What do you use?</title></entry>
<entry><author><name>/u/carol</name></author><category term="golang" label="r/golang"/><content type="html">&lt;table&gt;&lt;tr&gt;&lt;td&gt; &amp;#32; submitted by &amp;#32; &lt;a href=&quot;https://www.reddit.com/user/carol&quot;&gt; /u/carol &lt;/a&gt; &lt;br/&gt; &lt;span&gt;&lt;a href=&quot;https://go.dev/blog/simd&quot;&gt;[link]&lt;/a&gt;&lt;/span&gt; &amp;#32; &lt;span&gt;&lt;a href=&quot;https://www.reddit.com/r/golang/comments/1wpnab3/simd/&quot;&gt;[comments]&lt;/a&gt;&lt;/span&gt;&lt;/td&gt;&lt;/tr&gt;&lt;/table&gt;</content><id>t3_1wpnab3</id><link href="https://www.reddit.com/r/golang/comments/1wpnab3/simd/" /><updated>2026-09-25T10:00:00+00:00</updated><title>Platform-independent SIMD in Go</title></entry>
</feed>`

func TestParseAndRows(t *testing.T) {
	es, err := Parse([]byte(feedFixture))
	if err != nil {
		t.Fatal(err)
	}
	if len(es) != 3 {
		t.Fatalf("entries: %d", len(es))
	}
	post := es[0]
	if post.Kind() != "post" || post.Author != "alice" || post.Subreddit != "golang" || post.Updated.Unix() != 1790525486 {
		t.Fatalf("post entry: %+v", post)
	}
	if got := post.Text(); got != "Do you use the Standard library?\n\nOr something else?" {
		t.Fatalf("text: %q", got)
	}
	if post.ExternalURL() != "" {
		t.Fatalf("self post should have no external url, got %q", post.ExternalURL())
	}
	r := post.Row()
	if r["id"] != "1wro60n" || r["url"] != post.Link || r["title"] != "What do you use?" {
		t.Fatalf("post row: %+v", r)
	}
	if _, ok := r["score"]; ok {
		t.Fatal("feeds must not invent a score")
	}
	c := es[1]
	if c.Kind() != "comment" || c.Text() != `I like "urfave".` {
		t.Fatalf("comment: %+v text=%q", c, c.Text())
	}
	cr := c.Row()
	if cr["link_id"] != "t3_1wro60n" || cr["link_title"] != "What do you use?" || cr["body"] != `I like "urfave".` {
		t.Fatalf("comment row: %+v", cr)
	}
	link := es[2]
	if link.Text() != "" || link.ExternalURL() != "https://go.dev/blog/simd" || link.Row()["url"] != "https://go.dev/blog/simd" {
		t.Fatalf("link post: text=%q url=%q", link.Text(), link.ExternalURL())
	}
}

func TestSubredditRow(t *testing.T) {
	e := Entry{ID: "t5_2rc7j", Title: "The Go Programming Language", Link: "https://www.reddit.com/r/golang/", HTML: `<img src="x" /> <div> Ask questions about Go. </div> <div> <a href="https://www.reddit.com/r/golang/">[link]</a> </div>`}
	r := e.Row()
	if r["kind"] != "subreddit" || r["display_name"] != "golang" || r["description"] != "Ask questions about Go." {
		t.Fatalf("subreddit row: %+v", r)
	}
}

func TestHTMLToText(t *testing.T) {
	in := `<p>one</p><p>two<br/>three</p><ul><li>a</li><li>b</li></ul>&amp; done`
	want := "one\n\ntwo\nthree\n\na\nb\n& done"
	if got := HTMLToText(in); got != want {
		t.Fatalf("got %q", got)
	}
}

func TestTimeFilter(t *testing.T) {
	cases := map[string]string{"": "", "30min": "hour", "12h": "day", "7d": "week", "3w": "month", "6mo": "year", "3m": "year", "5y": "all", "2026-01-01": "all"}
	for in, want := range cases {
		if got := TimeFilter(in); got != want {
			t.Errorf("TimeFilter(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPacer(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	p := &Pacer{StatePath: filepath.Join(dir, "w.json"), Wait: false, MaxWait: 90 * time.Second, now: func() time.Time { return base }}
	if err := p.Before(context.Background()); err != nil {
		t.Fatalf("fresh pacer must allow: %v", err)
	}
	h := http.Header{}
	h.Set("X-Ratelimit-Remaining", "0.0")
	h.Set("X-Ratelimit-Reset", "40")
	p.After(h, 200)
	err := p.Before(context.Background())
	wc, ok := err.(*ErrWindowClosed)
	if !ok || wc.In < 40*time.Second || wc.In > 42*time.Second {
		t.Fatalf("want closed window ~41s, got %v", err)
	}
	// Another process sees the same state.
	p2 := &Pacer{StatePath: p.StatePath, Wait: true, MaxWait: 10 * time.Second, now: p.now}
	if _, ok := p2.Before(context.Background()).(*ErrWindowClosed); !ok {
		t.Fatal("wait longer than MaxWait must error")
	}
	p2.now = func() time.Time { return base.Add(time.Minute) }
	if err := p2.Before(context.Background()); err != nil {
		t.Fatalf("window reopened: %v", err)
	}
	if _, err := os.Stat(p.StatePath); err != nil {
		t.Fatal("state not persisted")
	}
	// remaining > 0 keeps the window open
	h.Set("X-Ratelimit-Remaining", "5")
	p3 := &Pacer{StatePath: filepath.Join(dir, "w2.json"), now: p.now}
	p3.After(h, 200)
	if err := p3.Before(context.Background()); err != nil {
		t.Fatalf("remaining>0 must not close: %v", err)
	}
}
