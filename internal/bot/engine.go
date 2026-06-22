// Package bot answers whodar questions from Slack. It is transport-agnostic: a
// socket-mode or events-api adapter normalizes Slack events into Events and
// feeds them to the Engine, which resolves and replies through a Replier.
package bot

import (
	"context"
	"strings"

	"github.com/dcadolph/whodar/internal/resolve"
)

// Event is a normalized inbound message for the bot to answer.
type Event struct {
	// Text is the message text, possibly containing the bot mention.
	Text string
	// Channel is the channel or DM id to reply in.
	Channel string
	// ThreadTS is the thread timestamp to reply within, if any.
	ThreadTS string
}

// Replier sends a reply to Slack.
type Replier interface {
	// Reply posts text to channel, threading under threadTS when non-empty.
	Reply(ctx context.Context, channel, threadTS, text string) error
}

// AskFunc resolves a query in a mode, matching the CLI and web ask path.
type AskFunc func(ctx context.Context, query, mode string, limit int) (resolve.Answer, error)

// Engine answers events by resolving the query and formatting a Slack reply.
type Engine struct {
	// ask resolves queries.
	ask AskFunc
	// defaultMode is used when the message carries no mode hint.
	defaultMode string
	// limit caps results per section.
	limit int
	// botUserID is the bot's own user id, stripped from inbound text.
	botUserID string
}

// New returns an Engine. It panics if ask is nil, and applies sensible defaults
// for an empty mode or non-positive limit.
func New(ask AskFunc, defaultMode, botUserID string, limit int) *Engine {
	if ask == nil {
		panic("bot: New requires an Ask function")
	}
	if defaultMode == "" {
		defaultMode = "keyword"
	}
	if limit <= 0 {
		limit = 5
	}
	return &Engine{ask: ask, defaultMode: defaultMode, limit: limit, botUserID: botUserID}
}

// Handle answers ev and replies through r. An empty question is ignored. A
// resolve error is reported back to the user rather than failing the transport.
func (e *Engine) Handle(ctx context.Context, ev Event, r Replier) error {
	query, mode := e.parse(ev.Text)
	if query == "" {
		return nil
	}
	ans, err := e.ask(ctx, query, mode, e.limit)
	if err != nil {
		return r.Reply(ctx, ev.Channel, ev.ThreadTS, "Sorry, I hit an error: "+err.Error())
	}
	return r.Reply(ctx, ev.Channel, ev.ThreadTS, Format(query, ans))
}

// parse strips the bot mention and a mode hint, returning the cleaned query and
// the chosen mode. A trailing or inline "--llm"/"--keyword" (or "mode:llm")
// selects the mode; otherwise the default applies.
func (e *Engine) parse(text string) (query, mode string) {
	mode = e.defaultMode
	if e.botUserID != "" {
		text = strings.ReplaceAll(text, "<@"+e.botUserID+">", " ")
	}
	fields := strings.Fields(text)
	kept := fields[:0]
	for _, f := range fields {
		switch strings.ToLower(f) {
		case "--llm", "mode:llm":
			mode = "llm"
		case "--keyword", "mode:keyword":
			mode = "keyword"
		default:
			kept = append(kept, f)
		}
	}
	return strings.Join(kept, " "), mode
}
