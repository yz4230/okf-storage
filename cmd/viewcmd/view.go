package viewcmd

import (
	"log/slog"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
	"github.com/spf13/cobra"
	"github.com/yz4230/okf-storage/internal/bundledir"
	"github.com/yz4230/okf-storage/internal/gateway"
	"github.com/yz4230/okf-storage/internal/localbundle"
)

var flags struct {
	addr string
	dir  string
	open bool
}

// Cmd serves a read-only web UI for browsing the bundle.
var Cmd = &cobra.Command{
	Use:   "view",
	Short: "Browse the knowledge bundle in a web browser (read-only)",
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, err := bundledir.Expand(flags.dir)
		if err != nil {
			return err
		}
		b, err := localbundle.NewLocalBundle(dir)
		if err != nil {
			return err
		}
		defer b.Close()

		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		e := echo.New()
		e.Logger = slog.Default()
		e.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
			LogStatus:   true,
			LogURI:      true,
			LogLatency:  true,
			LogMethod:   true,
			HandleError: true,
			LogValuesFunc: func(c *echo.Context, v middleware.RequestLoggerValues) error {
				slog.Debug("request", "method", v.Method, "uri", v.URI, "status", v.Status, "latency", v.Latency)
				return nil
			},
		}))
		gateway.RegisterHandlers(e, b)

		sc := echo.StartConfig{
			Address:    flags.addr,
			HideBanner: true,
			HidePort:   true,
			ListenerAddrFunc: func(addr net.Addr) {
				url := "http://" + browseHost(addr) + "/"
				slog.Info("serving bundle viewer", "url", url, "dir", dir)
				if flags.open {
					if err := openBrowser(url); err != nil {
						slog.Warn("failed to open a browser", "err", err)
					}
				}
			},
		}
		return sc.Start(ctx, e)
	},
}

func init() {
	Cmd.Flags().StringVar(&flags.addr, "addr", "localhost:8081", "Address to listen on")
	Cmd.Flags().StringVar(&flags.dir, "dir", bundledir.Default, "Knowledge bundle root directory")
	Cmd.Flags().BoolVar(&flags.open, "open", false, "Open the viewer in the default web browser")
}

// browseHost returns a host:port for a browser to reach addr, using
// localhost for an unspecified (wildcard) listen address.
func browseHost(addr net.Addr) string {
	tcp, ok := addr.(*net.TCPAddr)
	if !ok || tcp.IP.IsUnspecified() {
		_, port, _ := net.SplitHostPort(addr.String())
		return net.JoinHostPort("localhost", port)
	}
	return addr.String()
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}
