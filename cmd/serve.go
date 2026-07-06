package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/dcadolph/whodar/internal/feedback"
	"github.com/dcadolph/whodar/internal/index"
	"github.com/dcadolph/whodar/internal/model"
	"github.com/dcadolph/whodar/internal/resolve"
	"github.com/dcadolph/whodar/internal/web"
)

// shutdownTimeout bounds how long serve waits for in-flight requests to finish.
const shutdownTimeout = 5 * time.Second

// webConfig carries the resolver settings shared by serve and demo.
type webConfig struct {
	// addr is the listen address.
	addr string
	// mode is the default resolver mode.
	mode string
	// model is the Ollama chat model for llm mode.
	model string
	// embedModel is the Ollama embed model.
	embedModel string
	// ollamaURL is the Ollama base URL.
	ollamaURL string
}

// newServeCmd builds the serve command, which runs the web UI on localhost and
// shuts down cleanly on interrupt.
func newServeCmd(opts *options) *cobra.Command {
	var cfg webConfig
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve the whodar web UI on localhost",
		Long: `Serve the local web UI over the same engine as ask. Binds to localhost by
default, so nothing leaves the machine. Queries are shareable links
(/?q=who+owns+billing runs on load) and every result has feedback buttons.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ix, err := index.Load(opts.indexPath())
			if err != nil {
				return fmt.Errorf("%w: run `whodar index` first: %w", ErrNoIndex, err)
			}
			store := applyFeedback(ix, opts, cmd.ErrOrStderr())
			return serveWeb(cmd, opts, ix, store, cfg)
		},
	}
	addWebFlags(cmd, &cfg, "127.0.0.1:8765")
	return cmd
}

// addWebFlags registers the shared web-serving flags on cmd.
func addWebFlags(cmd *cobra.Command, cfg *webConfig, defaultAddr string) {
	f := cmd.Flags()
	f.StringVar(&cfg.addr, "addr", defaultAddr, "Address to listen on.")
	f.StringVar(&cfg.mode, "mode", "keyword", "Default resolver: keyword, semantic, or llm.")
	f.StringVar(&cfg.model, "model", "", "Ollama chat model for llm mode.")
	f.StringVar(&cfg.embedModel, "embed-model", "", "Ollama embed model for semantic/llm mode.")
	f.StringVar(&cfg.ollamaURL, "ollama-url", "http://localhost:11434", "Ollama base URL.")
}

// serveWeb runs the web UI over ix until interrupted. A nil store disables
// the feedback API.
func serveWeb(cmd *cobra.Command, opts *options, ix *index.Index, store *feedback.Store, cfg webConfig) error {
	ask := func(ctx context.Context, query, reqMode string, limit int) (resolve.Answer, error) {
		if reqMode == "" {
			reqMode = cfg.mode
		}
		res, err := pickResolver(ix, opts, reqMode, cfg.model, cfg.embedModel, cfg.ollamaURL)
		if err != nil {
			return resolve.Answer{}, err
		}
		return res.Resolve(ctx, query, limit)
	}
	var vote web.FeedbackFunc
	if store != nil {
		vote = func(e feedback.Entry) error {
			if err := store.Add(e); err != nil {
				return err
			}
			ix.SetFeedback(store.All())
			return nil
		}
	}
	person := func(id string) (resolve.JSONProfile, bool) {
		profile, ok := ix.Profile(model.ID(id))
		if !ok {
			return resolve.JSONProfile{}, false
		}
		return resolve.ProfileView(profile), true
	}

	handler, err := web.Handler(web.Config{Ask: ask, Feedback: vote, Person: person, Version: version})
	if err != nil {
		return err
	}
	srv := &http.Server{Addr: cfg.addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second}

	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	fmt.Fprintf(cmd.ErrOrStderr(), "whodar serving on http://%s (Ctrl-C to stop)\n", cfg.addr)

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		fmt.Fprintln(cmd.ErrOrStderr(), "whodar: shutting down")
		shutCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		return srv.Shutdown(shutCtx)
	}
}
