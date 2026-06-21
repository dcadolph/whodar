package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/dcadolph/whodar/internal/connector"
	"github.com/dcadolph/whodar/internal/index"
)

// slackTokenEnv is the environment variable holding the Slack bot token.
const slackTokenEnv = "WHODAR_SLACK_TOKEN"

// newIndexCmd builds the index command, which ingests a source into the index.
func newIndexCmd(opts *options) *cobra.Command {
	var (
		source         string
		file           string
		includePrivate bool
		sinceDays      int
		maxMessages    int
	)
	cmd := &cobra.Command{
		Use:   "index",
		Short: "Build the index from a source",
		RunE: func(cmd *cobra.Command, _ []string) error {
			var (
				recs []connector.Record
				err  error
			)
			switch source {
			case "org-csv":
				if file == "" {
					return fmt.Errorf("%w: --file is required for org-csv", ErrBadArgs)
				}
				recs, err = connector.NewOrgCSV(file).Fetch(cmd.Context())
			case "slack":
				recs, err = fetchSlack(cmd, opts, slackArgs{includePrivate, sinceDays, maxMessages})
			default:
				return fmt.Errorf("%w: %q (want org-csv or slack)", ErrUnknownSource, source)
			}
			if err != nil {
				return err
			}

			ix := index.New()
			ix.Build(recs)
			if err := ix.Save(opts.indexPath()); err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(),
				"indexed %d people, %d channels, %d teams, %d topics into %s\n",
				len(ix.Graph.People), len(ix.Graph.Channels), len(ix.Graph.Teams),
				len(ix.Graph.Topics), opts.indexPath())
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&source, "source", "org-csv", "Source type: org-csv or slack.")
	f.StringVar(&file, "file", "", "Path to the source file (org-csv).")
	f.BoolVar(&includePrivate, "include-private", false, "Ingest private Slack channels if policy allows.")
	f.IntVar(&sinceDays, "since-days", 180, "Slack history window in days.")
	f.IntVar(&maxMessages, "max-messages", 5000, "Slack message cap per channel.")
	return cmd
}

// slackArgs holds the Slack-specific index flags.
type slackArgs struct {
	// includePrivate requests private-channel ingest.
	includePrivate bool
	// sinceDays is the history window in days.
	sinceDays int
	// maxMessages caps messages per channel.
	maxMessages int
}

// fetchSlack builds Slack records, enforcing the private-channel policy guard.
func fetchSlack(cmd *cobra.Command, opts *options, a slackArgs) ([]connector.Record, error) {
	token := os.Getenv(slackTokenEnv)
	if token == "" {
		return nil, fmt.Errorf("%w: set %s", ErrBadArgs, slackTokenEnv)
	}
	if a.includePrivate && !opts.pol.AllowPrivateChannels() {
		return nil, fmt.Errorf("%w: private-channel ingest is disabled by policy", ErrBadArgs)
	}
	src := connector.NewSlack(token, connector.SlackOptions{
		IncludePrivate: a.includePrivate,
		SinceDays:      a.sinceDays,
		MaxMessages:    a.maxMessages,
		Log:            cmd.ErrOrStderr(),
	})
	return src.Fetch(cmd.Context())
}
