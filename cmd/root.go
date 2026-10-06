package cmd

import (
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"

	"github.com/lmittmann/tint"
	"github.com/spf13/cobra"
	"github.com/yz4230/okf-storage/cmd/mcpcmd"
)

var rootPstFlags struct {
	verbose bool
}

var rootCmd = &cobra.Command{
	Use:     filepath.Base(os.Args[0]),
	Version: version(),
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		options := &tint.Options{AddSource: true, TimeFormat: time.TimeOnly}
		if rootPstFlags.verbose {
			options.Level = slog.LevelDebug
		}
		logger := slog.New(tint.NewTextHandler(os.Stderr, options))
		slog.SetDefault(logger)
	},
}

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(&rootPstFlags.verbose, "verbose", "v", false, "Enable verbose output")
	rootCmd.AddCommand(mcpcmd.Cmd)
}

// version returns the main module version that the Go toolchain records:
// the tag when built at a clean vX.Y.Z commit or with `go install ...@vX.Y.Z`,
// otherwise a pseudo-version (+dirty for a modified tree), or (devel) for go run.
func version() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		return info.Main.Version
	}
	return "(devel)"
}
