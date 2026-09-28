package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/iamnikolie/reddit-cli/internal/archive"
)

// now is overridable in tests.
var now = time.Now

// age renders a unix timestamp as a compact relative age (5m, 3h, 2d, 4mo, 2y).
func age(unix int64) string {
	if unix <= 0 {
		return "?"
	}
	d := now().Sub(time.Unix(unix, 0))
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 60*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d < 2*365*24*time.Hour:
		return fmt.Sprintf("%dmo", int(d.Hours()/(24*30)))
	default:
		return fmt.Sprintf("%dy", int(d.Hours()/(24*365)))
	}
}

func dateTime(unix int64) string {
	if unix <= 0 {
		return "?"
	}
	return time.Unix(unix, 0).UTC().Format("2006-01-02 15:04")
}

// fresh reports whether the archive's score/num_comments are still the
// creation-time placeholders (they settle ~36h after posting).
func fresh(unix int64) bool {
	return unix > 0 && now().Sub(time.Unix(unix, 0)) < 36*time.Hour
}

// postLine is one token-lean Markdown line for a post listing.
//
//   - 1wro60n · 20h · ↑50 · 37c · r/golang · **Title** · u/author
//     body preview…
func postLine(r archive.Row, withSub bool, body int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "- %s · %s", archive.Str(r, "id"), age(archive.Num(r, "created_utc")))
	if _, ok := r["score"]; ok {
		fmt.Fprintf(&b, " · ↑%d · %dc", archive.Num(r, "score"), archive.Num(r, "num_comments"))
	}
	if withSub {
		fmt.Fprintf(&b, " · r/%s", archive.Str(r, "subreddit"))
	}
	fmt.Fprintf(&b, " · **%s** · u/%s", strings.TrimSpace(archive.Str(r, "title")), archive.Str(r, "author"))
	if f := archive.Str(r, "link_flair_text"); f != "" {
		fmt.Fprintf(&b, " · [%s]", f)
	}
	if u := archive.Str(r, "url"); u != "" && !strings.Contains(u, "/comments/"+archive.Str(r, "id")) {
		fmt.Fprintf(&b, " · %s", u)
	}
	if body > 0 {
		if t := truncate(archive.Str(r, "selftext"), body); t != "" && t != "[removed]" && t != "[deleted]" {
			fmt.Fprintf(&b, "\n  %s", t)
		}
	}
	return b.String()
}

// commentLine is one token-lean Markdown line for a comment listing.
func commentLine(r archive.Row, withSub bool, body int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "- %s · %s · ↑%d", archive.Str(r, "id"), age(archive.Num(r, "created_utc")), archive.Num(r, "score"))
	if withSub {
		fmt.Fprintf(&b, " · r/%s", archive.Str(r, "subreddit"))
	}
	fmt.Fprintf(&b, " · u/%s · on %s", archive.Str(r, "author"), strings.TrimPrefix(archive.Str(r, "link_id"), "t3_"))
	fmt.Fprintf(&b, ": %s", truncate(archive.Str(r, "body"), body))
	return b.String()
}

// postHeader renders the post card at the top of a thread; bodyChars caps
// the self-text (0 = unlimited, negative = omit).
func postHeader(p archive.Row, bodyChars int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n", strings.TrimSpace(archive.Str(p, "title")))
	fmt.Fprintf(&b, "r/%s · u/%s · %s (%s)", archive.Str(p, "subreddit"), archive.Str(p, "author"), dateTime(archive.Num(p, "created_utc")), age(archive.Num(p, "created_utc")))
	if _, ok := p["score"]; ok {
		fmt.Fprintf(&b, " · ↑%d · %d comments", archive.Num(p, "score"), archive.Num(p, "num_comments"))
	}
	fmt.Fprintf(&b, " · id %s", archive.Str(p, "id"))
	if f := archive.Str(p, "link_flair_text"); f != "" {
		fmt.Fprintf(&b, " · [%s]", f)
	}
	b.WriteString("\n")
	id := archive.Str(p, "id")
	if u := archive.Str(p, "url"); u != "" && !strings.Contains(u, "/comments/"+id) {
		fmt.Fprintf(&b, "link: %s\n", u)
	}
	fmt.Fprintf(&b, "https://www.reddit.com/r/%s/comments/%s/\n", archive.Str(p, "subreddit"), id)
	if t := strings.TrimSpace(archive.Str(p, "selftext")); t != "" && bodyChars >= 0 {
		b.WriteString("\n")
		if bodyChars > 0 && len([]rune(t)) > bodyChars {
			t = strings.TrimSpace(string([]rune(t)[:bodyChars-1])) + "…"
		}
		b.WriteString(t)
		b.WriteString("\n")
	}
	return b.String()
}
