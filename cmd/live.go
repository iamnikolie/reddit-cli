package cmd

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/iamnikolie/reddit-cli/internal/archive"
	"github.com/iamnikolie/reddit-cli/internal/rss"
	"github.com/spf13/cobra"
)

// The live source: reddit.com Atom feeds. One request per command, because
// reddit.com allows roughly one anonymous feed request per minute per IP.

func feedLimit(n int) string {
	if n <= 0 || n > 100 {
		n = 100
	}
	return strconv.Itoa(n)
}

func liveNote(what string) {
	note("(live reddit.com feed: %s; feeds carry no scores — the archive has them after ~36h)", what)
}

// liveSub lists /r/<a+b>/<hot|new|top|rising>/.rss.
func liveSub(cmd *cobra.Command, subs []string, mode string) error {
	q := url.Values{"limit": {feedLimit(subLimit)}}
	if mode == "top" {
		q.Set("t", orDefault(rss.TimeFilter(subAfter), "week"))
	}
	if subFlair != "" || subBefore != "" {
		note("(--flair/--before are archive-only filters; ignored on the live feed)")
	}
	ctx, cancel := reqCtx(cmd)
	defer cancel()
	entries, err := rssc.Feed(ctx, "/r/"+strings.Join(subs, "+")+"/"+mode+"/.rss", q)
	if err != nil {
		return err
	}
	rows := rss.Rows(entries)
	if subLimit > 0 && len(rows) > subLimit {
		rows = rows[:subLimit]
	}
	if format() != "md" {
		return emitRows(rows, []string{"id", "created_utc", "subreddit", "title", "author", "url"})
	}
	liveNote(mode + " order as reddit ranks it now")
	printPosts(rows, len(subs) > 1, subBody)
	return nil
}

// livePost renders /comments/<id>/.rss: the post followed by comments in feed
// order, flat (feeds carry neither parent ids nor scores).
func livePost(cmd *cobra.Command, id string) error {
	q := url.Values{"limit": {feedLimit(postTop)}}
	ctx, cancel := reqCtx(cmd)
	defer cancel()
	entries, err := rssc.Feed(ctx, "/comments/"+id+"/.rss", q)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return fmt.Errorf("post %s: empty feed (deleted, private or not a post)", id)
	}
	rows := rss.Rows(entries)
	var post archive.Row
	var comments []archive.Row
	for _, r := range rows {
		if post == nil && r["title"] != nil && r["body"] == nil {
			post = r
			continue
		}
		if r["body"] != nil {
			comments = append(comments, r)
		}
	}
	if format() == "json" {
		return writeJSON(stdout, map[string]any{"post": post, "comments": comments})
	}
	if format() != "md" {
		return emitRows(comments, []string{"id", "created_utc", "author", "body"})
	}
	if post != nil {
		body := postBody
		if postNoBody {
			body = -1
		}
		fmt.Fprint(stdout, postHeader(post, body))
	}
	if postTop > 0 && len(comments) > postTop {
		comments = comments[:postTop]
	}
	fmt.Fprintf(stdout, "\n## Comments (live feed: %d shown, flat, no scores; use the archive source for the scored tree)\n", len(comments))
	if len(comments) == 0 {
		fmt.Fprintln(stdout, "_No comments._")
		return nil
	}
	for _, c := range comments {
		id := ""
		if postIDs {
			id = " #" + archive.Str(c, "id")
		}
		fmt.Fprintf(stdout, "- u/%s · %s%s: %s\n", archive.Str(c, "author"), age(archive.Num(c, "created_utc")), id, truncate(archive.Str(c, "body"), postMaxChars))
	}
	return nil
}

// liveUser lists /user/<name>/submitted|comments/.rss.
func liveUser(cmd *cobra.Command, name, mode string) error {
	var path string
	switch mode {
	case "overview", "posts":
		path = "/user/" + name + "/submitted/.rss"
		if mode == "overview" {
			note("(live feed shows posts only; 'user %s comments --live' for comments, the archive for the overview)", name)
		}
	case "comments":
		path = "/user/" + name + "/comments/.rss"
	case "subs":
		return fmt.Errorf("'user subs' is archive-only; drop --live")
	default:
		return fmt.Errorf("unknown view %q: use posts, comments, subs or overview", mode)
	}
	if userSub != "" || userAfter != "" {
		note("(--sub/--after are archive-only filters; ignored on the live feed)")
	}
	ctx, cancel := reqCtx(cmd)
	defer cancel()
	entries, err := rssc.Feed(ctx, path, url.Values{"limit": {feedLimit(userLimit)}})
	if err != nil {
		return err
	}
	rows := rss.Rows(entries)
	if userLimit > 0 && len(rows) > userLimit {
		rows = rows[:userLimit]
	}
	if mode == "comments" {
		if format() != "md" {
			return emitRows(rows, []string{"id", "created_utc", "subreddit", "link_id", "body"})
		}
		printComments(rows, true, userBody)
		return nil
	}
	if format() != "md" {
		return emitRows(rows, []string{"id", "created_utc", "subreddit", "title", "url"})
	}
	printPosts(rows, true, userBody)
	return nil
}

// liveSearch uses reddit's own search feed: /r/<a+b>/search.rss (or the
// site-wide /search.rss when no subreddit is given).
func liveSearch(cmd *cobra.Command, query string, subs []string) error {
	if searchComments {
		return fmt.Errorf("comment search is archive-only; drop --live")
	}
	if searchAuthor != "" {
		query = "author:" + strings.TrimPrefix(searchAuthor, "u/") + " " + query
	}
	if searchIn == "title" {
		query = "title:" + query
	}
	sort := map[string]string{"new": "new", "old": "new", "score": "top"}[searchSort]
	if sort == "" {
		return fmt.Errorf("--sort must be new, old or score")
	}
	q := url.Values{"q": {query}, "sort": {sort}, "limit": {feedLimit(searchLimit)}}
	if t := rss.TimeFilter(searchAfter); t != "" {
		q.Set("t", t)
	}
	path := "/search.rss"
	if len(subs) > 0 {
		path = "/r/" + strings.Join(subs, "+") + "/search.rss"
		q.Set("restrict_sr", "1")
	}
	ctx, cancel := reqCtx(cmd)
	defer cancel()
	entries, err := rssc.Feed(ctx, path, q)
	if err != nil {
		return err
	}
	rows := rss.Rows(entries)
	posts := rows[:0]
	for _, r := range rows {
		if archive.Str(r, "kind") == "post" {
			posts = append(posts, r)
		}
	}
	rows = posts
	if searchSort == "old" {
		archive.SortByDate(rows, false)
	}
	if searchLimit > 0 && len(rows) > searchLimit {
		rows = rows[:searchLimit]
	}
	if format() != "md" {
		return emitRows(rows, []string{"id", "created_utc", "subreddit", "title", "author", "url"})
	}
	liveNote("reddit's own search, " + sort + " first")
	printPosts(rows, len(subs) != 1, searchBody)
	return nil
}

// liveSubs uses /subreddits/search.rss.
func liveSubs(cmd *cobra.Command, query string) error {
	ctx, cancel := reqCtx(cmd)
	defer cancel()
	entries, err := rssc.Feed(ctx, "/subreddits/search.rss", url.Values{"q": {query}, "limit": {feedLimit(subsLimit)}})
	if err != nil {
		return err
	}
	rows := rss.Rows(entries)
	if subsLimit > 0 && len(rows) > subsLimit {
		rows = rows[:subsLimit]
	}
	if format() != "md" {
		return emitRows(rows, []string{"display_name", "title", "created_utc", "description", "permalink"})
	}
	if len(rows) == 0 {
		fmt.Fprintln(stdout, "_No subreddits._")
		return nil
	}
	liveNote("reddit's own subreddit search; no subscriber counts in feeds")
	for _, r := range rows {
		fmt.Fprintf(stdout, "- r/%s · %s", archive.Str(r, "display_name"), archive.Str(r, "title"))
		if d := truncate(archive.Str(r, "description"), 160); d != "" {
			fmt.Fprintf(stdout, " — %s", d)
		}
		fmt.Fprintln(stdout)
	}
	return nil
}
