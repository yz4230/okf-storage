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
	"github.com/yz4230/okf-storage/internal/localbundle"
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
	b, err := localbundle.NewLocalBundle(dir)
	if err != nil {
		return err
	}
	defer b.Close()

	if token == "" {
		slog.Warn("serving without authentication; set " + tokenEnv + " to require a bearer token")
	}
	handler := newHandler(b, path, token)
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

// newHandler serves the MCP server for b at path, and a tar.gz download of
// the whole bundle at GET /dump. A non-empty token makes every request
// require "Authorization: Bearer <token>", or, for clients that cannot send
// headers, the token as a trailing path segment ("<path>/<token>"). A request
// carrying an Authorization header is judged by the header alone.
//
// Without a token, cross-origin browser requests are rejected so web pages
// cannot drive an unauthenticated local server. With a token that check is
// redundant, and it would block hosted clients such as ChatGPT that send an
// Origin header.
func newHandler(b bundle, path, token string) http.Handler {
	server := newServer(b)
	var h http.Handler = mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server },
		// Stateless is required to negotiate protocol 2026-07-28, which
		// clients such as ChatGPT use exclusively. No tool relies on a session.
		&mcp.StreamableHTTPOptions{Stateless: true, Logger: slog.Default()},
	)
	dump := dumpHandler(b)
	mux := http.NewServeMux()
	if token == "" {
		mux.Handle(path, http.NewCrossOriginProtection().Handler(h))
		mux.Handle("GET "+dumpPath, dump)
		return logRequests(mux, "")
	}
	h = requireToken(h, token)
	mux.Handle(path, h)
	mux.Handle(strings.TrimSuffix(path, "/")+"/{token}", h)
	dump = requireToken(dump, token)
	mux.Handle("GET "+dumpPath, dump)
	mux.Handle("GET "+dumpPath+"/{token}", dump)
	return logRequests(mux, token)
}

// logRequests logs each request at debug level, redacting token from the path.
func logRequests(next http.Handler, token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if token != "" {
			path = strings.ReplaceAll(path, token, "<token>")
		}
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		slog.Debug("request", "method", r.Method, "path", path, "status", rec.status,
			"origin", r.Header.Get("Origin"), "user_agent", r.UserAgent())
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Unwrap lets http.ResponseController reach Flush for streaming responses.
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

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
