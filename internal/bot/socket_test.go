package bot

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/dcadolph/whodar/internal/resolve"
	"github.com/dcadolph/whodar/internal/slack"
)

// fakeConn yields canned frames then EOF, recording writes.
type fakeConn struct {
	reads  [][]byte
	idx    int
	writes [][]byte
}

// Read returns the next canned frame or io.EOF.
func (f *fakeConn) Read(context.Context) ([]byte, error) {
	if f.idx >= len(f.reads) {
		return nil, io.EOF
	}
	b := f.reads[f.idx]
	f.idx++
	return b, nil
}

// Write records the sent frame.
func (f *fakeConn) Write(_ context.Context, data []byte) error {
	f.writes = append(f.writes, append([]byte(nil), data...))
	return nil
}

// Close is a no-op.
func (f *fakeConn) Close() error { return nil }

// stubApp returns a client with a dummy app token; session does not call it.
func stubApp(t *testing.T) *slack.Client {
	t.Helper()
	return slack.New("xapp-test")
}

// okEngine returns an engine whose ask always answers.
func okEngine() *Engine {
	return New(func(context.Context, string, string, int) (resolve.Answer, error) {
		return sampleAnswer(), nil
	}, "keyword", "UBOT", 5)
}

// TestSocketSessionDispatchesAndAcks verifies a mention is answered and acked.
func TestSocketSessionDispatchesAndAcks(t *testing.T) {
	t.Parallel()
	rec := &recorder{}
	s := NewSocketRunner(stubApp(t), okEngine(), rec, "UBOT")
	conn := &fakeConn{reads: [][]byte{
		[]byte(`{"type":"hello"}`),
		[]byte(`{"type":"events_api","envelope_id":"e1","payload":{"event":{` +
			`"type":"app_mention","text":"<@UBOT> billing","channel":"C1","user":"U2","ts":"5.5"}}}`),
	}}

	_ = s.session(context.Background(), conn)

	if rec.calls != 1 || rec.channel != "C1" {
		t.Fatalf("expected one reply to C1, got %+v", rec)
	}
	if rec.thread != "5.5" {
		t.Errorf("mention reply should thread on ts, got %q", rec.thread)
	}
	acked := false
	for _, w := range conn.writes {
		var m map[string]string
		if json.Unmarshal(w, &m) == nil && m["envelope_id"] == "e1" {
			acked = true
		}
	}
	if !acked {
		t.Error("envelope e1 was not acked")
	}
}

// TestSocketIgnoresOwnAndOther verifies self, bot, and non-mention messages are
// not answered.
func TestSocketIgnoresOwnAndOther(t *testing.T) {
	t.Parallel()
	rec := &recorder{}
	s := NewSocketRunner(stubApp(t), okEngine(), rec, "UBOT")
	conn := &fakeConn{reads: [][]byte{
		[]byte(`{"type":"events_api","envelope_id":"e1","payload":{"event":{` +
			`"type":"app_mention","text":"hi","channel":"C1","user":"UBOT","ts":"1"}}}`),
		[]byte(`{"type":"events_api","envelope_id":"e2","payload":{"event":{` +
			`"type":"message","text":"hi","channel":"C1","bot_id":"B1"}}}`),
		[]byte(`{"type":"events_api","envelope_id":"e3","payload":{"event":{` +
			`"type":"message","channel_type":"channel","text":"hi","channel":"C1","user":"U2"}}}`),
	}}

	_ = s.session(context.Background(), conn)

	if rec.calls != 0 {
		t.Errorf("should ignore self, bot, and non-mention messages, calls=%d", rec.calls)
	}
}
