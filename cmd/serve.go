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

	"github.com/dcadolph/whodar/internal/index"
	"github.com/dcadolph/whodar/internal/resolve"
	"github.com/dcadolph/whodar/internal/web"
)

// shutdownTimeout bounds how long serve waits for in-flight requests to finish.
const shutdownTimeout = 5 * time.Second

// newServeCmd builds the serve command, which runs the web UI on localhost and
// shuts down cleanly on interrupt.
func newServeCmd(opts *options) *cobra.Command {
	var (
		addr      string
		mode      string
		model     string
		ollamaURL string
	)
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve the whodar web UI on localhost",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ix, err := index.Load(opts.indexPath())
			if err != nil {
				return fmt.Errorf("%w: run `whodar index` first: %w", ErrNoIndex, err)
			}
			ask := func(ctx context.Context, query, reqMode string, limit int) (resolve.Answer, error) {
				if reqMode == "" {
					reqMode = mode
				}
				res, err := pickResolver(ix, opts, reqMode, model, ollamaURL)
				if err != nil {
					return resolve.Answer{}, err
				}
				return res.Resolve(ctx, query, limit)
			}

			handler, err := web.Handler(web.Config{Ask: ask, Version: version})
			if err != nil {
				return err
			}
			srv := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			errCh := make(chan error, 1)
			go func() { errCh <- srv.ListenAndServe() }()
			fmt.Fprintf(cmd.ErrOrStderr(), "whodar serving on http://%s (Ctrl-C to stop)\n", addr)

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
		},
	}
	f := cmd.Flags()
	f.StringVar(&addr, "addr", "127.0.0.1:8765", "Address to listen on.")
	f.StringVar(&mode, "mode", "keyword", "Default resolver: keyword or llm.")
	f.StringVar(&model, "model", "", "Ollama model for llm mode.")
	f.StringVar(&ollamaURL, "ollama-url", "http://localhost:11434", "Ollama base URL for llm mode.")
	return cmd
}
