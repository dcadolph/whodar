package connector

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/dcadolph/whodar/internal/github"
)

// TestGitHubFetch verifies repo topics, PR labels, reviewers, and CODEOWNERS all
// become records.
func TestGitHubFetch(t *testing.T) {
	t.Parallel()
	owners := base64.StdEncoding.EncodeToString([]byte("/internal/ @kim"))
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/o/r", func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `{"name":"billing-service","full_name":"o/r","topics":["billing"]}`)
	})
	mux.HandleFunc("/repos/o/r/contributors", func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `[{"login":"jane","contributions":10}]`)
	})
	mux.HandleFunc("/repos/o/r/pulls", func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `[{"user":{"login":"jane"},"labels":[{"name":"retries"}],`+
			`"requested_reviewers":[{"login":"bob"}]}]`)
	})
	mux.HandleFunc("/repos/o/r/contents/CODEOWNERS", func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `{"encoding":"base64","content":"`+owners+`"}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := github.New("ghp-test", github.WithBaseURL(srv.URL))
	recs, err := NewGitHubWithClient(client, GitHubOptions{Repos: []string{"o/r"}}).Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	byID := make(map[string]Record)
	for _, r := range recs {
		byID[r.PersonID] = r
	}

	jane := byID["github:jane"]
	if !slices.Contains(jane.Topics, "billing") || !slices.Contains(jane.Topics, "retries") {
		t.Errorf("jane topics = %v, want billing and retries", jane.Topics)
	}
	if !slices.Contains(jane.Topics, "service") {
		t.Errorf("jane topics = %v, want repo-name word service", jane.Topics)
	}
	if bob := byID["github:bob"]; !slices.Contains(bob.Topics, "retries") {
		t.Errorf("reviewer bob topics = %v, want retries", bob.Topics)
	}
	if kim := byID["codeowners:kim"]; kim.Name != "@kim" {
		t.Errorf("codeowners record = %+v, want @kim", kim)
	}
}
