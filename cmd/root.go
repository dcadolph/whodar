package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/dcadolph/whodar/internal/policy"
)

// policyEnvVar names the environment variable pointing to an org policy file.
const policyEnvVar = "WHODAR_POLICY_FILE"

// defaultPolicyFile is the path an organization can pin a policy at.
const defaultPolicyFile = "/etc/whodar/policy.json"

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

// feedbackPath returns the feedback file path under the data directory. It is
// separate from the index so votes survive re-indexing.
func (o *options) feedbackPath() string {
	return filepath.Join(o.dataDir, "feedback.json")
}

// newRootCmd builds the root command, wires shared flags, and adds subcommands.
func newRootCmd() *cobra.Command {
	opts := &options{dataDir: defaultDataDir(), policyName: "strict"}

	root := &cobra.Command{
		Use:           "whodar",
		Short:         "Know who knows",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			return opts.resolvePolicy(cmd.Flags().Changed("policy"), cmd.ErrOrStderr())
		},
	}

	pf := root.PersistentFlags()
	pf.StringVar(&opts.dataDir, "data-dir", opts.dataDir, "Directory for the on-disk index.")
	pf.StringVar(&opts.policyName, "policy", opts.policyName, "Egress policy: strict, redacted, or open.")
	pf.BoolVar(&opts.pretty, "pretty", false, "Indent JSON output.")

	root.AddCommand(
		newIndexCmd(opts), newAskCmd(opts), newServeCmd(opts), newBotCmd(opts),
		newFeedbackCmd(opts), newVersionCmd())
	return root
}

// resolvePolicy sets o.pol from the org policy file and the user's flag. A
// locked org policy wins and ignores the flag; otherwise the file supplies the
// default mode and the flag may override it.
func (o *options) resolvePolicy(policyChanged bool, errOut io.Writer) error {
	cfg, found, err := policy.Load(policyFilePath())
	if err != nil {
		return err
	}
	if found && cfg.Locked {
		pol, err := cfg.Policy()
		if err != nil {
			return fmt.Errorf("org policy: %w", err)
		}
		o.pol = pol
		if policyChanged {
			fmt.Fprintln(errOut, "whodar: --policy ignored; pinned by org policy")
		}
		return nil
	}

	mode := o.policyName
	if found && !policyChanged && cfg.Mode != "" {
		mode = cfg.Mode
	}
	parsed, err := policy.ParseMode(mode)
	if err != nil {
		return err
	}
	o.pol = policy.New(parsed, false)
	return nil
}

// policyFilePath returns the org policy file path, preferring the environment.
func policyFilePath() string {
	if p := os.Getenv(policyEnvVar); p != "" {
		return p
	}
	return defaultPolicyFile
}

// defaultDataDir returns the default data directory under the user's home.
func defaultDataDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".whodar")
	}
	return ".whodar"
}
