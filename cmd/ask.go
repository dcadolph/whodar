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
	// People is the ranked list of people to talk to.
	People []personOut `json:"people"`
	// Channels is the ranked list of places to ask.
	Channels []channelOut `json:"channels,omitempty"`
}

// personOut is one ranked person in an answer.
type personOut struct {
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

// channelOut is one ranked channel in an answer.
type channelOut struct {
	// Name is the channel name.
	Name string `json:"name"`
	// Topic is the channel's stated topic.
	Topic string `json:"topic,omitempty"`
	// Score is the relevance score.
	Score float64 `json:"score"`
	// Reasons explains why the channel matched.
	Reasons []string `json:"reasons,omitempty"`
	// Members are the most relevant people active in the channel.
	Members []memberOut `json:"members,omitempty"`
}

// memberOut is one active member of a channel.
type memberOut struct {
	// Name is the member's display name.
	Name string `json:"name"`
	// Email is the member's work email.
	Email string `json:"email,omitempty"`
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
			people, err := resolve.NewKeyword(ix).Resolve(cmd.Context(), query, limit)
			if err != nil {
				return err
			}
			out := answer{
				Query:    query,
				People:   peopleOut(people),
				Channels: channelsOut(ix.SearchChannels(query, limit)),
			}
			return writeJSON(cmd.OutOrStdout(), out, opts.pretty)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 5, "Maximum number of results per section.")
	return cmd
}

// peopleOut converts person matches into the JSON output shape.
func peopleOut(matches []model.Match) []personOut {
	out := make([]personOut, 0, len(matches))
	for _, m := range matches {
		po := personOut{
			Name:    m.Person.Name,
			Email:   m.Person.Email,
			Title:   m.Person.Title,
			Score:   m.Score,
			Reasons: m.Reasons,
		}
		if m.Team != nil {
			po.Team = m.Team.Name
		}
		out = append(out, po)
	}
	return out
}

// channelsOut converts channel matches into the JSON output shape.
func channelsOut(matches []model.ChannelMatch) []channelOut {
	out := make([]channelOut, 0, len(matches))
	for _, m := range matches {
		co := channelOut{
			Name:    m.Channel.Name,
			Topic:   m.Channel.Topic,
			Score:   m.Score,
			Reasons: m.Reasons,
		}
		for _, p := range m.TopMembers {
			co.Members = append(co.Members, memberOut{Name: p.Name, Email: p.Email})
		}
		out = append(out, co)
	}
	return out
}
