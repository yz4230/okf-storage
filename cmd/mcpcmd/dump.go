package mcpcmd

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/yz4230/okf-storage/internal/bundle"
)

// dumpPath is the HTTP path that downloads the whole bundle as a tar.gz.
const dumpPath = "/dump"

// dumpHandler streams the bundle as a gzip-compressed tar archive. Since the
// archive is streamed, a failure midway can only be signalled by cutting the
// response short, which leaves the client with a truncated archive.
func dumpHandler(b *bundle.Bundle) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := "okf-bundle-" + time.Now().UTC().Format("20060102T150405Z") + ".tar.gz"
		w.Header().Set("Content-Type", "application/gzip")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
		if err := b.Dump(r.Context(), w); err != nil {
			slog.Error("dump failed", "err", err)
			panic(http.ErrAbortHandler)
		}
	})
}
