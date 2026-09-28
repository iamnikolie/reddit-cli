package cmd

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/iamnikolie/reddit-cli/internal/archive"
	"github.com/spf13/cobra"
)

var (
	postDepth    int
	postTop      int
	postMaxChars int
	postBody     int
	postSort     string
	postIDs      bool
	postParent   string
	postNoBody   bool
)

var postCmd = &cobra.Command{
	Use:   "post <id|url> [<id|url>...]",
	Short: "Read a post with its comment tree",
	Long: `Read a post and its comments as a compact tree. Accepts a post id
(1wro60n or t3_1wro60n), a reddit.com URL (post or comment permalink; a
comment permalink focuses on that comment's subtree) or a /r/<sub>/s/<code>
share link.

Defaults keep the output small: top 60 comments by score, 4 levels deep,
600 characters per comment. Loosen with --top 0 --depth 0 --max-chars 0.`,
	Example: `  reddit post 1wro60n
  reddit post https://www.reddit.com/r/golang/comments/1wro60n/what_do_you_use/ --top 20
  reddit post 1wro60n --sort new --depth 2 --ids
  reddit post 1wro60n --parent kx0abc1 --top 0
  reddit post 1wro60n --json`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := reqCtx(cmd)
		defer cancel()
		for i, arg := range args {
			ref, err := parsePostRef(arg)
			if err != nil {
				return err
			}
			if ref.short != "" {
				p, err := arc.ResolveShortLink(ctx, ref.short)
				if err != nil {
					return err
				}
				ref, err = parsePostRef("https://www.reddit.com" + p)
				if err != nil {
					return err
				}
			}
			parent := postParent
			if parent == "" {
				parent = ref.comment
			}
			if err := showPost(cmd, ref.post, parent); err != nil {
				return fmt.Errorf("%s: %w", arg, err)
			}
			if i < len(args)-1 && format() == "md" {
				fmt.Fprint(stdout, "\n---\n\n")
			}
		}
		return nil
	},
}

func showPost(cmd *cobra.Command, id, parent string) error {
	ctx, cancel := reqCtx(cmd)
	defer cancel()
	posts, err := arc.PostsByID(ctx, []string{id}, nil)
	if err != nil {
		return err
	}
	if len(posts) == 0 {
		return fmt.Errorf("post %s is not in the archive (yet)", id)
	}
	p := posts[0]
	fetch := 9999
	if postTop > 0 {
		fetch = postTop * 4
		if fetch < 500 {
			fetch = 500
		}
	}
	nodes, err := arc.Tree(ctx, id, parent, fetch)
	if err != nil {
		return err
	}
	if format() == "json" {
		return writeJSON(stdout, map[string]any{"post": p, "comments": nodesToJSON(nodes)})
	}
	if format() != "md" {
		rows := flatten(nodes, nil)
		return emitRows(rows, []string{"id", "parent_id", "created_utc", "score", "author", "body"})
	}
	body := postBody
	if postNoBody {
		body = -1
	}
	fmt.Fprint(stdout, postHeader(p, body))
	if fresh(archive.Num(p, "created_utc")) {
		note("(post is <36h old: archived score/comment counts are placeholders; the tree below is live)")
	}
	title := "Comments"
	if parent != "" {
		title = "Replies under " + strings.TrimPrefix(parent, "t1_")
	}
	fmt.Fprintf(stdout, "\n## %s", title)
	var sb strings.Builder
	st := renderThread(&sb, nodes, threadOpts{Depth: postDepth, Top: postTop, MaxChars: postMaxChars, Sort: postSort, ShowIDs: postIDs})
	fmt.Fprintf(stdout, " (%d archived, showing %d; sort %s", st.Total, st.Shown, postSort)
	if postDepth > 0 {
		fmt.Fprintf(stdout, ", depth ≤ %d", postDepth)
	}
	fmt.Fprintln(stdout, ")")
	if st.Shown == 0 {
		fmt.Fprintln(stdout, "_No comments._")
		return nil
	}
	fmt.Fprint(stdout, sb.String())
	if st.Hidden > 0 || st.More > 0 {
		fmt.Fprintf(stdout, "\n_%d comments hidden", st.Hidden+st.More)
		var hints []string
		if postTop > 0 {
			hints = append(hints, "--top 0")
		}
		if postDepth > 0 {
			hints = append(hints, "--depth 0")
		}
		hints = append(hints, "--parent <id> --ids")
		fmt.Fprintf(stdout, " (%s)._\n", strings.Join(hints, ", "))
	}
	return nil
}

func nodesToJSON(nodes []archive.Node) []map[string]any {
	out := make([]map[string]any, 0, len(nodes))
	for _, n := range nodes {
		if n.Kind == "more" {
			out = append(out, map[string]any{"kind": "more", "children": n.More})
			continue
		}
		m := make(map[string]any, len(n.Data)+1)
		for k, v := range n.Data {
			m[k] = v
		}
		m["replies"] = nodesToJSON(n.Children)
		out = append(out, m)
	}
	return out
}

func flatten(nodes []archive.Node, out []archive.Row) []archive.Row {
	for _, n := range nodes {
		if n.Kind != "t1" {
			continue
		}
		out = append(out, n.Data)
		out = flatten(n.Children, out)
	}
	return out
}

// postRef is a parsed post reference.
type postRef struct {
	post    string // base36 id without prefix
	comment string // optional comment id (from a permalink)
	short   string // /r/<sub>/s/<code> path needing resolution
}

var (
	idRe        = regexp.MustCompile(`^(t3_)?([a-z0-9]{2,10})$`)
	commentsRe  = regexp.MustCompile(`/comments/([a-z0-9]+)(?:/[^/]*/?([a-z0-9]+)?)?`)
	shortLinkRe = regexp.MustCompile(`^/r/[^/]+/s/[A-Za-z0-9]+`)
)

func parsePostRef(s string) (postRef, error) {
	s = strings.TrimSpace(s)
	if m := idRe.FindStringSubmatch(s); m != nil {
		return postRef{post: m[2]}, nil
	}
	if !strings.Contains(s, "://") {
		s = "https://" + strings.TrimPrefix(s, "//")
	}
	u, err := url.Parse(s)
	if err != nil || u.Path == "" {
		return postRef{}, fmt.Errorf("%q: not a post id or reddit URL", s)
	}
	if m := shortLinkRe.FindString(u.Path); m != "" {
		return postRef{short: m}, nil
	}
	if m := commentsRe.FindStringSubmatch(u.Path); m != nil {
		ref := postRef{post: m[1]}
		if len(m) > 2 && m[2] != "" && m[2] != m[1] {
			ref.comment = m[2]
		}
		// A comment permalink may also carry ?comment=<id> on new reddit.
		if c := u.Query().Get("comment"); c != "" {
			ref.comment = c
		}
		return ref, nil
	}
	// redd.it/<id>
	if strings.HasSuffix(u.Host, "redd.it") {
		if m := idRe.FindStringSubmatch(strings.Trim(u.Path, "/")); m != nil {
			return postRef{post: m[2]}, nil
		}
	}
	return postRef{}, fmt.Errorf("%q: cannot find a post id in this URL", s)
}

func init() {
	f := postCmd.Flags()
	f.IntVar(&postDepth, "depth", 4, "max comment nesting shown (0 = unlimited)")
	f.IntVar(&postTop, "top", 60, "max comments shown, best first (0 = all)")
	f.IntVar(&postMaxChars, "max-chars", 600, "characters per comment (0 = unlimited)")
	f.IntVar(&postBody, "body", 4000, "characters of the post body (0 = unlimited)")
	f.BoolVar(&postNoBody, "no-body", false, "omit the post body")
	f.StringVar(&postSort, "sort", "score", "sibling order: score, new, old")
	f.BoolVar(&postIDs, "ids", false, "print comment ids (#id) for --parent drill-down")
	f.StringVar(&postParent, "parent", "", "show only the subtree under this comment id")
	rootCmd.AddCommand(postCmd)
}
