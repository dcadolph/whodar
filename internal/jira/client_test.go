package jira

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// TestSearch verifies issue fields decode, including the assignee email.
func TestSearch(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `{"total":1,"startAt":0,"issues":[{"key":"SEC-1","fields":{`+
			`"summary":"Wiz scan flaky",`+
			`"assignee":{"accountId":"a1","displayName":"Jane Roe","emailAddress":"jane@x.com"},`+
			`"components":[{"name":"scanning"}],"labels":["wiz"],`+
			`"project":{"key":"SEC","name":"Security"}}}]}`)
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL, "me@x.com", "token")
	issues, err := c.Search(context.Background(), "project = SEC", 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("issues = %d, want 1", len(issues))
	}
	got := issues[0]
	if got.Key != "SEC-1" || got.Fields.Assignee == nil ||
		got.Fields.Assignee.EmailAddress != "jane@x.com" {
		t.Errorf("issue = %+v", got)
	}
	if len(got.Fields.Components) != 1 || got.Fields.Components[0].Name != "scanning" {
		t.Errorf("components = %v", got.Fields.Components)
	}
}

// TestSearchPagination verifies issues accumulate across pages.
func TestSearchPagination(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		i := calls
		calls++
		mu.Unlock()
		if i == 0 {
			io.WriteString(w, `{"total":2,"startAt":0,"issues":[{"key":"A-1","fields":{}}]}`)
			return
		}
		io.WriteString(w, `{"total":2,"startAt":1,"issues":[{"key":"A-2","fields":{}}]}`)
	}))
	t.Cleanup(srv.Close)

	issues, err := New(srv.URL, "me@x.com", "token").Search(context.Background(), "order by updated", 0)
	if err != nil || len(issues) != 2 {
		t.Fatalf("issues = %d, err %v; want 2", len(issues), err)
	}
}

// TestRetryAfter verifies a 429 is retried and then succeeds.
func TestRetryAfter(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		i := calls
		calls++
		mu.Unlock()
		if i == 0 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			io.WriteString(w, `{}`)
			return
		}
		io.WriteString(w, `{"total":0,"issues":[]}`)
	}))
	t.Cleanup(srv.Close)

	if _, err := New(srv.URL, "me@x.com", "token").Search(context.Background(), "x", 0); err != nil {
		t.Fatalf("Search after 429: %v", err)
	}
}

// TestNewEmptyPanics verifies the constructor guards empty arguments.
func TestNewEmptyPanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Error("New with empty args did not panic")
		}
	}()
	New("", "e", "t")
}
