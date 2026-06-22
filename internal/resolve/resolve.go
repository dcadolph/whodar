// Package resolve answers queries against an index through swappable resolvers.
// The keyword resolver needs no LLM. The LLM resolver retrieves candidates with
// the keyword index, then asks a local model to rank them and write a short
// recommendation, grounded so it cannot invent people or channels.
package resolve

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dcadolph/whodar/internal/index"
	"github.com/dcadolph/whodar/internal/model"
)

// Answer is a resolved response: ranked people, ranked channels, and an
// optional written summary that the LLM resolver fills in.
type Answer struct {
	// People is the ranked list of people to talk to.
	People []model.Match
	// Channels is the ranked list of places to ask.
	Channels []model.ChannelMatch
	// Summary is a short written recommendation; empty in keyword mode.
	Summary string
}

// Resolver answers a natural-language query.
type Resolver interface {
	// Resolve ranks up to limit people and channels for query.
	Resolve(ctx context.Context, query string, limit int) (Answer, error)
}

// ResolverFunc adapts a function to the Resolver interface.
type ResolverFunc func(ctx context.Context, query string, limit int) (Answer, error)

// Resolve calls f.
func (f ResolverFunc) Resolve(ctx context.Context, query string, limit int) (Answer, error) {
	return f(ctx, query, limit)
}

// Keyword is a non-LLM resolver that ranks by keyword and affinity scoring over
// the index. It needs no external services.
type Keyword struct {
	// ix is the index to search.
	ix *index.Index
}

// NewKeyword returns a Keyword resolver over ix. It panics if ix is nil.
func NewKeyword(ix *index.Index) *Keyword {
	if ix == nil {
		panic("resolve: NewKeyword requires a non-nil index")
	}
	return &Keyword{ix: ix}
}

// Resolve ranks people and channels for query using the index.
func (k *Keyword) Resolve(_ context.Context, query string, limit int) (Answer, error) {
	return Answer{
		People:   k.ix.Search(query, limit),
		Channels: k.ix.SearchChannels(query, limit),
	}, nil
}

// Chatter sends a system and user prompt to a model and returns its reply. The
// llm package's Ollama client satisfies it.
type Chatter interface {
	// Chat returns the model's reply to the system and user messages.
	Chat(ctx context.Context, system, user string) (string, error)
}

// llmSystem instructs the model to rank only provided candidates and reply as
// JSON, keeping the answer grounded in real data.
const llmSystem = `You help an employee find who to talk to and which channel to ask in.
You are given a question and a list of candidate people and channels retrieved from an
internal index. Pick and rank only the most relevant candidates. Never invent a person,
email, or channel that is not in the candidate list. Reply only as JSON of the form:
{"summary":"<one or two sentences naming the best person and channel>",
"people":["<email or name from candidates, best first>"],
"channels":["<channel name from candidates, best first>"]}.
If no candidate is relevant, return empty arrays and say so in the summary.`

// LLM is a resolver that retrieves candidates with the keyword index, then asks
// a local model to rank them and summarize. It stays grounded: model output is
// matched back to retrieved candidates and anything invented is dropped.
type LLM struct {
	// ix retrieves candidates.
	ix *index.Index
	// chat is the model client.
	chat Chatter
}

// NewLLM returns an LLM resolver. It panics if ix or chat is nil.
func NewLLM(ix *index.Index, chat Chatter) *LLM {
	if ix == nil || chat == nil {
		panic("resolve: NewLLM requires a non-nil index and chat client")
	}
	return &LLM{ix: ix, chat: chat}
}

// llmResult is the JSON the model is asked to return.
type llmResult struct {
	// Summary is the written recommendation.
	Summary string `json:"summary"`
	// People is the ranked list of person emails or names.
	People []string `json:"people"`
	// Channels is the ranked list of channel names.
	Channels []string `json:"channels"`
}

// Resolve retrieves candidates, asks the model to rank and summarize them, and
// returns the grounded result. If the model reply cannot be parsed, it falls
// back to keyword order and uses the raw reply as the summary.
func (l *LLM) Resolve(ctx context.Context, query string, limit int) (Answer, error) {
	n := candidateN(limit)
	people := l.ix.Search(query, n)
	channels := l.ix.SearchChannels(query, n)
	if len(people) == 0 && len(channels) == 0 {
		return Answer{}, nil
	}

	raw, err := l.chat.Chat(ctx, llmSystem, buildPrompt(query, people, channels))
	if err != nil {
		return Answer{}, fmt.Errorf("llm resolve: %w", err)
	}

	var out llmResult
	if jsonErr := json.Unmarshal([]byte(raw), &out); jsonErr != nil {
		return Answer{
			People:   capPeople(people, limit),
			Channels: capChannels(channels, limit),
			Summary:  strings.TrimSpace(raw),
		}, nil
	}
	return Answer{
		People:   capPeople(reorderPeople(people, out.People), limit),
		Channels: capChannels(reorderChannels(channels, out.Channels), limit),
		Summary:  strings.TrimSpace(out.Summary),
	}, nil
}

// candidateN returns how many candidates to retrieve for the model, a small
// multiple of the requested limit, bounded so prompts stay short.
func candidateN(limit int) int {
	n := limit * 2
	if limit <= 0 || n > 25 {
		n = 25
	}
	if n < 8 {
		n = 8
	}
	return n
}

// buildPrompt renders the question and candidates as the model's user message.
func buildPrompt(query string, people []model.Match, channels []model.ChannelMatch) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Question: %s\n\nCandidate people:\n", query)
	if len(people) == 0 {
		b.WriteString("(none)\n")
	}
	for i, m := range people {
		team := ""
		if m.Team != nil {
			team = m.Team.Name
		}
		fmt.Fprintf(&b, "%d. %s <%s> - %s; team %s; matched %s\n",
			i+1, m.Person.Name, m.Person.Email, m.Person.Title, team, strings.Join(m.Reasons, ", "))
	}
	b.WriteString("\nCandidate channels:\n")
	if len(channels) == 0 {
		b.WriteString("(none)\n")
	}
	for i, c := range channels {
		var members []string
		for _, p := range c.TopMembers {
			members = append(members, p.Name)
		}
		fmt.Fprintf(&b, "%d. #%s - topic: %s; active: %s\n",
			i+1, c.Channel.Name, c.Channel.Topic, strings.Join(members, ", "))
	}
	return b.String()
}

// reorderPeople ranks candidates by the model's order, matching on email or
// name, then appends any candidates the model omitted. Unknown entries from the
// model are ignored, keeping the result grounded.
func reorderPeople(cands []model.Match, order []string) []model.Match {
	byKey := make(map[string]model.Match, len(cands)*2)
	for _, m := range cands {
		if m.Person.Email != "" {
			byKey[strings.ToLower(m.Person.Email)] = m
		}
		if m.Person.Name != "" {
			byKey[strings.ToLower(m.Person.Name)] = m
		}
	}
	out := make([]model.Match, 0, len(cands))
	seen := make(map[model.ID]bool, len(cands))
	for _, key := range order {
		if m, ok := byKey[strings.ToLower(strings.TrimSpace(key))]; ok && !seen[m.Person.ID] {
			out = append(out, m)
			seen[m.Person.ID] = true
		}
	}
	for _, m := range cands {
		if !seen[m.Person.ID] {
			out = append(out, m)
			seen[m.Person.ID] = true
		}
	}
	return out
}

// reorderChannels ranks channel candidates by the model's order, matching on
// name, then appends any the model omitted. Unknown names are ignored.
func reorderChannels(cands []model.ChannelMatch, order []string) []model.ChannelMatch {
	byName := make(map[string]model.ChannelMatch, len(cands))
	for _, c := range cands {
		byName[strings.ToLower(c.Channel.Name)] = c
	}
	out := make([]model.ChannelMatch, 0, len(cands))
	seen := make(map[model.ID]bool, len(cands))
	for _, key := range order {
		key = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(key), "#")))
		if c, ok := byName[key]; ok && !seen[c.Channel.ID] {
			out = append(out, c)
			seen[c.Channel.ID] = true
		}
	}
	for _, c := range cands {
		if !seen[c.Channel.ID] {
			out = append(out, c)
			seen[c.Channel.ID] = true
		}
	}
	return out
}

// capPeople limits matches to limit; a non-positive limit returns all.
func capPeople(matches []model.Match, limit int) []model.Match {
	if limit > 0 && len(matches) > limit {
		return matches[:limit]
	}
	return matches
}

// capChannels limits matches to limit; a non-positive limit returns all.
func capChannels(matches []model.ChannelMatch, limit int) []model.ChannelMatch {
	if limit > 0 && len(matches) > limit {
		return matches[:limit]
	}
	return matches
}
