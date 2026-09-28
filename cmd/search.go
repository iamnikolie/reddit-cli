package cmd

import (
	"fmt"
	"strings"

	"github.com/iamnikolie/reddit-cli/internal/archive"
	"github.com/spf13/cobra"
)

var (
	searchSubs     string
	searchAuthor   string
	searchAfter    string
	searchBefore   string
	searchLimit    int
	searchComments bool
	searchIn       string
	searchSort     string
	searchBody     int
)

var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Keyword search in posts (or comments) of a subreddit or an author",
	Long: `Keyword search. The archive has no global full-text index: scope the
search with --sub (one or more, comma-separated) or --author.

Where it looks (--in):
  title   post titles; handles several words and "quoted phrases" reliably
  text    title + self-text; the archive only copes with a single keyword
          here and times out on longer queries
  auto    (default) title for multi-word queries, text for a single word

--comments searches comment bodies with full-text syntax: words are ANDed,
"quoted phrase", a OR b, -excluded. Narrow with --after when it times out.`,
	Example: `  reddit search "cobra urfave" --sub golang
  reddit search "\"claude code\"" --sub ClaudeAI,ChatGPTCoding --after 30d
  reddit search "sqlite" --sub golang --comments --after 90d -n 50
  reddit search "launch" --author spez --in text
  reddit search "raycast" --sub macapps --sort score`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		query, err := readArg(args[0])
		if err != nil {
			return err
		}
		subs := splitList(searchSubs)
		if len(subs) == 0 && searchAuthor == "" {
			return fmt.Errorf("scope the search: add --sub <name>[,<name>] or --author <user> (the archive has no global full-text search; find subreddits with 'reddit subs <prefix>')")
		}
		ctx, cancel := reqCtx(cmd)
		defer cancel()
		limit := searchLimit
		if searchSort == "score" && limit < 100 {
			limit = 100
		}
		var rows []archive.Row
		if searchComments {
			rows, err = arc.Comments(ctx, archive.CommentQuery{Subreddits: subs, Author: searchAuthor, Body: query, After: searchAfter, Before: searchBefore, Limit: limit, Sort: "desc", Fields: leanFields(archive.CommentFields)})
		} else {
			q := archive.PostQuery{Subreddits: subs, Author: searchAuthor, After: searchAfter, Before: searchBefore, Limit: limit, Sort: "desc", Fields: leanFields(archive.PostFields)}
			in := searchIn
			if in == "auto" {
				in = "text"
				if len(strings.Fields(query)) > 1 {
					in = "title"
					note("(multi-word query: searching titles; --in text also searches bodies but the archive usually times out on it)")
				}
			}
			switch in {
			case "title":
				q.Title = query
			case "text":
				q.Query = query
			default:
				return fmt.Errorf("--in must be auto, title or text")
			}
			rows, err = arc.Posts(ctx, q)
		}
		if err = partialOK(err); err != nil {
			return err
		}
		switch searchSort {
		case "score":
			archive.SortByScore(rows)
		case "old":
			archive.SortByDate(rows, false)
		case "new":
		default:
			return fmt.Errorf("--sort must be new, old or score")
		}
		if len(rows) > searchLimit {
			rows = rows[:searchLimit]
		}
		if format() != "md" {
			if searchComments {
				return emitRows(rows, []string{"id", "created_utc", "score", "subreddit", "author", "link_id", "body"})
			}
			return emitRows(rows, []string{"id", "created_utc", "score", "num_comments", "subreddit", "title", "author", "url"})
		}
		if len(rows) == 0 {
			fmt.Fprintln(stdout, "_No results._")
			return nil
		}
		withSub := len(subs) != 1
		for _, r := range rows {
			if searchComments {
				fmt.Fprintln(stdout, commentLine(r, withSub, searchBody))
			} else {
				fmt.Fprintln(stdout, postLine(r, withSub, searchBody))
			}
		}
		return nil
	},
}

func init() {
	f := searchCmd.Flags()
	f.StringVar(&searchSubs, "sub", "", "subreddit(s) to search, comma-separated")
	f.StringVar(&searchAuthor, "author", "", "restrict to one author")
	f.StringVar(&searchAfter, "after", "", "posted after: date, epoch or offset (7d, 3m, 1y)")
	f.StringVar(&searchBefore, "before", "", "posted before")
	f.IntVarP(&searchLimit, "limit", "n", 25, "number of results (max 100)")
	f.BoolVar(&searchComments, "comments", false, "search comment bodies instead of posts")
	f.StringVar(&searchIn, "in", "auto", "posts: where to match — auto, title, text (title+body)")
	f.StringVar(&searchSort, "sort", "new", "order: new, old, score (score fetches 100 then sorts)")
	f.IntVar(&searchBody, "body", 200, "characters of body preview per result (0 = none)")
	rootCmd.AddCommand(searchCmd)
}
