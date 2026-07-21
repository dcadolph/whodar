// Package web serves the whodar web UI: a search page and a JSON ask API over
// the same engine the CLI uses.
package web

import (
	"context"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/dcadolph/whodar/internal/feedback"
	"github.com/dcadolph/whodar/internal/llm"
	"github.com/dcadolph/whodar/internal/resolve"
)

// assets holds the embedded templates and static files.
//
//go:embed templates/*.html static/*
var assets embed.FS

// AskFunc resolves a query in the chosen mode and provider and returns the
// answer. Empty mode and provider mean the server defaults.
type AskFunc func(ctx context.Context, query, mode, provider string, limit int) (resolve.Answer, error)

// FeedbackFunc records a user's vote on one result.
type FeedbackFunc func(feedback.Entry) error

// PersonFunc returns the full profile for a person identifier, or false when
// the person is unknown.
type PersonFunc func(id string) (resolve.JSONProfile, bool)

// ModeInfo tells the UI whether an answer mode or provider can answer right
// now and what it uses or is missing.
type ModeInfo struct {
	// Ready reports whether the mode can answer right now.
	Ready bool `json:"ready"`
	// Hint says what the mode uses, or what to do to make it ready.
	Hint string `json:"hint,omitempty"`
}

// ModesReport is the readiness picture for the UI: the answer modes, the AI
// providers to pick from, and the server's default provider.
type ModesReport struct {
	// Modes is readiness per answer mode: keyword, semantic, llm.
	Modes map[string]ModeInfo `json:"modes"`
	// Providers is readiness per AI provider: ollama, anthropic, openai,
	// gemini.
	Providers map[string]ModeInfo `json:"providers,omitempty"`
	// Provider is the server's default AI provider.
	Provider string `json:"provider,omitempty"`
}

// ModesFunc reports mode and provider readiness.
type ModesFunc func(ctx context.Context) ModesReport

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
	// AuthToken, when set, requires the token on every request: a bearer
	// header, a token query parameter, or the cookie a prior visit set.
	AuthToken string
	// Directory is the browsable inventory served at /api/directory; nil
	// disables the directory API.
	Directory *resolve.Directory
	// Modes reports answer-mode readiness at /api/modes; nil disables it.
	Modes ModesFunc
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
	if cfg.Directory != nil {
		mux.HandleFunc("/api/directory", directoryHandler(cfg.Directory))
	}
	if cfg.Modes != nil {
		mux.HandleFunc("/api/modes", modesHandler(cfg.Modes))
	}
	mux.HandleFunc("/", indexHandler(tmpl, cfg.Version))

	h := securityHeaders(mux)
	if cfg.AuthToken != "" {
		h = requireToken(cfg.AuthToken, h)
	}
	return h, nil
}

// securityHeaders sets response headers that harden every page and API reply.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

// authCookie names the session cookie set after a token is presented.
const authCookie = "whodar_token"

// requireToken gates every request behind the shared token. A token query
// parameter also sets a strict same-site cookie so a shared link keeps
// working after the first visit.
func requireToken(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !tokenOK(token, r) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="whodar"`)
			writeError(w, http.StatusUnauthorized, "missing or wrong token")
			return
		}
		if r.URL.Query().Get("token") != "" {
			http.SetCookie(w, &http.Cookie{
				Name: authCookie, Value: token, Path: "/",
				HttpOnly: true, SameSite: http.SameSiteStrictMode,
			})
		}
		next.ServeHTTP(w, r)
	})
}

// tokenOK reports whether r carries the token in a bearer header, a query
// parameter, or the session cookie. Comparisons are constant-time.
func tokenOK(token string, r *http.Request) bool {
	const bearer = "Bearer "
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, bearer) {
		if subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(auth, bearer)), []byte(token)) == 1 {
			return true
		}
	}
	if q := r.URL.Query().Get("token"); q != "" {
		if subtle.ConstantTimeCompare([]byte(q), []byte(token)) == 1 {
			return true
		}
	}
	if c, err := r.Cookie(authCookie); err == nil {
		if subtle.ConstantTimeCompare([]byte(c.Value), []byte(token)) == 1 {
			return true
		}
	}
	return false
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
		const maxLimit = 50
		limit := 5
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= maxLimit {
				limit = n
			}
		}

		ans, err := ask(r.Context(), query, r.URL.Query().Get("mode"), r.URL.Query().Get("provider"), limit)
		if err != nil {
			if errors.Is(err, ErrBadRequest) {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			if errors.Is(err, llm.ErrModel) {
				writeError(w, http.StatusBadGateway,
					"The local model is not reachable. LLM and semantic modes need Ollama "+
						"running on this machine: install it from ollama.com, run "+
						"`ollama pull llama3.1`, and ask again. Keyword mode always works.")
				return
			}
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

// modesHandler reports each answer mode's readiness so the UI can guide the
// user before they ask.
func modesHandler(modes ModesFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(modes(r.Context()))
	}
}

// directoryHandler serves the precomputed directory of people, channels,
// teams, and topics for the browse views.
func directoryHandler(dir *resolve.Directory) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(dir)
	}
}

// feedbackHandler records a vote on one result. It accepts a POST with a JSON
// body naming the query, the person or channel, and the vote direction. A
// cross-origin POST is rejected so another site the operator has open cannot
// cast votes through their browser.
func feedbackHandler(record FeedbackFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "POST only")
			return
		}
		if o := r.Header.Get("Origin"); o != "" && !sameOrigin(o, r.Host) {
			writeError(w, http.StatusForbidden, "cross-origin feedback rejected")
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
			// Comment is an optional note explaining the vote.
			Comment string `json:"comment"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		entry := feedback.Entry{
			Query:   strings.TrimSpace(body.Query),
			Person:  strings.TrimSpace(body.Person),
			Channel: strings.TrimSpace(body.Channel),
			Comment: strings.TrimSpace(body.Comment),
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

// sameOrigin reports whether the Origin header names this server.
func sameOrigin(origin, host string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return u.Host == host
}

// writeError writes a JSON error response with the given status.
func writeError(w http.ResponseWriter, status int, msg string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
