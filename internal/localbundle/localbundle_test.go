package localbundle

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/gofrs/flock"
)

func TestHiddenPathsRejected(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "x.md"), []byte("# X\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := NewLocalBundle(dir)
	if err != nil {
		t.Fatalf("NewLocalBundle() error = %v", err)
	}
	t.Cleanup(func() { b.Close() })
	if _, err := b.Write("a.md", "# A\n"); err != nil {
		t.Fatalf("Write(a.md) error = %v", err)
	}

	for _, p := range []string{".git/x.md", ".hidden.md", "a/.x.md", "a/../.git/x.md", "./.git", "/.git/x.md", "/a/.x.md"} {
		ops := map[string]error{
			"List":      func() error { _, err := b.List(p); return err }(),
			"Read":      func() error { _, err := b.Read(p); return err }(),
			"Write":     func() error { _, err := b.Write(p, "# X\n"); return err }(),
			"Edit":      b.Edit(p, "X", "Y", false),
			"Delete":    b.Delete(p),
			"Move from": b.Move(p, "moved.md"),
			"Move to":   b.Move("a.md", p),
		}
		for op, err := range ops {
			if !errors.Is(err, ErrHiddenPath) {
				t.Errorf("%s(%q) error = %v, want ErrHiddenPath", op, p, err)
			}
		}
	}

	if got, err := b.Read("a.md"); err != nil || got != "# A\n" {
		t.Errorf("Read(a.md) = %q, %v; want it untouched", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git", "x.md")); err != nil {
		t.Errorf(".git/x.md: %v, want it untouched", err)
	}
	if _, err := b.List("."); err != nil {
		t.Errorf("List(.) error = %v", err)
	}
}

func TestListTrailingSlash(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "a.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := NewLocalBundle(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	for _, p := range []string{"sub", "sub/", "sub//", "./sub/", "sub/.", "/sub", "/sub/"} {
		got, err := b.List(p)
		if err != nil {
			t.Errorf("List(%q) error = %v", p, err)
			continue
		}
		if !slices.Equal(got, []string{"a.md"}) {
			t.Errorf("List(%q) = %v, want [a.md]", p, got)
		}
	}
}

func TestRootedPaths(t *testing.T) {
	dir := t.TempDir()
	b, err := NewLocalBundle(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	if _, err := b.Write("/sub/a.md", "# A\n"); err != nil {
		t.Fatalf("Write(/sub/a.md) error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "sub", "a.md")); err != nil {
		t.Fatalf("sub/a.md: %v, want it written under the bundle root", err)
	}
	for _, p := range []string{"/", "//"} {
		if got, err := b.List(p); err != nil || !slices.Equal(got, []string{"sub/"}) {
			t.Errorf("List(%q) = %v, %v; want [sub/]", p, got, err)
		}
	}
	for _, p := range []string{"/sub/a.md", "/../sub/a.md", "sub/a.md"} {
		if got, err := b.Read(p); err != nil || got != "# A\n" {
			t.Errorf("Read(%q) = %q, %v; want # A", p, got, err)
		}
	}
	if err := b.Edit("/sub/a.md", "A", "B", false); err != nil {
		t.Errorf("Edit(/sub/a.md) error = %v", err)
	}
	if err := b.Move("/sub/a.md", "/b.md"); err != nil {
		t.Fatalf("Move(/sub/a.md, /b.md) error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "sub")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("sub: %v, want the emptied directory removed", err)
	}
	if err := b.Delete("/b.md"); err != nil {
		t.Errorf("Delete(/b.md) error = %v", err)
	}
	if _, err := b.Read("../b.md"); err == nil {
		t.Error("Read(../b.md) error = nil, want relative paths still unable to escape the root")
	}
}

func TestSearchSkipsInvalidDocuments(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "broken.md"), []byte("---\ntype: [\n---\nneedle\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := NewLocalBundle(dir)
	if err != nil {
		t.Fatalf("NewLocalBundle() error = %v", err)
	}
	t.Cleanup(func() { b.Close() })
	if _, err := b.Write("a.md", "---\ntype: Concept\n---\nneedle\n"); err != nil {
		t.Fatalf("Write(a.md) error = %v", err)
	}

	if got, err := b.SearchFrontmatter(map[string]any{"type": "Concept"}); err != nil || !slices.Equal(got, []string{"a.md"}) {
		t.Errorf("SearchFrontmatter() = %q, %v; want [a.md]", got, err)
	}
	if got, err := b.SearchContent("needle"); err != nil || !slices.Equal(got, []string{"a.md"}) {
		t.Errorf("SearchContent() = %q, %v; want [a.md]", got, err)
	}
}

func TestBundleLock(t *testing.T) {
	dir := t.TempDir()
	b, err := NewLocalBundle(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if _, err := b.Write("a.md", "# A\n"); err != nil {
		t.Fatal(err)
	}
	other := flock.New(filepath.Join(dir, lockName))

	// waits returns nil if op finished, or a channel for its result if it blocks.
	waits := func(op func() error) chan error {
		done := make(chan error, 1)
		go func() { done <- op() }()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("op error = %v", err)
			}
			return nil
		case <-time.After(50 * time.Millisecond):
			return done
		}
	}
	read := func() error { _, err := b.Read("a.md"); return err }
	write := func() error { return b.Edit("a.md", "# ", "## ", false) }

	if err := other.RLock(); err != nil {
		t.Fatal(err)
	}
	if waits(read) != nil {
		t.Error("Read() waited for a shared lock")
	}
	done := waits(write)
	if done == nil {
		t.Fatal("Edit() did not wait for a shared lock")
	}
	other.Unlock()
	if err := <-done; err != nil {
		t.Fatalf("Edit() error = %v", err)
	}

	if err := other.Lock(); err != nil {
		t.Fatal(err)
	}
	done = waits(read)
	if done == nil {
		t.Fatal("Read() did not wait for an exclusive lock")
	}
	other.Unlock()
	if err := <-done; err != nil {
		t.Fatalf("Read() error = %v", err)
	}

	if got, err := b.Read("a.md"); err != nil || got != "## A\n" {
		t.Errorf("Read(a.md) = %q, %v; want ## A", got, err)
	}
	if names, err := b.List("."); err != nil || !slices.Equal(names, []string{"a.md"}) {
		t.Errorf("List(.) = %v, %v; want the lock file left out", names, err)
	}
}

func TestWriteKeepsPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows ignores most permission bits")
	}
	dir := t.TempDir()
	b, err := NewLocalBundle(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	if _, err := b.Write("a.md", "# A\n"); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "a.md")
	if fi, err := os.Stat(p); err != nil || fi.Mode().Perm() != 0o644 {
		t.Errorf("new file mode = %v, %v; want 0644", fi.Mode().Perm(), err)
	}
	if err := os.Chmod(p, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := b.Edit("a.md", "A", "B", false); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(p); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("edited file mode = %v, %v; want 0600 kept", fi.Mode().Perm(), err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 2 {
		t.Errorf("bundle has %v, want a.md and the lock file only", entries)
	}
}

func TestConcurrentOperations(t *testing.T) {
	b, err := NewLocalBundle(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	var wg sync.WaitGroup
	for i := range 4 {
		wg.Go(func() {
			p := fmt.Sprintf("sub/deep/%d.md", i)
			for range 50 {
				if _, err := b.Write(p, "# X\n"); err != nil {
					t.Errorf("Write(%s) error = %v", p, err)
					return
				}
				if err := b.Delete(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
					t.Errorf("Delete(%s) error = %v", p, err)
					return
				}
			}
		})
	}
	wg.Go(func() {
		for range 50 {
			b.Move("sub", "moved")
			b.Move("moved", "sub")
		}
	})
	for range 2 {
		wg.Go(func() {
			for range 50 {
				if _, err := b.SearchContent("X"); err != nil {
					t.Errorf("SearchContent() error = %v", err)
					return
				}
			}
		})
	}
	wg.Wait()
}
