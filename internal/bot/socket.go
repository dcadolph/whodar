package bot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/coder/websocket"

	"github.com/dcadolph/whodar/internal/slack"
)

// wsConn is the minimal WebSocket the socket runner needs.
type wsConn interface {
	// Read returns the next text message.
	Read(ctx context.Context) ([]byte, error)
	// Write sends a text message.
	Write(ctx context.Context, data []byte) error
	// Close closes the connection.
	Close() error
}

// Dialer opens a WebSocket to url.
type Dialer func(ctx context.Context, url string) (wsConn, error)

// socketEnvelope is a Socket Mode frame.
type socketEnvelope struct {
	// Type is the frame type: hello, disconnect, or events_api.
	Type string `json:"type"`
	// EnvelopeID identifies the frame for acknowledgment.
	EnvelopeID string `json:"envelope_id"`
	// Payload carries the event for events_api frames.
	Payload struct {
		// Event is the Slack event.
		Event slackEvent `json:"event"`
	} `json:"payload"`
}

// slackEvent is the subset of a Slack event the bot reads.
type slackEvent struct {
	// Type is the event type, such as app_mention or message.
	Type string `json:"type"`
	// Text is the message text.
	Text string `json:"text"`
	// Channel is the channel or DM id.
	Channel string `json:"channel"`
	// User is the author's user id.
	User string `json:"user"`
	// TS is the message timestamp.
	TS string `json:"ts"`
	// ThreadTS is the thread timestamp, if any.
	ThreadTS string `json:"thread_ts"`
	// BotID is set when a bot authored the message.
	BotID string `json:"bot_id"`
	// ChannelType is "im" for direct messages.
	ChannelType string `json:"channel_type"`
}

// SocketRunner runs a Slack Socket Mode session: it opens a WebSocket with the
// app-level token, reads event frames, acknowledges them, and dispatches
// questions to the Engine. It reconnects until the context is canceled.
type SocketRunner struct {
	// app is the app-level token client used to open connections.
	app *slack.Client
	// engine answers questions.
	engine *Engine
	// replier posts answers back to Slack.
	replier Replier
	// botUserID is the bot's own user id, used to ignore its own messages.
	botUserID string
	// dial opens the WebSocket; overridable for tests.
	dial Dialer
	// log receives connection notices.
	log io.Writer
}

// SocketOption configures a SocketRunner.
type SocketOption func(*SocketRunner)

// WithDialer overrides the WebSocket dialer, for tests.
func WithDialer(d Dialer) SocketOption {
	return func(s *SocketRunner) {
		if d != nil {
			s.dial = d
		}
	}
}

// WithLog sets where connection notices are written.
func WithLog(w io.Writer) SocketOption {
	return func(s *SocketRunner) {
		if w != nil {
			s.log = w
		}
	}
}

// NewSocketRunner builds a SocketRunner. It panics on nil dependencies.
func NewSocketRunner(app *slack.Client, engine *Engine, replier Replier, botUserID string, opts ...SocketOption) *SocketRunner {
	if app == nil || engine == nil || replier == nil {
		panic("bot: NewSocketRunner requires app, engine, and replier")
	}
	s := &SocketRunner{
		app: app, engine: engine, replier: replier, botUserID: botUserID,
		dial: dialWebSocket, log: io.Discard,
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Run opens connections and processes events until ctx is canceled. A dropped
// connection is reopened.
func (s *SocketRunner) Run(ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			return nil
		}
		url, err := s.app.ConnectionsOpen(ctx)
		if err != nil {
			return fmt.Errorf("socket: open: %w", err)
		}
		conn, err := s.dial(ctx, url)
		if err != nil {
			return fmt.Errorf("socket: dial: %w", err)
		}
		err = s.session(ctx, conn)
		if ctx.Err() != nil {
			return nil
		}
		fmt.Fprintf(s.log, "whodar bot: reconnecting after: %v\n", err)
	}
}

// session reads and dispatches frames until the connection ends.
func (s *SocketRunner) session(ctx context.Context, conn wsConn) error {
	defer conn.Close()
	for {
		data, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		var env socketEnvelope
		if json.Unmarshal(data, &env) != nil {
			continue
		}
		switch env.Type {
		case "hello":
			continue
		case "disconnect":
			return errors.New("server requested disconnect")
		default:
			if env.EnvelopeID != "" {
				s.ack(ctx, conn, env.EnvelopeID)
			}
			if env.Type == "events_api" {
				routeEvent(ctx, s.engine, s.replier, s.botUserID, env.Payload.Event, s.log)
			}
		}
	}
}

// ack acknowledges a frame so Slack does not redeliver it.
func (s *SocketRunner) ack(ctx context.Context, conn wsConn, id string) {
	body, err := json.Marshal(map[string]string{"envelope_id": id})
	if err != nil {
		return
	}
	_ = conn.Write(ctx, body)
}

// routeEvent answers a mention or direct message, ignoring the bot's own and
// other bots' messages. It is shared by the socket and events transports.
func routeEvent(ctx context.Context, e *Engine, r Replier, botUserID string, ev slackEvent, log io.Writer) {
	if ev.BotID != "" || (botUserID != "" && ev.User == botUserID) {
		return
	}
	mention := ev.Type == "app_mention"
	directMessage := ev.Type == "message" && ev.ChannelType == "im"
	if !mention && !directMessage {
		return
	}
	thread := ev.ThreadTS
	if thread == "" && mention {
		thread = ev.TS
	}
	event := Event{Text: ev.Text, Channel: ev.Channel, ThreadTS: thread}
	if err := e.Handle(ctx, event, r); err != nil {
		fmt.Fprintf(log, "whodar bot: handle: %v\n", err)
	}
}

// dialWebSocket is the production dialer backed by the WebSocket library.
func dialWebSocket(ctx context.Context, url string) (wsConn, error) {
	c, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		return nil, err
	}
	c.SetReadLimit(1 << 20)
	return &coderConn{c: c}, nil
}

// coderConn adapts the WebSocket library connection to wsConn.
type coderConn struct {
	// c is the underlying connection.
	c *websocket.Conn
}

// Read returns the next text message.
func (cc *coderConn) Read(ctx context.Context) ([]byte, error) {
	_, data, err := cc.c.Read(ctx)
	return data, err
}

// Write sends a text message.
func (cc *coderConn) Write(ctx context.Context, data []byte) error {
	return cc.c.Write(ctx, websocket.MessageText, data)
}

// Close closes the connection normally.
func (cc *coderConn) Close() error {
	return cc.c.Close(websocket.StatusNormalClosure, "")
}
