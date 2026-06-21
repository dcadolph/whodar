package connector

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/dcadolph/whodar/internal/slack"
)

// TestSlackFetch verifies the connector turns Slack data into person and
// channel records, joining message authors to channel membership by email.
func TestSlackFetch(t *testing.T) {
	t.Parallel()
	const (
		usersJSON = `{"ok":true,"members":[
			{"id":"U1","profile":{"real_name":"Jane Roe","email":"jane@x.com","title":"Staff Engineer"}},
			{"id":"U2","profile":{"real_name":"Bob Lee","email":"bob@x.com","title":"SRE"}}]}`
		channelsJSON = `{"ok":true,"channels":[
			{"id":"C1","name":"billing","topic":{"value":"retries and dunning"},
			 "purpose":{"value":"billing platform"}}]}`
		historyJSON = `{"ok":true,"has_more":false,"messages":[
			{"type":"message","user":"U1","text":"we fixed the retries bug","ts":"1.0"},
			{"type":"message","user":"U2","text":"kafka lag again","ts":"2.0"},
			{"type":"message","subtype":"channel_join","user":"U1","text":"joined","ts":"3.0"},
			{"type":"message","bot_id":"B1","text":"deploy done","ts":"4.0"}]}`
	)

	mux := http.NewServeMux()
	mux.HandleFunc("/users.list", func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, usersJSON)
	})
	mux.HandleFunc("/conversations.list", func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, channelsJSON)
	})
	mux.HandleFunc("/conversations.history", func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, historyJSON)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := slack.New("xoxb-test", slack.WithBaseURL(srv.URL))
	recs, err := NewSlackWithClient(client, SlackOptions{}).Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	var channel *Record
	people := make(map[string]Record)
	janeTalksRetries := false
	for i := range recs {
		r := recs[i]
		switch r.Kind {
		case KindChannel:
			channel = &recs[i]
		case KindPerson:
			if r.Name != "" {
				people[r.PersonID] = r
			}
			if r.PersonID == "jane@x.com" && strings.Contains(r.Text, "retries") {
				janeTalksRetries = true
			}
		}
	}

	if channel == nil {
		t.Fatal("no channel record emitted")
	}
	if channel.Name != "billing" || channel.Title != "retries and dunning" {
		t.Errorf("channel = %+v, want billing / retries and dunning", channel)
	}
	if !slices.Contains(channel.Members, "jane@x.com") || !slices.Contains(channel.Members, "bob@x.com") {
		t.Errorf("members = %v, want jane and bob", channel.Members)
	}
	if len(channel.Members) != 2 {
		t.Errorf("members = %v, want exactly 2 (bot and join skipped)", channel.Members)
	}
	if people["jane@x.com"].Email != "jane@x.com" || people["jane@x.com"].Title != "Staff Engineer" {
		t.Errorf("jane person record = %+v", people["jane@x.com"])
	}
	if !janeTalksRetries {
		t.Error("expected a person record giving Jane retries affinity from her messages")
	}
}
