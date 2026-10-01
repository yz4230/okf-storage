//go:build !windows

package localbundle

import (
	"os"

	"github.com/google/renameio/v2"
)

func writeFile(root *os.Root, path string, data []byte) error {
	return renameio.WriteFile(path, data, 0o644, renameio.WithRoot(root))
}
