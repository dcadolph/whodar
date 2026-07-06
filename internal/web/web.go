// Package web serves the whodar web UI: a search page and a JSON ask API over
// the same engine the CLI uses.
package web

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dcadolph/whodar/internal/feedback"
	"github.com/dcadolph/whodar/internal/resolve"
)

// assets holds the embedded templates and static files.
//
//go:embed templates/*.html static/*
var assets embed.FS

// AskFunc resolves a query in the chosen mode and returns the answer.
type AskFunc func(ctx context.Context, query, mode string, limit int) (resolve.Answer, error)

// FeedbackFunc records a user's vote on one result.
type FeedbackFunc func(feedback.Entry) error

// PersonFunc returns the full profile for a person identifier, or false when
// the person is unknown.
type PersonFunc func(id string) (resolve.JSONProfile, bool)

// Config configures the web handler.
type Config struct {
	// Ask resolves queries; required.
	Ask AskFunc
	// Feedback records votes on results; nil disables the feedback API.
	Feedback FeedbackFunc
	// Person returns full person profiles; nil disables the person API.
	Person PersonFunc
	// Version is shown in the page footer.
	Version string
}

// Handler returns the whodar web handler: an index page, embedded assets, and a
// JSON ask API. It panics if cfg.Ask is nil.
func Handler(cfg Config) (http.Handler, error) {
	if cfg.Ask == nil {
		panic("web: Handler requires an Ask function")
	}
	tmpl, err := template.ParseFS(assets, "templates/index.html")
	if err != nil {
		return nil, fmt.Errorf("web: parse templates: %w", err)
	}
	static, err := fs.Sub(assets, "static")
	if err != nil {
		return nil, fmt.Errorf("web: static assets: %w", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(static))))
	mux.HandleFunc("/api/ask", askHandler(cfg.Ask))
	if cfg.Feedback != nil {
		mux.HandleFunc("/api/feedback", feedbackHandler(cfg.Feedback))
	}
	if cfg.Person != nil {
		mux.HandleFunc("/api/person", personHandler(cfg.Person))
	}
	mux.HandleFunc("/", indexHandler(tmpl, cfg.Version))
	return mux, nil
}

// indexHandler serves the search page at the root path.
func indexHandler(tmpl *template.Template, version string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := tmpl.Execute(w, map[string]string{"Version": version}); err != nil {
			http.Error(w, "template error", http.StatusInternalServerError)
		}
	}
}

// askHandler answers queries as JSON. It reads q, mode, and limit from the query
// string and returns the same shape the CLI emits.
func askHandler(ask AskFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		query := strings.TrimSpace(r.URL.Query().Get("q"))
		if query == "" {
			writeError(w, http.StatusBadRequest, "missing q")
			return
		}
		limit := 5
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				limit = n
			}
		}

		ans, err := ask(r.Context(), query, r.URL.Query().Get("mode"), limit)
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		_ = json.NewEncoder(w).Encode(ans.View(query))
	}
}

// personHandler returns the full profile for the person named by the id query
// parameter.
func personHandler(person PersonFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id := strings.TrimSpace(r.URL.Query().Get("id"))
		if id == "" {
			writeError(w, http.StatusBadRequest, "missing id")
			return
		}
		profile, ok := person(id)
		if !ok {
			writeError(w, http.StatusNotFound, "unknown person")
			return
		}
		_ = json.NewEncoder(w).Encode(profile)
	}
}

// feedbackHandler records a vote on one result. It accepts a POST with a JSON
// body naming the query, the person or channel, and the vote direction.
func feedbackHandler(record FeedbackFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "POST only")
			return
		}
		var body struct {
			// Query is the question the vote is about.
			Query string `json:"query"`
			// Person is the voted person's identifier.
			Person string `json:"person"`
			// Channel is the voted channel's name.
			Channel string `json:"channel"`
			// Vote is "helpful" or "not-helpful".
			Vote string `json:"vote"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		entry := feedback.Entry{
			Query:   strings.TrimSpace(body.Query),
			Person:  strings.TrimSpace(body.Person),
			Channel: strings.TrimSpace(body.Channel),
			Time:    time.Now(),
		}
		switch body.Vote {
		case "helpful":
			entry.Vote = feedback.Helpful
		case "not-helpful":
			entry.Vote = feedback.NotHelpful
		}
		if !entry.Valid() {
			writeError(w, http.StatusBadRequest, feedback.ErrBadEntry.Error())
			return
		}
		if err := record(entry); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "recorded"})
	}
}

// writeError writes a JSON error response with the given status.
func writeError(w http.ResponseWriter, status int, msg string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
