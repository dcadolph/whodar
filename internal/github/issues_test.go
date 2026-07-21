package github

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

// TestIssues verifies issues decode, the endpoint path is escaped, pull
// requests are distinguished, and the accessors read authors, labels, and
// assignees.
func TestIssues(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/repos/acme/app/issues" {
			t.Errorf("path = %s, want /repos/acme/app/issues", got)
		}
		_, _ = io.WriteString(w, `[
			{"title":"Fix retries","user":{"login":"jane"},
			 "labels":[{"name":"bug"}],"assignees":[{"login":"kim"}]},
			{"title":"Add cache","user":{"login":"bob"},"pull_request":{"url":"x"}}
		]`)
	}))
	t.Cleanup(srv.Close)

	issues, err := New("token", WithBaseURL(srv.URL)).Issues(context.Background(), "acme", "app")
	if err != nil {
		t.Fatalf("Issues: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("issues = %d, want 2", len(issues))
	}

	first := issues[0]
	if first.IsPullRequest() {
		t.Error("first issue should not be a pull request")
	}
	if first.Author() != "jane" {
		t.Errorf("author = %q, want jane", first.Author())
	}
	if !slices.Contains(first.LabelNames(), "bug") {
		t.Errorf("labels = %v, want bug", first.LabelNames())
	}
	if !slices.Contains(first.AssigneeLogins(), "kim") {
		t.Errorf("assignees = %v, want kim", first.AssigneeLogins())
	}
	if !issues[1].IsPullRequest() {
		t.Error("second issue carries pull_request and should report as a pull request")
	}
}
