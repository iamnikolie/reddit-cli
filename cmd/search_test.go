package cmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iamnikolie/reddit-cli/internal/archive"
	"github.com/iamnikolie/reddit-cli/internal/client"
)

// stubArchive answers each search with the response picked for the request's
// subreddit; unknown subreddits get an empty data array.
func stubArchive(t *testing.T, bySub map[string]func(http.ResponseWriter)) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h, ok := bySub[r.URL.Query().Get("subreddit")]; ok {
			h(w)
			return
		}
		w.Write([]byte(`{"data":[]}`))
	}))
	t.Cleanup(srv.Close)

	c := client.New("archive", srv.URL, "test")
	c.RetryWait = 0
	oldArc, oldOut, oldErr := arc, stdout, stderr
	oldSrc, oldFmt, oldJSON := source, outputFormat, jsonOutput
	t.Cleanup(func() {
		arc, stdout, stderr = oldArc, oldOut, oldErr
		source, outputFormat, jsonOutput = oldSrc, oldFmt, oldJSON
		searchSubs, searchAuthor, searchAfter, searchBefore = "", "", "", ""
		searchLimit, searchComments, searchIn, searchSort, searchBody = 25, false, "text", "new", 200
	})
	arc = archive.New(c)
	source, outputFormat, jsonOutput = "archive", "", false
	searchAuthor, searchAfter, searchBefore = "", "", ""
	searchLimit, searchIn, searchSort, searchBody = 5, "text", "new", 0
}

func rateLimited(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "58")
	w.WriteHeader(http.StatusTooManyRequests)
	w.Write([]byte(`{"error":"Too many requests"}`))
}

func timedOut(w http.ResponseWriter) {
	w.WriteHeader(http.StatusUnprocessableEntity)
	w.Write([]byte(`{"error":"Timeout. Maybe slow down a bit"}`))
}

func runSearch(query string) (string, string, error) {
	var out, errb bytes.Buffer
	stdout, stderr = &out, &errb
	err := searchCmd.RunE(searchCmd, []string{query})
	return out.String(), errb.String(), err
}

func TestSearchArchiveErrorIsNotNoResults(t *testing.T) {
	cases := []struct {
		name     string
		comments bool
		query    string
		answer   func(http.ResponseWriter)
		want     string
	}{
		{"comments 429", true, "Viewpoint", rateLimited, "HTTP 429"},
		{"comments 422", true, "Viewpoint", timedOut, "HTTP 422"},
		{"posts 429", false, "Viewpoint", rateLimited, "HTTP 429"},
		{"posts 422", false, "Viewpoint", timedOut, "HTTP 422"},
		{"posts filtered 429", false, "viewpoint schedule", rateLimited, "HTTP 429"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			stubArchive(t, map[string]func(http.ResponseWriter){"ConstructionManagers": c.answer})
			searchSubs, searchComments = "ConstructionManagers", c.comments
			out, _, err := runSearch(c.query)
			if err == nil {
				t.Fatalf("want an error, got none; stdout %q", out)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error %q does not mention %q", err, c.want)
			}
			if strings.Contains(out, "No results") {
				t.Errorf("stdout claims no results on an archive error: %q", out)
			}
		})
	}
}

func TestSearchPartialStillPrints(t *testing.T) {
	for _, comments := range []bool{true, false} {
		stubArchive(t, map[string]func(http.ResponseWriter){
			"good": func(w http.ResponseWriter) {
				w.Write([]byte(`{"data":[{"id":"a1","title":"viewpoint schedule","selftext":"","body":"viewpoint schedule","author":"x","subreddit":"good","created_utc":1700000000,"score":3}]}`))
			},
			"bad": rateLimited,
		})
		searchSubs, searchComments = "good,bad", comments
		out, errs, err := runSearch("viewpoint schedule")
		if err != nil {
			t.Fatalf("comments=%v: partial result returned an error: %v", comments, err)
		}
		if !strings.Contains(out, "viewpoint schedule") {
			t.Errorf("comments=%v: partial rows missing from stdout: %q", comments, out)
		}
		if !strings.Contains(errs, "warning: partial results") {
			t.Errorf("comments=%v: no partial warning on stderr: %q", comments, errs)
		}
	}
}
