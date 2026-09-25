package bundle

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestDirStore(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	r, err := OpenDir(dir)
	if err != nil {
		t.Fatalf("OpenDir() error = %v", err)
	}
	defer r.Close()

	if _, err := r.Write(ctx, "metrics/revenue.md", "---\ntype: Metric\n---\n", ""); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if _, err := r.Write(ctx, "index.md", "# Index\n", ""); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	os.WriteFile(filepath.Join(dir, "notes.txt"), nil, 0o644)
	os.MkdirAll(filepath.Join(dir, ".git"), 0o755)
	os.WriteFile(filepath.Join(dir, ".git", "x.md"), nil, 0o644)

	data, _, err := r.Read(ctx, "metrics/revenue.md")
	if err != nil || data != "---\ntype: Metric\n---\n" {
		t.Errorf("Read() = %q, %v", data, err)
	}

	entries, err := r.List(ctx, ".")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if want := []Entry{{"index.md", false}, {"metrics", true}}; !slices.Equal(entries, want) {
		t.Errorf("List(.) = %v, want %v", entries, want)
	}
	entries, err = r.List(ctx, "metrics")
	if want := []Entry{{"metrics/revenue.md", false}}; err != nil || !slices.Equal(entries, want) {
		t.Errorf("List(metrics) = %v, %v, want %v", entries, err, want)
	}
	if _, err := r.List(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("List(missing) error = %v, want %v", err, ErrNotFound)
	}

	page, err := r.Tree(ctx, ".", -1, PageRequest{})
	if err != nil {
		t.Fatalf("Tree() error = %v", err)
	}
	if want := []string{"index.md", "metrics/revenue.md"}; !slices.Equal(page.Paths, want) || page.Next != "" {
		t.Errorf("Tree(.) = %+v, want %v", page, want)
	}
	page, err = r.Tree(ctx, ".", 1, PageRequest{})
	if want := []string{"index.md"}; err != nil || !slices.Equal(page.Paths, want) {
		t.Errorf("Tree(., 1) = %v, %v, want %v", page.Paths, err, want)
	}
	page, err = r.Tree(ctx, "metrics", 1, PageRequest{})
	if want := []string{"metrics/revenue.md"}; err != nil || !slices.Equal(page.Paths, want) {
		t.Errorf("Tree(metrics) = %v, %v, want %v", page.Paths, err, want)
	}
	if _, err := r.Tree(ctx, "missing", -1, PageRequest{}); !errors.Is(err, ErrNotFound) {
		t.Errorf("Tree(missing) error = %v, want %v", err, ErrNotFound)
	}

	if err := r.Delete(ctx, "index.md"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, _, err := r.Read(ctx, "index.md"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Read() after Delete error = %v, want %v", err, ErrNotFound)
	}
	if err := r.Delete(ctx, "index.md"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete() missing error = %v, want %v", err, ErrNotFound)
	}
}

func TestDirStoreTreePagination(t *testing.T) {
	ctx := t.Context()
	r, err := OpenDir(t.TempDir())
	if err != nil {
		t.Fatalf("OpenDir() error = %v", err)
	}
	defer r.Close()
	// WalkDir visits a/b.md before a.md; byte order puts a.md first.
	for _, p := range []string{"a/b.md", "a.md", "b.md", "a/c/d.md"} {
		if _, err := r.Write(ctx, p, "", ""); err != nil {
			t.Fatalf("Write(%q) error = %v", p, err)
		}
	}

	var got [][]string
	req := PageRequest{Limit: 2}
	for {
		page, err := r.Tree(ctx, ".", -1, req)
		if err != nil {
			t.Fatalf("Tree(%+v) error = %v", req, err)
		}
		got = append(got, page.Paths)
		if page.Next == "" || len(got) > 5 {
			break
		}
		req.After = page.Next
	}
	want := [][]string{{"a.md", "a/b.md"}, {"a/c/d.md", "b.md"}}
	if !slices.EqualFunc(got, want, slices.Equal) {
		t.Errorf("pages = %v, want %v", got, want)
	}
}

func TestDirStoreRejectsEscapingPaths(t *testing.T) {
	r, err := OpenDir(t.TempDir())
	if err != nil {
		t.Fatalf("OpenDir() error = %v", err)
	}
	defer r.Close()
	for _, name := range []string{"../x.md", "/etc/passwd", "a/../../x.md", "."} {
		if _, err := r.Write(t.Context(), name, "", ""); err == nil {
			t.Errorf("Write(%q) error = nil", name)
		}
	}
}

func TestDirStoreConditionalWrite(t *testing.T) {
	ctx := t.Context()
	r, err := OpenDir(t.TempDir())
	if err != nil {
		t.Fatalf("OpenDir() error = %v", err)
	}
	defer r.Close()

	v1, err := r.Write(ctx, "a.md", "one", "")
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if _, got, _ := r.Read(ctx, "a.md"); got != v1 {
		t.Errorf("Read() version = %q, want %q", got, v1)
	}
	v2, err := r.Write(ctx, "a.md", "two", v1)
	if err != nil {
		t.Fatalf("Write(ifMatch current) error = %v", err)
	}
	if v2 == v1 {
		t.Errorf("version did not change after content changed")
	}
	if _, err := r.Write(ctx, "a.md", "three", v1); !errors.Is(err, ErrConflict) {
		t.Errorf("Write(ifMatch stale) error = %v, want %v", err, ErrConflict)
	}
	if got, _, _ := r.Read(ctx, "a.md"); got != "two" {
		t.Errorf("content after conflict = %q, want %q", got, "two")
	}
	if _, err := r.Write(ctx, "missing.md", "x", v1); !errors.Is(err, ErrConflict) {
		t.Errorf("Write(missing, ifMatch) error = %v, want %v", err, ErrConflict)
	}
}

func TestDirStoreMove(t *testing.T) {
	ctx := t.Context()
	r, err := OpenDir(t.TempDir())
	if err != nil {
		t.Fatalf("OpenDir() error = %v", err)
	}
	defer r.Close()
	for _, p := range []string{"a.md", "b.md"} {
		if _, err := r.Write(ctx, p, p, ""); err != nil {
			t.Fatalf("Write(%q) error = %v", p, err)
		}
	}

	if err := r.Move(ctx, "a.md", "b.md"); !errors.Is(err, ErrExists) {
		t.Errorf("Move() onto existing error = %v, want %v", err, ErrExists)
	}
	if err := r.Move(ctx, "missing.md", "c.md"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Move() missing error = %v, want %v", err, ErrNotFound)
	}
	if err := r.Move(ctx, "a.md", "../a.md"); err == nil {
		t.Errorf("Move() out of root succeeded, want an error")
	}

	if err := r.Move(ctx, "a.md", "sub/dir/c.md"); err != nil {
		t.Fatalf("Move() error = %v", err)
	}
	if _, _, err := r.Read(ctx, "a.md"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Read(a.md) after Move error = %v, want %v", err, ErrNotFound)
	}
	if data, _, err := r.Read(ctx, "sub/dir/c.md"); err != nil || data != "a.md" {
		t.Errorf("Read(sub/dir/c.md) = %q, %v, want %q", data, err, "a.md")
	}
	if data, _, _ := r.Read(ctx, "b.md"); data != "b.md" {
		t.Errorf("Read(b.md) = %q, want it untouched", data)
	}
}
