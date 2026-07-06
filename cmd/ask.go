package cmd

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dcadolph/whodar/internal/index"
	"github.com/dcadolph/whodar/internal/llm"
	"github.com/dcadolph/whodar/internal/policy"
	"github.com/dcadolph/whodar/internal/resolve"
)

// Cloud provider credentials come only from the environment, never flags.
const (
	// anthropicKeyEnv holds the Claude API key.
	anthropicKeyEnv = "WHODAR_ANTHROPIC_KEY"
	// openaiKeyEnv holds the OpenAI-compatible API key.
	openaiKeyEnv = "WHODAR_OPENAI_KEY"
)

// newAskCmd builds the ask command, which answers a question from the index.
func newAskCmd(opts *options) *cobra.Command {
	var (
		limit      int
		mode       string
		model      string
		embedModel string
		ollamaURL  string
		provider   string
		openaiURL  string
	)
	cmd := &cobra.Command{
		Use:   "ask [question]",
		Short: "Ask who to talk to about something",
		Long: `Answer a question from the index: the people to talk to and the channels to
ask in, each with reasons and a confidence from zero to one.

Modes:
  keyword   no model, deterministic, always works (default)
  semantic  match on meaning; needs an index built with --embed
  llm       a local Ollama model re-ranks and writes a recommendation

Examples:
  whodar ask "who do I talk to about billing retries"
  whodar ask --mode llm "where do I ask about kafka"`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ix, err := index.Load(opts.indexPath())
			if err != nil {
				return fmt.Errorf("%w: run `whodar index` first: %w", ErrNoIndex, err)
			}
			applyFeedback(ix, opts, cmd.ErrOrStderr())
			res, err := pickResolver(ix, opts, mode, model, embedModel, ollamaURL, provider, openaiURL)
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
	f.StringVar(&mode, "mode", "keyword", "Resolver: keyword, semantic, or llm.")
	f.StringVar(&model, "model", "", "Ollama chat model for --mode llm (default llama3.1).")
	f.StringVar(&embedModel, "embed-model", "", "Ollama embed model for semantic/llm (default nomic-embed-text).")
	f.StringVar(&ollamaURL, "ollama-url", "http://localhost:11434", "Ollama base URL.")
	f.StringVar(&provider, "provider", "ollama",
		"LLM provider: ollama, anthropic, or openai. Cloud providers need --policy redacted or open.")
	f.StringVar(&openaiURL, "openai-url", "",
		"OpenAI-compatible base URL, e.g. a local LM Studio or vLLM server.")
	return cmd
}

// pickResolver builds the resolver for the chosen mode. Semantic mode and the
// default ollama provider target a local server; anything non-local is gated
// by the egress policy. Cloud providers additionally run redacted under the
// redacted policy, so no names or emails leave the machine.
func pickResolver(ix *index.Index, opts *options, mode, model, embedModel, ollamaURL string, provider, openaiURL string) (resolve.Resolver, error) {
	switch mode {
	case "", "keyword":
		return resolve.NewKeyword(ix), nil
	case "semantic":
		if provider != "" && provider != "ollama" {
			return nil, fmt.Errorf("%w: semantic mode needs local embeddings; use --provider ollama", ErrBadArgs)
		}
		if err := guardLLMHost(opts.pol, ollamaURL); err != nil {
			return nil, err
		}
		return resolve.NewSemantic(ix, newOllama(model, embedModel, ollamaURL)), nil
	case "llm":
		switch provider {
		case "", "ollama":
			if err := guardLLMHost(opts.pol, ollamaURL); err != nil {
				return nil, err
			}
			client := newOllama(model, embedModel, ollamaURL)
			return resolve.NewLLM(ix, client, client), nil
		case "anthropic", "openai":
			chat, err := cloudChatter(opts.pol, provider, model, openaiURL)
			if err != nil {
				return nil, err
			}
			if opts.pol.Mode() == policy.Redacted {
				return resolve.NewRedactedLLM(ix, chat, nil), nil
			}
			return resolve.NewLLM(ix, chat, nil), nil
		default:
			return nil, fmt.Errorf("%w: provider %q (want ollama, anthropic, or openai)", ErrBadArgs, provider)
		}
	default:
		return nil, fmt.Errorf("%w: mode %q (want keyword, semantic, or llm)", ErrBadArgs, mode)
	}
}

// cloudChatter builds a chat client for the anthropic or openai provider,
// gated by the egress policy. Strict denies anything non-local; redacted and
// open permit, with redaction applied by the resolver under redacted. A local
// --openai-url, such as LM Studio, counts as local and needs no opt-in. Keys
// come only from the environment.
func cloudChatter(pol policy.Policy, provider, model, openaiURL string) (resolve.Chatter, error) {
	if provider == "anthropic" {
		if err := pol.AllowEgress("api.anthropic.com", 0); err != nil {
			return nil, cloudDenied(provider, err)
		}
		key := os.Getenv(anthropicKeyEnv)
		if key == "" {
			return nil, fmt.Errorf("%w: set %s", ErrBadArgs, anthropicKeyEnv)
		}
		return llm.NewAnthropic(key, llm.WithAnthropicModel(model)), nil
	}

	key := os.Getenv(openaiKeyEnv)
	var clientOpts []llm.OpenAIOption
	if model != "" {
		clientOpts = append(clientOpts, llm.WithOpenAIModel(model))
	}
	if openaiURL != "" {
		if err := guardLLMHost(pol, openaiURL); err != nil {
			return nil, err
		}
		clientOpts = append(clientOpts, llm.WithOpenAIBaseURL(openaiURL))
	} else {
		if err := pol.AllowEgress("api.openai.com", 0); err != nil {
			return nil, cloudDenied(provider, err)
		}
		if key == "" {
			return nil, fmt.Errorf("%w: set %s (or point --openai-url at a local server)", ErrBadArgs, openaiKeyEnv)
		}
	}
	return llm.NewOpenAI(key, clientOpts...), nil
}

// cloudDenied explains a policy denial with the way to opt in.
func cloudDenied(provider string, err error) error {
	return fmt.Errorf(
		"cloud provider %s: %w (use --policy redacted to send anonymized candidates, or --policy open)",
		provider, err)
}

// newOllama builds an Ollama client for the chat and embed models.
func newOllama(model, embedModel, ollamaURL string) *llm.Ollama {
	return llm.New(model, llm.WithBaseURL(ollamaURL), llm.WithEmbedModel(embedModel))
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
