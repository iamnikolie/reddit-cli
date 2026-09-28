package cmd

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
)

var apiQuery []string

// apiCmd is the escape hatch for archive endpoints without a dedicated command
// (aggregations, wikis, rules, interactions, time series).
var apiCmd = &cobra.Command{
	Use:   "api <path>",
	Short: "Call any archive endpoint and print the JSON response",
	Example: `  reddit api /api/posts/search/aggregate -q aggregate=author -q subreddit=golang -q after=30d
  reddit api /api/subreddits/rules -q subreddits=golang
  reddit api /api/subreddits/wikis/list -q subreddit=golang
  reddit api /api/time_series -q key=r/golang/posts/count -q precision=month -q after=1y
  reddit api /api/users/interactions/users -q author=spez -q min_count=10`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		path := args[0]
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		q := url.Values{}
		for _, kv := range apiQuery {
			k, v, ok := strings.Cut(kv, "=")
			if !ok {
				return fmt.Errorf("-q %q: want key=value", kv)
			}
			q.Add(k, v)
		}
		ctx, cancel := reqCtx(cmd)
		defer cancel()
		raw, err := arc.Raw(ctx, path, q)
		if err != nil {
			return err
		}
		if len(strings.TrimSpace(string(raw))) == 0 {
			return nil
		}
		return writeRaw(raw)
	},
}

func init() {
	apiCmd.Flags().StringArrayVarP(&apiQuery, "query", "q", nil, "query parameter key=value (repeat)")
	rootCmd.AddCommand(apiCmd)
}
