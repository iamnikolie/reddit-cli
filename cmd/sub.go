package cmd

import (
	"fmt"
	"strings"

	"github.com/iamnikolie/reddit-cli/internal/archive"
	"github.com/spf13/cobra"
)

var (
	subLimit  int
	subAfter  string
	subBefore string
	subFlair  string
	subBody   int
	subNSFW   string
)

var subCmd = &cobra.Command{
	Use:   "sub <name>[,<name>...] [new|top|hot|rising]",
	Short: "List a subreddit's posts: new/top from the archive, hot/rising live",
	Long: `List posts of one or more subreddits.

  new     newest posts first (default; archive, or live feed with --live)
  top     highest score within --after (default 7d); the archive settles
          scores ~36h after posting, so posts younger than that rank low
  hot     reddit's live front page of the subreddit (rss source: ~1 request
          per minute per IP, no scores in feeds)
  rising  reddit's live rising list (rss)`,
	Example: `  reddit sub golang -n 10
  reddit sub golang,rust new --after 24h
  reddit sub golang top --after 30d -n 20
  reddit sub golang hot -n 10
  reddit sub golang,rust rising
  reddit sub golang new --live
  reddit sub selfhosted --flair "Need Help" --body 0`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		subs := splitList(args[0])
		mode := "new"
		if len(args) == 2 {
			mode = args[1]
		}
		switch mode {
		case "hot", "rising":
			if err := needRSS("sub " + mode); err != nil {
				return err
			}
			return liveSub(cmd, subs, mode)
		case "new", "top":
			if useRSS(false) {
				return liveSub(cmd, subs, mode)
			}
		}
		q := archive.PostQuery{Subreddits: subs, After: subAfter, Before: subBefore, Flair: subFlair, Sort: "desc", Fields: leanFields(archive.PostFields)}
		if subNSFW != "" {
			v := subNSFW == "true" || subNSFW == "yes" || subNSFW == "only"
			q.NSFW = &v
		}
		switch mode {
		case "new":
			q.Limit = subLimit
		case "top":
			q.Limit = 100
			if q.After == "" {
				q.After = "7d"
			}
		default:
			return fmt.Errorf("unknown listing %q: use new or top", mode)
		}
		ctx, cancel := reqCtx(cmd)
		defer cancel()
		rows, err := arc.Posts(ctx, q)
		if err = partialOK(err); err != nil {
			return err
		}
		if mode == "top" {
			archive.SortByScore(rows)
			if subLimit > 0 && len(rows) > subLimit {
				rows = rows[:subLimit]
			}
			note("(top = best of the last %d archived posts since %s; scores settle ~36h after posting)", 100, q.After)
		}
		if format() != "md" {
			return emitRows(rows, []string{"id", "created_utc", "score", "num_comments", "subreddit", "title", "author", "url"})
		}
		if len(rows) == 0 {
			fmt.Fprintln(stdout, "_No posts._")
			return nil
		}
		for _, r := range rows {
			fmt.Fprintln(stdout, postLine(r, len(subs) > 1, subBody))
		}
		return nil
	},
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// leanFields returns the archive projection unless --fields asks for
// something else, in which case the full record is fetched.
func leanFields(defaults []string) []string {
	if len(fieldsFlag) > 0 {
		return nil
	}
	return defaults
}

func init() {
	f := subCmd.Flags()
	f.IntVarP(&subLimit, "limit", "n", 25, "number of posts (max 100)")
	f.StringVar(&subAfter, "after", "", "posted after: 2026-09-01, epoch, or offset like 24h, 7d, 3m")
	f.StringVar(&subBefore, "before", "", "posted before (same formats)")
	f.StringVar(&subFlair, "flair", "", "keyword match on link flair")
	f.IntVar(&subBody, "body", 160, "characters of self-text preview per post (0 = titles only)")
	f.StringVar(&subNSFW, "nsfw", "", "true/false to filter by over_18")
	rootCmd.AddCommand(subCmd)
}
