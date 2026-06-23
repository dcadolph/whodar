package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

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
		changesFile    string
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

			var changes index.Changes
			if old, lerr := index.Load(opts.indexPath()); lerr == nil {
				changes = index.Diff(old.Graph, ix.Graph)
			}
			if err := ix.Save(opts.indexPath()); err != nil {
				return err
			}

			out := cmd.ErrOrStderr()
			fmt.Fprintf(out,
				"indexed %d people, %d channels, %d teams, %d topics into %s\n",
				len(ix.Graph.People), len(ix.Graph.Channels), len(ix.Graph.Teams),
				len(ix.Graph.Topics), opts.indexPath())
			reportChanges(out, changes)
			if changesFile != "" {
				if err := writeChangesFile(changesFile, changes); err != nil {
					return err
				}
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&source, "source", "org-csv", "Source type: org-csv or slack.")
	f.StringVar(&file, "file", "", "Path to the source file (org-csv).")
	f.BoolVar(&includePrivate, "include-private", false, "Ingest private Slack channels if policy allows.")
	f.IntVar(&sinceDays, "since-days", 180, "Slack history window in days.")
	f.IntVar(&maxMessages, "max-messages", 5000, "Slack message cap per channel.")
	f.StringVar(&changesFile, "changes-file", "", "Write the index diff as JSON to this path.")
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

// reportChanges prints a one-line summary and capped lists of who and what
// joined or left since the last index.
func reportChanges(w io.Writer, c index.Changes) {
	if c.Empty() {
		return
	}
	fmt.Fprintf(w, "changes since last index: %s\n", c.Summary())
	printChangeList(w, "joined", c.PeopleJoined)
	printChangeList(w, "left", c.PeopleLeft)
	printChangeList(w, "new channels", c.ChannelsAdded)
	printChangeList(w, "gone channels", c.ChannelsRemoved)
}

// printChangeList prints up to a fixed number of items under a label, noting
// any remainder.
func printChangeList(w io.Writer, label string, items []string) {
	const limit = 15
	if len(items) == 0 {
		return
	}
	shown := items
	if len(shown) > limit {
		shown = shown[:limit]
	}
	fmt.Fprintf(w, "  %s: %s", label, strings.Join(shown, ", "))
	if len(items) > limit {
		fmt.Fprintf(w, ", and %d more", len(items)-limit)
	}
	fmt.Fprintln(w)
}

// writeChangesFile writes the changes as JSON to path.
func writeChangesFile(path string, c index.Changes) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("changes file: %w", err)
	}
	defer f.Close()
	return writeJSON(f, c, true)
}
