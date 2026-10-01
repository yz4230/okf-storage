package localbundle

import (
	"cmp"
	"fmt"
	"path"
	"strings"
)

// cleanPath returns p in the canonical form fs.FS and os.Root accept. A leading
// slash makes p relative to the bundle root, so "/" is the root itself and
// "/a.md" is "a.md".
func cleanPath(p string) (string, error) {
	c := path.Clean(p)
	if rel, ok := strings.CutPrefix(c, "/"); ok {
		c = cmp.Or(rel, ".")
	}
	for elem := range strings.SplitSeq(c, "/") {
		if isHidden(elem) {
			return "", fmt.Errorf("%w: %s", ErrHiddenPath, p)
		}
	}
	return c, nil
}

// isHidden reports whether name is a dotfile. The "." and ".." path
// elements are not hidden names.
func isHidden(name string) bool { return name != "." && name != ".." && strings.HasPrefix(name, ".") }
