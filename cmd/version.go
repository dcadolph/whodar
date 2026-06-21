package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// version is the build version, overridden via -ldflags at release time.
var version = "dev"

// newVersionCmd builds the version command.
func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the whodar version",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), "whodar", version)
			return err
		},
	}
}
