//go:build unix

package bundle

import "testing"

// A second DirStore stands in for a second process: flock locks per open
// file, so it conflicts even within one process.
func TestOpenDirLocksDirectory(t *testing.T) {
	dir := t.TempDir()
	r, err := OpenDir(dir)
	if err != nil {
		t.Fatalf("OpenDir() error = %v", err)
	}
	if other, err := OpenDir(dir); err == nil {
		other.Close()
		t.Fatalf("second OpenDir() error = nil, want the directory in use")
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	other, err := OpenDir(dir)
	if err != nil {
		t.Fatalf("OpenDir() after Close() error = %v", err)
	}
	other.Close()
}
