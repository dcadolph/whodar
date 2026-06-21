package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/dcadolph/whodar/internal/connector"
	"github.com/dcadolph/whodar/internal/index"
)

// newIndexCmd builds the index command, which ingests a source into the index.
func newIndexCmd(opts *options) *cobra.Command {
	var (
		source string
		file   string
	)
	cmd := &cobra.Command{
		Use:   "index",
		Short: "Build the index from a source",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if source != "org-csv" {
				return fmt.Errorf("%w: %q (only org-csv is supported)", ErrUnknownSource, source)
			}
			if file == "" {
				return fmt.Errorf("%w: --file is required for org-csv", ErrBadArgs)
			}

			recs, err := connector.NewOrgCSV(file).Fetch(cmd.Context())
			if err != nil {
				return err
			}
			ix := index.New()
			ix.Build(recs)
			if err := ix.Save(opts.indexPath()); err != nil {
				return err
			}

			fmt.Fprintf(cmd.ErrOrStderr(),
				"indexed %d people, %d teams, %d topics into %s\n",
				len(ix.Graph.People), len(ix.Graph.Teams), len(ix.Graph.Topics), opts.indexPath())
			return nil
		},
	}
	cmd.Flags().StringVar(&source, "source", "org-csv", "Source type to ingest.")
	cmd.Flags().StringVar(&file, "file", "", "Path to the source file.")
	return cmd
}
