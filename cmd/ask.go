package cmd

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dcadolph/whodar/internal/index"
	"github.com/dcadolph/whodar/internal/llm"
	"github.com/dcadolph/whodar/internal/policy"
	"github.com/dcadolph/whodar/internal/resolve"
)

// newAskCmd builds the ask command, which answers a question from the index.
func newAskCmd(opts *options) *cobra.Command {
	var (
		limit     int
		mode      string
		model     string
		ollamaURL string
	)
	cmd := &cobra.Command{
		Use:   "ask [question]",
		Short: "Ask who to talk to about something",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ix, err := index.Load(opts.indexPath())
			if err != nil {
				return fmt.Errorf("%w: run `whodar index` first: %w", ErrNoIndex, err)
			}
			res, err := pickResolver(ix, opts, mode, model, ollamaURL)
			if err != nil {
				return err
			}
			query := strings.Join(args, " ")
			ans, err := res.Resolve(cmd.Context(), query, limit)
			if err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), ans.View(query), opts.pretty)
		},
	}
	f := cmd.Flags()
	f.IntVar(&limit, "limit", 5, "Maximum number of results per section.")
	f.StringVar(&mode, "mode", "keyword", "Resolver: keyword (no LLM) or llm (local Ollama).")
	f.StringVar(&model, "model", "", "Ollama model for --mode llm (default llama3.1).")
	f.StringVar(&ollamaURL, "ollama-url", "http://localhost:11434", "Ollama base URL for --mode llm.")
	return cmd
}

// pickResolver builds the resolver for the chosen mode. LLM mode targets a local
// Ollama server; a non-local server is gated by the egress policy.
func pickResolver(ix *index.Index, opts *options, mode, model, ollamaURL string) (resolve.Resolver, error) {
	switch mode {
	case "", "keyword":
		return resolve.NewKeyword(ix), nil
	case "llm":
		if err := guardLLMHost(opts.pol, ollamaURL); err != nil {
			return nil, err
		}
		return resolve.NewLLM(ix, llm.New(model, llm.WithBaseURL(ollamaURL))), nil
	default:
		return nil, fmt.Errorf("%w: mode %q (want keyword or llm)", ErrBadArgs, mode)
	}
}

// guardLLMHost permits a loopback Ollama address unconditionally and requires
// egress permission for any other host.
func guardLLMHost(pol policy.Policy, raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%w: invalid ollama url %q: %v", ErrBadArgs, raw, err)
	}
	switch u.Hostname() {
	case "", "localhost", "127.0.0.1", "::1":
		return nil
	}
	if err := pol.AllowEgress(u.Hostname(), 0); err != nil {
		return fmt.Errorf("llm host %s: %w", u.Hostname(), err)
	}
	return nil
}
