package cmd

import (
	"fmt"
	"strings"

	"github.com/iamnikolie/reddit-cli/internal/archive"
	"github.com/spf13/cobra"
)

var (
	userLimit int
	userSub   string
	userAfter string
	userBody  int
)

var userCmd = &cobra.Command{
	Use:   "user <name> [posts|comments|subs|overview]",
	Short: "A user's posts, comments or the subreddits they are active in",
	Long: `Public activity of a user from the archive.

  overview  where they post + latest posts and comments (default; 3 requests)
  posts     their posts, newest first
  comments  their comments, newest first
  subs      subreddits ranked by their activity there`,
	Example: `  reddit user spez
  reddit user spez posts -n 10 --after 1y
  reddit user spez comments --sub programming
  reddit user spez subs`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := strings.TrimPrefix(strings.TrimPrefix(args[0], "u/"), "/u/")
		mode := "overview"
		if len(args) == 2 {
			mode = args[1]
		}
		if useRSS(false) {
			return liveUser(cmd, name, mode)
		}
		ctx, cancel := reqCtx(cmd)
		defer cancel()
		subs := splitList(userSub)
		switch mode {
		case "posts":
			rows, err := arc.Posts(ctx, archive.PostQuery{Author: name, Subreddits: subs, After: userAfter, Limit: userLimit, Sort: "desc", Fields: leanFields(archive.PostFields)})
			if err = partialOK(err); err != nil {
				return err
			}
			if format() != "md" {
				return emitRows(rows, []string{"id", "created_utc", "score", "num_comments", "subreddit", "title", "url"})
			}
			printPosts(rows, true, userBody)
		case "comments":
			rows, err := arc.Comments(ctx, archive.CommentQuery{Author: name, Subreddits: subs, After: userAfter, Limit: userLimit, Sort: "desc", Fields: leanFields(archive.CommentFields)})
			if err = partialOK(err); err != nil {
				return err
			}
			if format() != "md" {
				return emitRows(rows, []string{"id", "created_utc", "score", "subreddit", "link_id", "body"})
			}
			printComments(rows, true, userBody)
		case "subs":
			rows, err := arc.UserSubreddits(ctx, name, userLimit)
			if err != nil {
				return err
			}
			if format() != "md" {
				return emitRows(rows, []string{"subreddit", "count"})
			}
			printUserSubs(rows)
		case "overview":
			if format() != "md" {
				return fmt.Errorf("overview is Markdown only; use 'user %s posts|comments|subs' with --format", name)
			}
			n := userLimit
			if n > 10 {
				n = 10
			}
			subsRows, err := arc.UserSubreddits(ctx, name, n)
			if err != nil {
				return err
			}
			posts, err := arc.Posts(ctx, archive.PostQuery{Author: name, Limit: n, Sort: "desc", Fields: archive.PostFields})
			if err != nil {
				return err
			}
			comments, err := arc.Comments(ctx, archive.CommentQuery{Author: name, Limit: n, Sort: "desc", Fields: archive.CommentFields})
			if err != nil {
				return err
			}
			fmt.Fprintf(stdout, "# u/%s\n\n## Active in\n", name)
			printUserSubs(subsRows)
			fmt.Fprintf(stdout, "\n## Latest posts\n")
			printPosts(posts, true, userBody)
			fmt.Fprintf(stdout, "\n## Latest comments\n")
			printComments(comments, true, userBody)
		default:
			return fmt.Errorf("unknown view %q: use posts, comments, subs or overview", mode)
		}
		return nil
	},
}

func printPosts(rows []archive.Row, withSub bool, body int) {
	if len(rows) == 0 {
		fmt.Fprintln(stdout, "_No posts._")
		return
	}
	for _, r := range rows {
		fmt.Fprintln(stdout, postLine(r, withSub, body))
	}
}

func printComments(rows []archive.Row, withSub bool, body int) {
	if len(rows) == 0 {
		fmt.Fprintln(stdout, "_No comments._")
		return
	}
	for _, r := range rows {
		fmt.Fprintln(stdout, commentLine(r, withSub, body))
	}
}

func printUserSubs(rows []archive.Row) {
	if len(rows) == 0 {
		fmt.Fprintln(stdout, "_No activity in the archive._")
		return
	}
	parts := make([]string, 0, len(rows))
	for _, r := range rows {
		parts = append(parts, fmt.Sprintf("r/%s (%d)", archive.Str(r, "subreddit"), archive.Num(r, "count")))
	}
	fmt.Fprintln(stdout, strings.Join(parts, ", "))
}

func init() {
	f := userCmd.Flags()
	f.IntVarP(&userLimit, "limit", "n", 25, "number of items (max 100)")
	f.StringVar(&userSub, "sub", "", "restrict posts/comments to subreddit(s)")
	f.StringVar(&userAfter, "after", "", "posted after: date, epoch or offset (30d, 1y)")
	f.IntVar(&userBody, "body", 160, "characters of body preview per item (0 = none)")
	rootCmd.AddCommand(userCmd)
}
