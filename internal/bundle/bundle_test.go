package bundle

import (
	"context"
	"errors"
	"slices"
	"testing"
)

func newTestBundle(t *testing.T) (Bundle, *DirStore, *MemCatalog) {
	t.Helper()
	store, err := OpenDir(t.TempDir())
	if err != nil {
		t.Fatalf("OpenDir() error = %v", err)
	}
	t.Cleanup(func() { store.Close() })
	catalog := NewMemCatalog()
	return NewBundle(store, catalog), store, catalog
}

func TestBundleEdit(t *testing.T) {
	ctx := t.Context()
	b, _, _ := newTestBundle(t)
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
			if err := b.Write(ctx, "a.md", orig); err != nil {
				t.Fatalf("Write() error = %v", err)
			}
			err := b.Edit(ctx, "a.md", tt.old, tt.new, tt.replaceAll)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Edit() error = %v, want %v", err, tt.wantErr)
			}
			if got, _ := b.Read(ctx, "a.md"); got != tt.want {
				t.Errorf("content = %q, want %q", got, tt.want)
			}
		})
	}

	if err := b.Edit(ctx, "missing.md", "a", "b", false); !errors.Is(err, ErrNotFound) {
		t.Errorf("Edit(missing) error = %v, want %v", err, ErrNotFound)
	}
	for _, args := range [][2]string{{"", "x"}, {"foo", "foo"}} {
		if err := b.Edit(ctx, "a.md", args[0], args[1], false); err == nil {
			t.Errorf("Edit(%q, %q) error = nil", args[0], args[1])
		}
	}
}

func TestBundleEditUpdatesCatalog(t *testing.T) {
	ctx := t.Context()
	b, _, _ := newTestBundle(t)
	if err := b.Write(ctx, "a.md", "---\ntype: Metric\n---\n"); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := b.Edit(ctx, "a.md", "Metric", "Playbook", false); err != nil {
		t.Fatalf("Edit() error = %v", err)
	}
	if got, _ := b.Search(ctx, map[string]any{"type": "Playbook"}); !slices.Equal(got, []string{"a.md"}) {
		t.Errorf("Search(Playbook) = %v, want [a.md]", got)
	}
	if got, _ := b.Search(ctx, map[string]any{"type": "Metric"}); got != nil {
		t.Errorf("Search(Metric) = %v, want none", got)
	}
}

// racingStore simulates another writer changing the document right after
// each of the first n reads, so the following conditional write conflicts.
type racingStore struct {
	*DirStore
	n int
}

func (s *racingStore) Read(ctx context.Context, path string) (string, Version, error) {
	content, ver, err := s.DirStore.Read(ctx, path)
	if err == nil && s.n > 0 {
		s.n--
		if _, err := s.DirStore.Write(ctx, path, content+"theirs\n", ""); err != nil {
			return "", "", err
		}
	}
	return content, ver, err
}

func TestBundleEditRetriesOnConflict(t *testing.T) {
	ctx := t.Context()
	_, dir, _ := newTestBundle(t)
	if _, err := dir.Write(ctx, "a.md", "mine\n", ""); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	store := &racingStore{DirStore: dir, n: 1}
	b := NewBundle(store, NewMemCatalog())
	if err := b.Edit(ctx, "a.md", "mine", "edited", false); err != nil {
		t.Fatalf("Edit() error = %v", err)
	}
	if got, _ := b.Read(ctx, "a.md"); got != "edited\ntheirs\n" {
		t.Errorf("content = %q, want the concurrent write preserved", got)
	}

	store.n = maxEditAttempts
	if err := b.Edit(ctx, "a.md", "edited", "again", false); !errors.Is(err, ErrConflict) {
		t.Errorf("Edit() under persistent contention error = %v, want %v", err, ErrConflict)
	}
}

func TestReindex(t *testing.T) {
	ctx := t.Context()
	_, store, catalog := newTestBundle(t)
	for path, content := range map[string]string{
		"metrics/revenue.md": "---\ntype: Metric\n---\n",
		"index.md":           "# Index\n",
		"broken.md":          "---\ntype: [\n---\n",
	} {
		if _, err := store.Write(ctx, path, content, ""); err != nil {
			t.Fatalf("Write(%q) error = %v", path, err)
		}
	}

	if err := Reindex(ctx, store, catalog); err != nil {
		t.Fatalf("Reindex() error = %v", err)
	}
	if got, _ := catalog.Search(ctx, nil); !slices.Equal(got, []string{"metrics/revenue.md"}) {
		t.Errorf("Search() after Reindex = %v, want [metrics/revenue.md]", got)
	}
}
