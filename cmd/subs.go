package cmd

import (
	"fmt"

	"github.com/iamnikolie/reddit-cli/internal/archive"
	"github.com/spf13/cobra"
)

var (
	subsLimit   int
	subsMin     int
	subsExact   bool
	subsSort    string
	subsNSFWOff bool
)

var subsCmd = &cobra.Command{
	Use:   "subs <prefix>",
	Short: "Find subreddits by name prefix (or one exactly with --exact)",
	Long: `Search the subreddit index by name prefix, biggest first. The index is
refreshed infrequently, so subscriber counts are approximate.`,
	Example: `  reddit subs golang
  reddit subs claude --min-subscribers 1000
  reddit subs selfhosted --exact`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		q := archive.SubredditQuery{Prefix: args[0], MinSubscribers: subsMin, Limit: subsLimit, SortType: subsSort, Sort: "desc"}
		if subsExact {
			q.Name, q.Prefix = args[0], ""
		}
		if subsSort == "subreddit" {
			q.Sort = "asc"
		}
		ctx, cancel := reqCtx(cmd)
		defer cancel()
		rows, err := arc.Subreddits(ctx, q)
		if err != nil {
			return err
		}
		if subsNSFWOff {
			kept := rows[:0]
			for _, r := range rows {
				if b, _ := r["over18"].(bool); !b {
					kept = append(kept, r)
				}
			}
			rows = kept
		}
		if format() != "md" {
			return emitRows(rows, []string{"display_name", "subscribers", "created_utc", "over18", "title", "public_description"})
		}
		if len(rows) == 0 {
			fmt.Fprintln(stdout, "_No subreddits._")
			return nil
		}
		for _, r := range rows {
			fmt.Fprintf(stdout, "- r/%s · %s subs · since %s", archive.Str(r, "display_name"), humanCount(archive.Num(r, "subscribers")), dateTime(archive.Num(r, "created_utc"))[:4])
			if b, _ := r["over18"].(bool); b {
				fmt.Fprint(stdout, " · NSFW")
			}
			if d := truncate(archive.Str(r, "public_description"), 160); d != "" {
				fmt.Fprintf(stdout, " — %s", d)
			}
			fmt.Fprintln(stdout)
		}
		return nil
	},
}

func humanCount(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 10_000:
		return fmt.Sprintf("%dk", n/1000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1e3)
	}
	return fmt.Sprintf("%d", n)
}

func init() {
	f := subsCmd.Flags()
	f.IntVarP(&subsLimit, "limit", "n", 15, "number of subreddits")
	f.IntVar(&subsMin, "min-subscribers", 0, "minimum subscriber count")
	f.BoolVar(&subsExact, "exact", false, "look up one subreddit by exact name")
	f.StringVar(&subsSort, "sort", "subscribers", "order: subscribers, created_utc, subreddit")
	f.BoolVar(&subsNSFWOff, "sfw", false, "hide NSFW subreddits")
	rootCmd.AddCommand(subsCmd)
}
