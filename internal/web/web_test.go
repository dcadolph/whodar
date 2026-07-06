package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dcadolph/whodar/internal/feedback"
	"github.com/dcadolph/whodar/internal/model"
	"github.com/dcadolph/whodar/internal/resolve"
)

// testHandler builds a handler whose Ask returns one canned person.
func testHandler(t *testing.T) http.Handler {
	t.Helper()
	ask := func(_ context.Context, _, _ string, _ int) (resolve.Answer, error) {
		return resolve.Answer{
			Summary: "talk to jane",
			People: []model.Match{{
				Person:  &model.Person{Name: "Jane Roe", Email: "jane@x.com", Title: "Engineer"},
				Score:   1,
				Reasons: []string{"retries (topic)"},
			}},
		}, nil
	}
	h, err := Handler(Config{Ask: ask, Version: "test"})
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	return h
}

// TestIndexPage verifies the root serves HTML with the version.
func TestIndexPage(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	testHandler(t).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "whodar") || !strings.Contains(body, "test") {
		t.Errorf("index page missing whodar or version:\n%s", body)
	}
}

// TestAskAPI verifies the ask endpoint returns the answer as JSON.
func TestAskAPI(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	testHandler(t).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/ask?q=retries", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var ans resolve.JSONAnswer
	if err := json.Unmarshal(rec.Body.Bytes(), &ans); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if ans.Summary != "talk to jane" {
		t.Errorf("summary = %q", ans.Summary)
	}
	if len(ans.People) != 1 || ans.People[0].Email != "jane@x.com" {
		t.Errorf("people = %+v, want jane@x.com", ans.People)
	}
}

// TestAskMissingQuery verifies a missing q is a 400.
func TestAskMissingQuery(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	testHandler(t).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/ask", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// TestNilAskPanics verifies the handler guards a nil Ask function.
func TestNilAskPanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Error("Handler with nil Ask did not panic")
		}
	}()
	_, _ = Handler(Config{})
}

// TestFeedbackAPI verifies the feedback endpoint records votes and rejects bad
// requests.
func TestFeedbackAPI(t *testing.T) {
	t.Parallel()
	var got feedback.Entry
	ask := func(_ context.Context, _, _ string, _ int) (resolve.Answer, error) {
		return resolve.Answer{}, nil
	}
	h, err := Handler(Config{
		Ask:      ask,
		Feedback: func(e feedback.Entry) error { got = e; return nil },
		Version:  "test",
	})
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}

	body := `{"query":"billing retries","person":"jane@x.com","vote":"helpful"}`
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/feedback", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if got.Query != "billing retries" || got.Person != "jane@x.com" || got.Vote != feedback.Helpful {
		t.Errorf("recorded entry = %+v", got)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/feedback", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET status = %d, want 405", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/feedback",
		strings.NewReader(`{"query":"","vote":"helpful"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid entry status = %d, want 400", rec.Code)
	}
}

// TestFeedbackAPIDisabled verifies the endpoint is absent without a callback.
func TestFeedbackAPIDisabled(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	testHandler(t).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/feedback", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 when feedback is disabled", rec.Code)
	}
}

// TestPersonAPI verifies the person endpoint returns a profile and a 404 for
// unknown identifiers.
func TestPersonAPI(t *testing.T) {
	t.Parallel()
	ask := func(_ context.Context, _, _ string, _ int) (resolve.Answer, error) {
		return resolve.Answer{}, nil
	}
	person := func(id string) (resolve.JSONProfile, bool) {
		if id != "jane@x.com" {
			return resolve.JSONProfile{}, false
		}
		return resolve.JSONProfile{ID: id, Name: "Jane Roe", Channels: []string{"payments"}}, true
	}
	h, err := Handler(Config{Ask: ask, Person: person, Version: "test"})
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/person?id=jane%40x.com", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var got resolve.JSONProfile
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Name != "Jane Roe" || len(got.Channels) != 1 {
		t.Errorf("profile = %+v", got)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/person?id=ghost", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown person status = %d, want 404", rec.Code)
	}
}
