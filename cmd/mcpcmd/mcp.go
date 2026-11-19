package mcpcmd

import (
	"context"
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
	"github.com/yz4230/mcpauth"
	"github.com/yz4230/okf-storage/internal/bundledir"
	"github.com/yz4230/okf-storage/internal/localbundle"
)

// passwordEnv names the environment variable holding the OAuth login
// password. It is read from the environment rather than a flag to keep it
// out of process listings.
const passwordEnv = "OKF_STORAGE_PASSWORD"

// tokenEnv names the environment variable holding a static bearer token, for
// clients that cannot go through OAuth, such as scripts.
const tokenEnv = "OKF_STORAGE_TOKEN"

// oauthStateFile is where registered OAuth clients and token hashes are kept,
// under the bundle root. Being hidden, it is out of reach of every tool.
const oauthStateFile = ".okf-oauth.json"

var flags struct {
	addr         string
	dir          string
	publicURL    string
	redirectURIs []string
	stdio        bool
}

// Cmd serves the MCP server over Streamable HTTP, or over stdin/stdout with
// --stdio.
var Cmd = &cobra.Command{
	Use:   "mcp",
	Short: "Serve the MCP server over HTTP or stdio",
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, err := bundledir.Expand(flags.dir)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}

		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if flags.stdio {
			return serveStdio(ctx, dir, &mcp.StdioTransport{})
		}
		return serve(ctx, flags.addr, dir, os.Getenv(tokenEnv), os.Getenv(passwordEnv), mcpauth.Config{
			PublicURL:           flags.publicURL,
			AllowedRedirectURIs: append(slices.Clone(mcpauth.DefaultRedirectURIs), flags.redirectURIs...),
		})
	},
}

func init() {
	Cmd.Flags().StringVar(&flags.addr, "addr", "localhost:8080", "Address to listen on")
	Cmd.Flags().StringVar(&flags.dir, "dir", bundledir.Default, "Knowledge bundle root directory")
	Cmd.Flags().StringVar(&flags.publicURL, "public-url", "", "Public base URL clients reach the server at, e.g. https://okf.example.com (required with "+passwordEnv+")")
	Cmd.Flags().StringSliceVar(&flags.redirectURIs, "redirect-uri", nil, "OAuth redirect URI to accept at client registration, besides ChatGPT's and Claude's (repeatable)")
	Cmd.Flags().BoolVar(&flags.stdio, "stdio", false, "Serve over stdin/stdout instead of HTTP (no authentication)")
	Cmd.MarkFlagsMutuallyExclusive("stdio", "addr")
	Cmd.MarkFlagsMutuallyExclusive("stdio", "public-url")
	Cmd.MarkFlagsMutuallyExclusive("stdio", "redirect-uri")
}

// serveStdio serves the MCP server for the bundle at dir over t until the
// client disconnects or ctx is cancelled. The client is the process that
// spawned this one, so there is no authentication; logs go to stderr, keeping
// stdout for the protocol.
func serveStdio(ctx context.Context, dir string, t mcp.Transport) error {
	b, err := localbundle.NewLocalBundle(dir)
	if err != nil {
		return err
	}
	defer b.Close()

	slog.Info("MCP server serving over stdio", "dir", dir)
	err = newServer(b).Run(ctx, t)
	if ctx.Err() != nil {
		return nil
	}
	return err
}

// newAuth returns the OAuth middleware configured by cfg and password, with
// its state kept in the bundle at dir, or nil when password is empty.
func newAuth(dir, password string, cfg mcpauth.Config) (*mcpauth.Auth, error) {
	if password == "" {
		if cfg.PublicURL != "" {
			return nil, errors.New("--public-url requires " + passwordEnv)
		}
		return nil, nil
	}
	if cfg.PublicURL == "" {
		return nil, errors.New(passwordEnv + " requires --public-url")
	}
	cfg.Password = password
	cfg.StateFile = filepath.Join(dir, oauthStateFile)
	return mcpauth.New(cfg)
}

func serve(ctx context.Context, addr, dir, token, password string, cfg mcpauth.Config) error {
	a, err := newAuth(dir, password, cfg)
	if err != nil {
		return err
	}
	if a == nil && token == "" {
		slog.Warn("serving without authentication; set " + tokenEnv + ", or " + passwordEnv + " and --public-url for OAuth")
	}
	b, err := localbundle.NewLocalBundle(dir)
	if err != nil {
		return err
	}
	defer b.Close()

	handler := newHandler(b, a, token)
	srv := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("MCP server listening", "addr", addr, "endpoint", "/mcp", "dir", dir, "oauth", a != nil, "token", token != "")
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

// newHandler serves the MCP server for b at /mcp, and a tar.gz download of
// the whole bundle at GET /dump. Both require "Authorization: Bearer <tok>",
// where tok is token (when non-empty) or an access token issued by a (when
// non-nil), which also serves the OAuth endpoints.
//
// Without either, cross-origin browser requests are rejected so web pages
// cannot drive an unauthenticated local server. With authentication that
// check is redundant, and it would block hosted clients such as ChatGPT that
// send an Origin header.
func newHandler(b bundle, a *mcpauth.Auth, token string) http.Handler {
	server := newServer(b)
	var h http.Handler = mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server },
		// Stateless is required to negotiate protocol 2026-07-28, which
		// clients such as ChatGPT use exclusively. No tool relies on a session.
		&mcp.StreamableHTTPOptions{Stateless: true, Logger: slog.Default()},
	)
	mux := http.NewServeMux()
	mux.Handle("GET /dump", dumpHandler(b))
	if a == nil && token == "" {
		mux.Handle("/mcp", http.NewCrossOriginProtection().Handler(h))
		return logRequests(mux)
	}
	mux.Handle("/mcp", h)
	return logRequests(requireAuth(mux, a, token))
}

// requireAuth lets through requests bearing token, and hands every other
// request to a, or rejects it when a is nil.
func requireAuth(next http.Handler, a *mcpauth.Auth, token string) http.Handler {
	fallback := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "invalid token", http.StatusUnauthorized)
	})
	if a != nil {
		fallback = a.Middleware(next).ServeHTTP
	}
	if token == "" {
		return fallback
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if ok && subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1 {
			next.ServeHTTP(w, r)
			return
		}
		fallback.ServeHTTP(w, r)
	})
}

// logRequests logs each request at debug level.
func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		slog.Debug("request", "method", r.Method, "path", r.URL.Path, "status", rec.status,
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
