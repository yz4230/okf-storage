// Package bundledir resolves the --dir flag the commands share.
package bundledir

import (
	"os"
	"path/filepath"
	"strings"
)

// Default is the bundle directory used when --dir is not given.
const Default = "~/.okf-storage/bundle"

// Expand replaces a leading "~" path element with the user's home directory.
func Expand(path string) (string, error) {
	rest, ok := strings.CutPrefix(path, "~")
	if !ok || (rest != "" && !os.IsPathSeparator(rest[0])) {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, rest), nil
}
