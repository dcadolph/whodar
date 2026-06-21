package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dcadolph/whodar/internal/index"
	"github.com/dcadolph/whodar/internal/model"
	"github.com/dcadolph/whodar/internal/resolve"
)

// answer is the JSON shape emitted by the ask command.
type answer struct {
	// Query echoes the question asked.
	Query string `json:"query"`
	// Matches is the ranked list of people to talk to.
	Matches []matchOut `json:"matches"`
}

// matchOut is one ranked person in an answer.
type matchOut struct {
	// Name is the person's display name.
	Name string `json:"name"`
	// Email is the person's work email.
	Email string `json:"email,omitempty"`
	// Title is the person's job title.
	Title string `json:"title,omitempty"`
	// Team is the person's team name.
	Team string `json:"team,omitempty"`
	// Score is the relevance score.
	Score float64 `json:"score"`
	// Reasons explains why the person matched.
	Reasons []string `json:"reasons,omitempty"`
}

// newAskCmd builds the ask command, which answers a question from the index.
func newAskCmd(opts *options) *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "ask [question]",
		Short: "Ask who to talk to about something",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ix, err := index.Load(opts.indexPath())
			if err != nil {
				return fmt.Errorf("%w: run `whodar index` first: %w", ErrNoIndex, err)
			}
			query := strings.Join(args, " ")
			matches, err := resolve.NewKeyword(ix).Resolve(cmd.Context(), query, limit)
			if err != nil {
				return err
			}
			out := answer{Query: query, Matches: toMatchOut(matches)}
			return writeJSON(cmd.OutOrStdout(), out, opts.pretty)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 5, "Maximum number of people to return.")
	return cmd
}

// toMatchOut converts model matches into the JSON output shape.
func toMatchOut(matches []model.Match) []matchOut {
	out := make([]matchOut, 0, len(matches))
	for _, m := range matches {
		mo := matchOut{
			Name:    m.Person.Name,
			Email:   m.Person.Email,
			Title:   m.Person.Title,
			Score:   m.Score,
			Reasons: m.Reasons,
		}
		if m.Team != nil {
			mo.Team = m.Team.Name
		}
		out = append(out, mo)
	}
	return out
}
