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

Posts: the archive copes with one keyword per query, so the CLI sends the
longest word (the anchor) and applies the remaining words and "quoted
phrases" itself over the newest 100 anchor matches (all words must appear).
Narrow with --after/--before when the window is too wide. --in title matches
titles only; --in text (default) matches title + self-text.

--comments searches comment bodies with the archive's full-text syntax:
words are ANDed, "quoted phrase", a OR b, -excluded. Narrow with --after
when it times out.`,
	Example: `  reddit search "cobra urfave" --sub golang
  reddit search "\"claude code\"" --sub ClaudeAI,ChatGPTCoding --after 30d
  reddit search "sqlite" --sub golang --comments --after 90d -n 50
  reddit search "launch" --author spez --in title
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
		var rows []archive.Row
		if searchComments {
			limit := searchLimit
			if searchSort == "score" && limit < 100 {
				limit = 100
			}
			rows, err = arc.Comments(ctx, archive.CommentQuery{Subreddits: subs, Author: searchAuthor, Body: query, After: searchAfter, Before: searchBefore, Limit: limit, Sort: "desc", Fields: leanFields(archive.CommentFields)})
		} else {
			terms := parseTerms(query)
			if terms.anchor == "" {
				return fmt.Errorf("query has no searchable word")
			}
			limit := searchLimit
			if searchSort == "score" || terms.needsFilter() {
				limit = 100
			}
			q := archive.PostQuery{Subreddits: subs, Author: searchAuthor, After: searchAfter, Before: searchBefore, Limit: limit, Sort: "desc", Fields: leanFields(archive.PostFields)}
			switch searchIn {
			case "title":
				q.Title = terms.anchor
			case "text":
				q.Query = terms.anchor
			default:
				return fmt.Errorf("--in must be title or text")
			}
			rows, err = arc.Posts(ctx, q)
			if terms.needsFilter() && err == nil {
				fetched := len(rows)
				rows = terms.filter(rows, searchIn == "title")
				if fetched >= 100 && len(rows) < searchLimit {
					note("(matched %d of the newest %d posts with %q; narrow with --after/--before or --sub to search further back)", len(rows), fetched, terms.anchor)
				}
			}
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
	f.StringVar(&searchIn, "in", "text", "posts: where to match — text (title+body) or title")
	f.StringVar(&searchSort, "sort", "new", "order: new, old, score (score fetches 100 then sorts)")
	f.IntVar(&searchBody, "body", 200, "characters of body preview per result (0 = none)")
	rootCmd.AddCommand(searchCmd)
}

// terms is a parsed post query: quoted phrases, bare words and the anchor
// word sent to the archive (the longest bare word, or the longest word of the
// first phrase).
type terms struct {
	anchor  string
	words   []string
	phrases []string
}

var stopWords = map[string]bool{"the": true, "and": true, "for": true, "with": true, "that": true, "this": true, "from": true, "are": true, "you": true, "not": true, "how": true, "what": true, "why": true, "can": true, "does": true, "has": true, "have": true, "its": true, "our": true, "your": true, "but": true, "any": true, "all": true}

func parseTerms(q string) terms {
	var t terms
	rest := q
	for {
		i := strings.Index(rest, "\"")
		if i < 0 {
			break
		}
		j := strings.Index(rest[i+1:], "\"")
		if j < 0 {
			rest = rest[:i] + " " + rest[i+1:]
			break
		}
		if p := strings.Join(strings.Fields(rest[i+1:i+1+j]), " "); p != "" {
			t.phrases = append(t.phrases, strings.ToLower(p))
		}
		rest = rest[:i] + " " + rest[i+1+j+1:]
	}
	for _, w := range strings.Fields(rest) {
		w = strings.Trim(strings.ToLower(w), ",.;:!?()[]{}")
		if w != "" {
			t.words = append(t.words, w)
		}
	}
	pick := func(cands []string) {
		for _, c := range cands {
			if stopWords[c] && t.anchor != "" {
				continue
			}
			if len(c) > len(t.anchor) || (stopWords[t.anchor] && !stopWords[c]) {
				t.anchor = c
			}
		}
	}
	pick(t.words)
	if t.anchor == "" || stopWords[t.anchor] {
		for _, p := range t.phrases {
			pick(strings.Fields(p))
		}
	}
	return t
}

// needsFilter reports whether client-side matching is required beyond the
// single anchor word the archive searched for.
func (t terms) needsFilter() bool {
	return len(t.phrases) > 0 || len(t.words) > 1
}

// filter keeps rows whose title (and self-text unless titleOnly) contain
// every word and every phrase, case-insensitively.
func (t terms) filter(rows []archive.Row, titleOnly bool) []archive.Row {
	kept := rows[:0]
	for _, r := range rows {
		hay := strings.ToLower(archive.Str(r, "title"))
		if !titleOnly {
			hay += " " + strings.ToLower(archive.Str(r, "selftext"))
		}
		hay = strings.Join(strings.Fields(hay), " ")
		ok := true
		for _, w := range t.words {
			if !strings.Contains(hay, w) {
				ok = false
				break
			}
		}
		for _, p := range t.phrases {
			if ok && !strings.Contains(hay, p) {
				ok = false
			}
		}
		if ok {
			kept = append(kept, r)
		}
	}
	return kept
}
