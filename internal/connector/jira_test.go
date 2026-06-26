package connector

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/dcadolph/whodar/internal/jira"
)

// TestJiraFetch verifies the assignee and reporter get topics from components,
// labels, summary words, and project name, with email and account-id identity.
func TestJiraFetch(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `{"total":2,"startAt":0,"issues":[`+
			`{"key":"SEC-1","fields":{"summary":"Fix wiz scan flaky",`+
			`"assignee":{"accountId":"a1","displayName":"Jane Roe","emailAddress":"jane@x.com"},`+
			`"reporter":{"accountId":"p1","displayName":"Pat","emailAddress":"pat@x.com"},`+
			`"components":[{"name":"scanning"}],"labels":["wiz"],`+
			`"project":{"key":"SEC","name":"Security"}}},`+
			`{"key":"OPS-2","fields":{"summary":"Dashboard down",`+
			`"assignee":{"accountId":"b1","displayName":"Bob"},`+
			`"labels":["dashboard"],"project":{"key":"OPS","name":"Operations"}}}]}`)
	}))
	t.Cleanup(srv.Close)

	client := jira.New(srv.URL, "me@x.com", "token")
	recs, err := NewJiraWithClient(client, JiraOptions{Projects: []string{"SEC"}}).Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	byKey := make(map[string]Record)
	for _, r := range recs {
		key := r.PersonID
		if key == "" {
			key = r.Email
		}
		byKey[key] = r
	}

	if jane := byKey["jane@x.com"]; !slices.Contains(jane.Topics, "wiz") ||
		!slices.Contains(jane.Topics, "scan") || !slices.Contains(jane.Topics, "scanning") {
		t.Errorf("jane topics = %v, want wiz, scan, scanning", jane.Topics)
	}
	if bob := byKey["jira:b1"]; !slices.Contains(bob.Topics, "dashboard") {
		t.Errorf("bob topics = %v, want dashboard", bob.Topics)
	}
}
