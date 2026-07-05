package cmd

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/dcadolph/whodar/internal/feedback"
	"github.com/dcadolph/whodar/internal/index"
)

// newFeedbackCmd builds the feedback command, which records a vote on an
// answer so future rankings favor confirmed results.
func newFeedbackCmd(opts *options) *cobra.Command {
	var (
		person     string
		channel    string
		helpful    bool
		notHelpful bool
	)
	cmd := &cobra.Command{
		Use:   "feedback [question]",
		Short: "Confirm or correct an answer",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if helpful == notHelpful {
				return fmt.Errorf("%w: pass exactly one of --helpful or --not-helpful", ErrBadArgs)
			}
			vote := feedback.Helpful
			if notHelpful {
				vote = feedback.NotHelpful
			}
			store, err := feedback.Load(opts.feedbackPath())
			if err != nil {
				return err
			}
			entry := feedback.Entry{
				Query:   strings.Join(args, " "),
				Person:  person,
				Channel: channel,
				Vote:    vote,
				Time:    time.Now(),
			}
			if err := store.Add(entry); err != nil {
				return fmt.Errorf("%w: %w", ErrBadArgs, err)
			}
			fmt.Fprintln(cmd.ErrOrStderr(), "feedback recorded")
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&person, "person", "", "Person identifier from the answer.")
	f.StringVar(&channel, "channel", "", "Channel name from the answer.")
	f.BoolVar(&helpful, "helpful", false, "The result answered the question.")
	f.BoolVar(&notHelpful, "not-helpful", false, "The result was wrong for the question.")
	return cmd
}

// applyFeedback loads stored votes onto the index, warning instead of failing
// when the feedback file is unreadable. It returns the store, or nil when the
// file could not be read.
func applyFeedback(ix *index.Index, opts *options, errOut io.Writer) *feedback.Store {
	store, err := feedback.Load(opts.feedbackPath())
	if err != nil {
		fmt.Fprintf(errOut, "feedback ignored: %v\n", err)
		return nil
	}
	ix.SetFeedback(store.All())
	return store
}
