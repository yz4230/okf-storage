package mcpcmd

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
	"github.com/yz4230/okf-storage/internal/bundle"
)

var flags struct {
	addr string
	dir  string
}

// Cmd serves the MCP server over Streamable HTTP.
var Cmd = &cobra.Command{
	Use:   "mcp",
	Short: "Serve the MCP server over HTTP",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return serve(ctx, flags.addr, flags.dir)
	},
}

func init() {
	Cmd.Flags().StringVar(&flags.addr, "addr", "localhost:8080", "Address to listen on")
	Cmd.Flags().StringVar(&flags.dir, "dir", ".", "Knowledge bundle root directory")
}

func serve(ctx context.Context, addr, dir string) error {
	store, err := bundle.OpenDir(dir)
	if err != nil {
		return err
	}
	defer store.Close()
	b, err := bundle.OpenBundle(ctx, store, bundle.NewMemCatalog())
	if err != nil {
		return err
	}

	server := newServer(b)
	handler := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{Logger: slog.Default()},
	)

	mux := http.NewServeMux()
	mux.Handle("/mcp", http.NewCrossOriginProtection().Handler(handler))

	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("MCP server listening", "addr", addr, "endpoint", "/mcp", "dir", dir)
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
