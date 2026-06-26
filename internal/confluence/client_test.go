package confluence

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

// TestPages verifies page fields, labels, and authors decode.
func TestPages(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `{"size":1,"limit":100,"results":[{`+
			`"title":"Wiz scanning runbook",`+
			`"space":{"key":"SEC","name":"Security"},`+
			`"metadata":{"labels":{"results":[{"name":"wiz"}]}},`+
			`"history":{"createdBy":{"accountId":"a1","displayName":"Jane","email":"jane@x.com"}},`+
			`"version":{"by":{"accountId":"a2","displayName":"Bob","email":"bob@x.com"}}}]}`)
	}))
	t.Cleanup(srv.Close)

	pages, err := New(srv.URL, "me@x.com", "token").Pages(context.Background(), "type = page", 0)
	if err != nil {
		t.Fatalf("Pages: %v", err)
	}
	if len(pages) != 1 {
		t.Fatalf("pages = %d, want 1", len(pages))
	}
	p := pages[0]
	if p.Title != "Wiz scanning runbook" || p.Space.Key != "SEC" {
		t.Errorf("page = %+v", p)
	}
	if !slices.Contains(p.LabelNames(), "wiz") {
		t.Errorf("labels = %v, want wiz", p.LabelNames())
	}
	authors := p.Authors()
	if len(authors) != 2 || authors[0].Email != "jane@x.com" || authors[1].Email != "bob@x.com" {
		t.Errorf("authors = %+v, want jane and bob", authors)
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
