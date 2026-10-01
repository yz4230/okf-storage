package localbundle

import "os"

// renameio does not support Windows, so writes there are not atomic.
func writeFile(root *os.Root, path string, data []byte) error {
	return root.WriteFile(path, data, 0o644)
}
