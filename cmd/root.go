package cmd

import (
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/dcadolph/whodar/internal/policy"
)

// options holds the shared flags resolved on the root command.
type options struct {
	// dataDir is the directory holding the on-disk index.
	dataDir string
	// policyName is the requested egress policy mode.
	policyName string
	// pretty indents JSON output when true.
	pretty bool
	// pol is the resolved egress policy, set before any subcommand runs.
	pol policy.Policy
}

// indexPath returns the index file path under the data directory.
func (o *options) indexPath() string {
	return filepath.Join(o.dataDir, "index.json")
}

// newRootCmd builds the root command, wires shared flags, and adds subcommands.
func newRootCmd() *cobra.Command {
	opts := &options{dataDir: defaultDataDir(), policyName: "strict"}

	root := &cobra.Command{
		Use:           "whodar",
		Short:         "Find who to talk to about X",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
			mode, err := policy.ParseMode(opts.policyName)
			if err != nil {
				return err
			}
			opts.pol = policy.New(mode, false)
			return nil
		},
	}

	pf := root.PersistentFlags()
	pf.StringVar(&opts.dataDir, "data-dir", opts.dataDir, "Directory for the on-disk index.")
	pf.StringVar(&opts.policyName, "policy", opts.policyName, "Egress policy: strict, redacted, or open.")
	pf.BoolVar(&opts.pretty, "pretty", false, "Indent JSON output.")

	root.AddCommand(newIndexCmd(opts), newAskCmd(opts), newServeCmd(opts), newVersionCmd())
	return root
}

// defaultDataDir returns the default data directory under the user's home.
func defaultDataDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".whodar")
	}
	return ".whodar"
}
