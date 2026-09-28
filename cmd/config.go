package cmd

import (
	"fmt"

	"github.com/iamnikolie/reddit-cli/internal/config"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Show the profile configuration",
	Long: `The archive source needs no credentials, so a profile is optional.
Settings live in ~/.reddit-cli/<profile>/config.yaml:

  user_agent:   sent on every request (default: reddit-cli/<version>)
  archive_url:  Arctic Shift base URL (default: ` + config.DefaultArchiveURL + `)

Environment overrides: REDDIT_CONFIG (profile), REDDIT_ARCHIVE_URL, REDDIT_USER_AGENT.`,
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Print the effective configuration",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := config.Load(profile)
		if err != nil {
			return err
		}
		path, _ := config.Path(profile)
		fmt.Fprintf(stdout, "profile: %s\nfile: %s\narchive_url: %s\nuser_agent: %s\n", profile, path, c.ArchiveURL, orDefault(c.UserAgent, "(default)"))
		return nil
	},
}

var configPathCmd = &cobra.Command{
	Use:   "path",
	Short: "Print the config file path",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		path, err := config.Path(profile)
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, path)
		return nil
	},
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func init() {
	configCmd.AddCommand(configShowCmd, configPathCmd)
	rootCmd.AddCommand(configCmd)
}
