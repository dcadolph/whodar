package bot

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"
)

// maxBody bounds the request body the events handler reads.
const maxBody = 1 << 20

// EventsHandler serves the Slack Events API. It verifies the request signature,
// answers the url_verification handshake, and dispatches event callbacks.
type EventsHandler struct {
	// engine answers questions.
	engine *Engine
	// replier posts answers back to Slack.
	replier Replier
	// botUserID is the bot's own user id, used to ignore its own messages.
	botUserID string
	// signingSecret verifies request signatures.
	signingSecret string
	// maxSkew is the allowed clock skew for the request timestamp.
	maxSkew time.Duration
	// now returns the current time; overridable for tests.
	now func() time.Time
	// log receives handler notices.
	log io.Writer
}

// EventsOption configures an EventsHandler.
type EventsOption func(*EventsHandler)

// WithClock overrides the clock, for tests.
func WithClock(now func() time.Time) EventsOption {
	return func(h *EventsHandler) {
		if now != nil {
			h.now = now
		}
	}
}

// WithEventsLog sets where handler notices are written.
func WithEventsLog(w io.Writer) EventsOption {
	return func(h *EventsHandler) {
		if w != nil {
			h.log = w
		}
	}
}

// NewEventsHandler builds an EventsHandler. It panics on a nil engine or
// replier, or an empty signing secret.
func NewEventsHandler(engine *Engine, replier Replier, botUserID, signingSecret string, opts ...EventsOption) *EventsHandler {
	if engine == nil || replier == nil {
		panic("bot: NewEventsHandler requires an engine and a replier")
	}
	if signingSecret == "" {
		panic("bot: NewEventsHandler requires a signing secret")
	}
	h := &EventsHandler{
		engine: engine, replier: replier, botUserID: botUserID,
		signingSecret: signingSecret, maxSkew: 5 * time.Minute,
		now: time.Now, log: io.Discard,
	}
	for _, o := range opts {
		o(h)
	}
	return h
}

// ServeHTTP verifies the request, answers url_verification, and dispatches
// event callbacks asynchronously so Slack receives a fast acknowledgment.
func (h *EventsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
	if err != nil {
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}
	ts := r.Header.Get("X-Slack-Request-Timestamp")
	if !h.freshTimestamp(ts) {
		http.Error(w, "stale or missing timestamp", http.StatusBadRequest)
		return
	}
	if !h.verify(r.Header.Get("X-Slack-Signature"), ts, body) {
		http.Error(w, "bad signature", http.StatusUnauthorized)
		return
	}

	var outer struct {
		// Type is the payload type.
		Type string `json:"type"`
		// Challenge is echoed for url_verification.
		Challenge string `json:"challenge"`
		// Event is the Slack event for event_callback.
		Event slackEvent `json:"event"`
	}
	if err := json.Unmarshal(body, &outer); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}

	switch outer.Type {
	case "url_verification":
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, outer.Challenge)
	case "event_callback":
		w.WriteHeader(http.StatusOK)
		ev := outer.Event
		go routeEvent(context.Background(), h.engine, h.replier, h.botUserID, ev, h.log)
	default:
		w.WriteHeader(http.StatusOK)
	}
}

// freshTimestamp reports whether ts is a unix time within the allowed skew.
func (h *EventsHandler) freshTimestamp(ts string) bool {
	n, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return false
	}
	delta := h.now().Unix() - n
	if delta < 0 {
		delta = -delta
	}
	return time.Duration(delta)*time.Second <= h.maxSkew
}

// verify checks the Slack request signature over the raw body.
func (h *EventsHandler) verify(sig, ts string, body []byte) bool {
	mac := hmac.New(sha256.New, []byte(h.signingSecret))
	_, _ = io.WriteString(mac, "v0:"+ts+":")
	mac.Write(body)
	expected := "v0=" + hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(sig))
}

// compile-time guard that EventsHandler is an http.Handler.
var _ http.Handler = (*EventsHandler)(nil)
