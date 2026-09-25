package mcpcmd

import (
	"context"
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
	"github.com/yz4230/okf-storage/internal/bundle"
)

// tokenEnv names the environment variable holding the bearer token. It is
// read from the environment rather than a flag to keep it out of process
// listings.
const tokenEnv = "OKF_STORAGE_TOKEN"

var flags struct {
	addr string
	dir  string
	path string
}

// Cmd serves the MCP server over Streamable HTTP.
var Cmd = &cobra.Command{
	Use:   "mcp",
	Short: "Serve the MCP server over HTTP",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return serve(ctx, flags.addr, flags.dir, flags.path, os.Getenv(tokenEnv))
	},
}

func init() {
	Cmd.Flags().StringVar(&flags.addr, "addr", "localhost:8080", "Address to listen on")
	Cmd.Flags().StringVar(&flags.dir, "dir", ".", "Knowledge bundle root directory")
	Cmd.Flags().StringVar(&flags.path, "path", "/mcp", "HTTP path of the MCP endpoint")
}

func serve(ctx context.Context, addr, dir, path, token string) error {
	store, err := bundle.OpenDir(dir)
	if err != nil {
		return err
	}
	defer store.Close()
	catalog := bundle.NewMemCatalog()
	if err := bundle.Reindex(ctx, store, catalog); err != nil {
		return err
	}
	b := bundle.NewBundle(store, catalog)

	if token == "" {
		slog.Warn("serving without authentication; set " + tokenEnv + " to require a bearer token")
	}
	handler := newHandler(newServer(b), path, token)
	srv := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("MCP server listening", "addr", addr, "endpoint", path, "dir", dir, "auth", token != "")
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	slog.Info("shutting down MCP server")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	if err := <-errCh; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// newHandler serves server at path. A non-empty token makes every request
// require "Authorization: Bearer <token>", or, for clients that cannot send
// headers, the token as a trailing path segment ("<path>/<token>"). A request
// carrying an Authorization header is judged by the header alone.
func newHandler(server *mcp.Server, path, token string) http.Handler {
	var h http.Handler = mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{Logger: slog.Default()},
	)
	mux := http.NewServeMux()
	if token != "" {
		h = requireToken(h, token)
		mux.Handle(strings.TrimSuffix(path, "/")+"/{token}", http.NewCrossOriginProtection().Handler(h))
	}
	mux.Handle(path, http.NewCrossOriginProtection().Handler(h))
	return mux
}

func requireToken(next http.Handler, token string) http.Handler {
	valid := func(got string) bool { return subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1 }
	verify := func(_ context.Context, got string, _ *http.Request) (*auth.TokenInfo, error) {
		if !valid(got) {
			return nil, auth.ErrInvalidToken
		}
		return &auth.TokenInfo{}, nil
	}
	bearer := auth.RequireBearerToken(verify, &auth.RequireBearerTokenOptions{AllowMissingExpiration: true})(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.PathValue("token")
		if r.Header.Get("Authorization") != "" || got == "" {
			bearer.ServeHTTP(w, r)
			return
		}
		if !valid(got) {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
