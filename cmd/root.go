// Package cmd implements the reddit CLI commands.
package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"time"

	"github.com/iamnikolie/reddit-cli/internal/archive"
	"github.com/iamnikolie/reddit-cli/internal/client"
	"github.com/iamnikolie/reddit-cli/internal/config"
	"github.com/spf13/cobra"
)

// version is stamped at link time by the Makefile and by GoReleaser
// (-X github.com/iamnikolie/reddit-cli/cmd.version=…). An unstamped build says
// "dev" rather than claiming a version it does not have.
var version = "dev"

func buildVersion() string {
	v := version
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" {
				rev := s.Value
				if len(rev) > 12 {
					rev = rev[:12]
				}
				v += " (" + rev + ")"
			}
		}
	}
	return v
}

var (
	jsonOutput   bool
	profile      string
	verbose      bool
	outputFormat string
	fieldsFlag   []string
	timeout      time.Duration
	source       string

	cfg *config.Config
	arc *archive.Client
)

var (
	stdout io.Writer = os.Stdout
	stderr io.Writer = os.Stderr
	stdin  io.Reader = os.Stdin
)

var rootCmd = &cobra.Command{
	Use:   "reddit",
	Short: "Reddit CLI — read subreddits, threads, search and users from the terminal",
	Long: "Reddit CLI — read subreddits, threads, search and users from the terminal.\n\n" +
		"Sources: archive (Arctic Shift, no credentials, default).\n" +
		"Run 'reddit skill' to print the full agent reference (commands, flags, workflows).",
	SilenceUsage: true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		switch outputFormat {
		case "", "md", "table", "json", "csv", "tsv":
		default:
			return fmt.Errorf("--format must be one of md, table, json, csv, tsv")
		}
		switch source {
		case "archive":
		default:
			return fmt.Errorf("--source %q is not available; this build supports: archive", source)
		}
		if profileExempt(cmd) {
			return nil
		}
		var err error
		cfg, err = config.Load(profile)
		if err != nil {
			return err
		}
		ua := cfg.UserAgent
		if ua == "" {
			ua = "reddit-cli/" + version + " (https://github.com/iamnikolie/reddit-cli)"
		}
		c := client.New("archive", cfg.ArchiveURL, ua)
		c.Verbose = verbose
		arc = archive.New(c)
		return nil
	},
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.ExecuteContext(context.Background()); err != nil {
		os.Exit(1)
	}
}

func init() {
	def := os.Getenv("REDDIT_CONFIG")
	if def == "" {
		def = config.DefaultProfile
	}
	pf := rootCmd.PersistentFlags()
	pf.BoolVar(&jsonOutput, "json", false, "output JSON (same as --format json)")
	pf.StringVar(&profile, "config", def, "profile name (subdirectory of ~/.reddit-cli/; env REDDIT_CONFIG)")
	pf.BoolVar(&verbose, "verbose", false, "print requests and statuses to stderr")
	pf.StringVar(&outputFormat, "format", "", "output format: md (default), table, json, csv, tsv")
	pf.StringSliceVar(&fieldsFlag, "fields", nil, "comma-separated output fields for table/json/csv, e.g. id,title,score")
	pf.DurationVar(&timeout, "timeout", 2*time.Minute, "per-request timeout (0 disables)")
	pf.StringVar(&source, "source", "archive", "data source: archive")

	rootCmd.Version = buildVersion()
	rootCmd.AddCommand(versionCmd)
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show the reddit version",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Fprintf(stdout, "reddit version %s\n", buildVersion())
		return nil
	},
}

// profileExempt reports whether a command runs without loading a profile.
func profileExempt(cmd *cobra.Command) bool {
	switch cmd.Name() {
	case "reddit", "skill", "help", "completion", "version", "bash", "zsh", "fish", "powershell":
		return true
	}
	return cmd.Parent() != nil && cmd.Parent().Name() == "config"
}

// reqCtx bounds a single request by --timeout.
func reqCtx(cmd *cobra.Command) (context.Context, context.CancelFunc) {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	if timeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, timeout)
}
