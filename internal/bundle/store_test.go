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

	if err := r.Write(ctx, "metrics/revenue.md", "---\ntype: Metric\n---\n"); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := r.Write(ctx, "index.md", "# Index\n"); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	os.WriteFile(filepath.Join(dir, "notes.txt"), nil, 0o644)
	os.MkdirAll(filepath.Join(dir, ".git"), 0o755)
	os.WriteFile(filepath.Join(dir, ".git", "x.md"), nil, 0o644)

	data, err := r.Read(ctx, "metrics/revenue.md")
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

	names, err := r.Tree(ctx, ".", -1)
	if err != nil {
		t.Fatalf("Tree() error = %v", err)
	}
	if want := []string{"index.md", "metrics/revenue.md"}; !slices.Equal(names, want) {
		t.Errorf("Tree(.) = %v, want %v", names, want)
	}
	names, err = r.Tree(ctx, ".", 1)
	if want := []string{"index.md"}; err != nil || !slices.Equal(names, want) {
		t.Errorf("Tree(., 1) = %v, %v, want %v", names, err, want)
	}
	names, err = r.Tree(ctx, "metrics", 1)
	if want := []string{"metrics/revenue.md"}; err != nil || !slices.Equal(names, want) {
		t.Errorf("Tree(metrics) = %v, %v, want %v", names, err, want)
	}
	if _, err := r.Tree(ctx, "missing", -1); !errors.Is(err, ErrNotFound) {
		t.Errorf("Tree(missing) error = %v, want %v", err, ErrNotFound)
	}

	if err := r.Delete(ctx, "index.md"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := r.Read(ctx, "index.md"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Read() after Delete error = %v, want %v", err, ErrNotFound)
	}
	if err := r.Delete(ctx, "index.md"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete() missing error = %v, want %v", err, ErrNotFound)
	}
}

func TestDirStoreRejectsEscapingPaths(t *testing.T) {
	r, err := OpenDir(t.TempDir())
	if err != nil {
		t.Fatalf("OpenDir() error = %v", err)
	}
	defer r.Close()
	for _, name := range []string{"../x.md", "/etc/passwd", "a/../../x.md", "."} {
		if err := r.Write(t.Context(), name, ""); err == nil {
			t.Errorf("Write(%q) error = nil", name)
		}
	}
}

func TestDirStoreEdit(t *testing.T) {
	ctx := t.Context()
	r, err := OpenDir(t.TempDir())
	if err != nil {
		t.Fatalf("OpenDir() error = %v", err)
	}
	defer r.Close()
	const orig = "---\ntype: Metric\n---\nfoo bar foo\n"

	tests := []struct {
		name       string
		old, new   string
		replaceAll bool
		want       string
		wantErr    error
	}{
		{"unique", "bar", "baz", false, "---\ntype: Metric\n---\nfoo baz foo\n", nil},
		{"ambiguous", "foo", "x", false, orig, ErrAmbiguousMatch},
		{"replace all", "foo", "x", true, "---\ntype: Metric\n---\nx bar x\n", nil},
		{"no match", "qux", "x", false, orig, ErrNoMatch},
		{"whitespace must match exactly", "foo  bar", "x", false, orig, ErrNoMatch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := r.Write(ctx, "a.md", orig); err != nil {
				t.Fatalf("Write() error = %v", err)
			}
			err := r.Edit(ctx, "a.md", tt.old, tt.new, tt.replaceAll)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Edit() error = %v, want %v", err, tt.wantErr)
			}
			if got, _ := r.Read(ctx, "a.md"); got != tt.want {
				t.Errorf("content = %q, want %q", got, tt.want)
			}
		})
	}

	if err := r.Edit(ctx, "missing.md", "a", "b", false); !errors.Is(err, ErrNotFound) {
		t.Errorf("Edit(missing) error = %v, want %v", err, ErrNotFound)
	}
	for _, args := range [][2]string{{"", "x"}, {"foo", "foo"}} {
		if err := r.Edit(ctx, "a.md", args[0], args[1], false); err == nil {
			t.Errorf("Edit(%q, %q) error = nil", args[0], args[1])
		}
	}
}
