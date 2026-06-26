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

// TestGitHubFetch verifies topics come from repo metadata, PR labels and titles,
// reviewers and assignees, non-PR issues, and CODEOWNERS, and that pull requests
// returned by the issues endpoint are skipped.
func TestGitHubFetch(t *testing.T) {
	t.Parallel()
	owners := base64.StdEncoding.EncodeToString([]byte("/internal/ @kim"))
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/o/r", func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `{"name":"billing-service","full_name":"o/r",`+
			`"description":"Wiz scanning integration","topics":["billing"]}`)
	})
	mux.HandleFunc("/repos/o/r/contributors", func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `[{"login":"jane","contributions":10}]`)
	})
	mux.HandleFunc("/repos/o/r/pulls", func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `[{"title":"Fix wiz scan flakiness","user":{"login":"jane"},`+
			`"labels":[{"name":"retries"}],"requested_reviewers":[{"login":"bob"}],`+
			`"assignees":[{"login":"carol"}]}]`)
	})
	mux.HandleFunc("/repos/o/r/issues", func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `[{"user":{"login":"dan"},"labels":[{"name":"dashboard"}],`+
			`"title":"Wiz dashboard broken"},`+
			`{"user":{"login":"ghost"},"labels":[{"name":"shouldskip"}],"title":"x",`+
			`"pull_request":{"url":"y"}}]`)
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

	if jane := byID["github:jane"]; !slices.Contains(jane.Topics, "wiz") ||
		!slices.Contains(jane.Topics, "scan") || !slices.Contains(jane.Topics, "retries") {
		t.Errorf("jane topics = %v, want wiz, scan, retries", jane.Topics)
	}
	if bob := byID["github:bob"]; !slices.Contains(bob.Topics, "wiz") {
		t.Errorf("reviewer bob topics = %v, want wiz", bob.Topics)
	}
	if carol := byID["github:carol"]; !slices.Contains(carol.Topics, "wiz") {
		t.Errorf("assignee carol topics = %v, want wiz", carol.Topics)
	}
	if dan := byID["github:dan"]; !slices.Contains(dan.Topics, "dashboard") ||
		!slices.Contains(dan.Topics, "wiz") {
		t.Errorf("issue author dan topics = %v, want dashboard, wiz", dan.Topics)
	}
	if _, ok := byID["github:ghost"]; ok {
		t.Error("pull request returned by the issues endpoint should be skipped")
	}
	if kim := byID["codeowners:kim"]; kim.Name != "@kim" {
		t.Errorf("codeowners record = %+v, want @kim", kim)
	}
}
