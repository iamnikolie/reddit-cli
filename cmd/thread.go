package cmd

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/iamnikolie/reddit-cli/internal/archive"
)

// threadOpts controls how a comment tree is rendered.
type threadOpts struct {
	Depth    int    // max nesting shown (1 = top level only); 0 = unlimited
	Top      int    // max comments printed; 0 = all
	MaxChars int    // per-comment body cap; 0 = unlimited
	Sort     string // score | new | old
	ShowIDs  bool
}

type threadStats struct {
	Total, Shown, Hidden, More int
}

func sortNodes(nodes []archive.Node, how string) {
	sort.SliceStable(nodes, func(i, j int) bool {
		a, b := nodes[i], nodes[j]
		if a.Kind != "t1" || b.Kind != "t1" {
			return a.Kind == "t1" && b.Kind != "t1" // "more" placeholders last
		}
		switch how {
		case "new":
			return archive.Num(a.Data, "created_utc") > archive.Num(b.Data, "created_utc")
		case "old":
			return archive.Num(a.Data, "created_utc") < archive.Num(b.Data, "created_utc")
		default:
			as, bs := archive.Num(a.Data, "score"), archive.Num(b.Data, "score")
			if as != bs {
				return as > bs
			}
			return archive.Num(a.Data, "created_utc") < archive.Num(b.Data, "created_utc")
		}
	})
}

func isGone(body string) bool {
	switch strings.TrimSpace(body) {
	case "[deleted]", "[removed]", "":
		return true
	}
	return false
}

// renderThread writes the comment tree as indented Markdown list items:
//
//   - [50] u/SolFlorus · 20h: body…
//   - [12] u/reply · 19h: body…
//
// Siblings are sorted per opts.Sort; deleted leaves are dropped; the walk
// stops after opts.Top comments and opts.Depth levels.
func renderThread(w io.Writer, nodes []archive.Node, o threadOpts) threadStats {
	var st threadStats
	for _, n := range nodes {
		st.Total += n.Count()
	}
	var walk func(nodes []archive.Node, depth int) bool
	walk = func(nodes []archive.Node, depth int) bool {
		sortNodes(nodes, o.Sort)
		for _, n := range nodes {
			if n.Kind == "more" {
				st.More += len(n.More)
				continue
			}
			if n.Kind != "t1" {
				continue
			}
			body := archive.Str(n.Data, "body")
			if isGone(body) && len(n.Children) == 0 {
				st.Total--
				continue
			}
			if o.Top > 0 && st.Shown >= o.Top {
				st.Hidden += n.Count()
				continue
			}
			if o.Depth > 0 && depth >= o.Depth {
				st.Hidden += n.Count()
				continue
			}
			st.Shown++
			indent := strings.Repeat("  ", depth)
			text := truncate(body, o.MaxChars)
			if isGone(body) {
				text = "[deleted]"
			}
			id := ""
			if o.ShowIDs {
				id = " #" + archive.Str(n.Data, "id")
			}
			author := archive.Str(n.Data, "author")
			if archive.Str(n.Data, "distinguished") != "" {
				author += " (" + archive.Str(n.Data, "distinguished") + ")"
			}
			fmt.Fprintf(w, "%s- [%d] u/%s · %s%s: %s\n", indent, archive.Num(n.Data, "score"), author, age(archive.Num(n.Data, "created_utc")), id, text)
			walk(n.Children, depth+1)
		}
		return true
	}
	walk(nodes, 0)
	return st
}
