package bundle

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/yz4230/okf-storage/internal/okf"
)

// paths drops the error of a search or tree call for use in assertions,
// where a failed call shows up as missing paths.
func paths(p Page, _ error) []string {
	return p.Paths
}

func mustFrontmatter(t *testing.T, yaml string) okf.Frontmatter {
	t.Helper()
	doc, err := okf.ParseDocument("---\n" + yaml + "\n---\n")
	if err != nil {
		t.Fatalf("ParseDocument() error = %v", err)
	}
	return doc.Frontmatter
}

func newTestBundle(t *testing.T) (*Bundle, *DirStore, *MemCatalog) {
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
			if _, err := b.Write(ctx, "a.md", orig); err != nil {
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
	if _, err := b.Write(ctx, "a.md", "---\ntype: Metric\n---\n"); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := b.Edit(ctx, "a.md", "Metric", "Playbook", false); err != nil {
		t.Fatalf("Edit() error = %v", err)
	}
	if got := paths(b.Search(ctx, map[string]any{"type": "Playbook"}, PageRequest{})); !slices.Equal(got, []string{"a.md"}) {
		t.Errorf("Search(Playbook) = %v, want [a.md]", got)
	}
	if got := paths(b.Search(ctx, map[string]any{"type": "Metric"}, PageRequest{})); got != nil {
		t.Errorf("Search(Metric) = %v, want none", got)
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
		if err := store.Write(ctx, path, content); err != nil {
			t.Fatalf("Write(%q) error = %v", path, err)
		}
	}

	if err := Reindex(ctx, store, catalog); err != nil {
		t.Fatalf("Reindex() error = %v", err)
	}
	if got := paths(catalog.Search(ctx, nil, PageRequest{})); !slices.Equal(got, []string{"metrics/revenue.md"}) {
		t.Errorf("Search() after Reindex = %v, want [metrics/revenue.md]", got)
	}
}

func TestBundleWriteDropsRemovedFrontmatter(t *testing.T) {
	ctx := t.Context()
	b, _, _ := newTestBundle(t)
	if _, err := b.Write(ctx, "a.md", "---\ntype: Metric\n---\n"); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if _, err := b.Write(ctx, "a.md", "# plain\n"); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if got := paths(b.Search(ctx, nil, PageRequest{})); got != nil {
		t.Errorf("Search() = %v, want none", got)
	}
}

func TestBundleRejectsInvalidFrontmatterWithoutWriting(t *testing.T) {
	ctx := t.Context()
	b, _, _ := newTestBundle(t)
	const orig = "---\ntype: Metric\n---\n"
	if _, err := b.Write(ctx, "a.md", orig); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	if _, err := b.Write(ctx, "a.md", "---\ntype: [\n---\n"); err == nil {
		t.Errorf("Write(invalid) error = nil")
	}
	if err := b.Edit(ctx, "a.md", "Metric", "[", false); err == nil {
		t.Errorf("Edit(invalid) error = nil")
	}
	if got, _ := b.Read(ctx, "a.md"); got != orig {
		t.Errorf("content = %q, want unchanged %q", got, orig)
	}
	if got := paths(b.Search(ctx, map[string]any{"type": "Metric"}, PageRequest{})); !slices.Equal(got, []string{"a.md"}) {
		t.Errorf("Search() = %v, want [a.md]", got)
	}
}

func TestBundleConcurrentWritesLeaveCatalogConsistent(t *testing.T) {
	ctx := t.Context()
	b, store, catalog := newTestBundle(t)

	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() {
			content := fmt.Sprintf("---\ntype: T%d\n---\n", i)
			if _, err := b.Write(ctx, "a.md", content); err != nil {
				t.Errorf("Write() error = %v", err)
			}
		})
	}
	wg.Wait()

	doc, err := load(ctx, store, "a.md")
	if err != nil {
		t.Fatalf("load() error = %v", err)
	}
	want := doc.Frontmatter["type"]
	if got := paths(catalog.Search(ctx, map[string]any{"type": want}, PageRequest{})); !slices.Equal(got, []string{"a.md"}) {
		t.Errorf("Search(%v) = %v, want [a.md]", want, got)
	}
	if got := paths(catalog.Search(ctx, nil, PageRequest{})); len(got) != 1 {
		t.Errorf("catalog has %d entries, want 1", len(got))
	}
}

func TestBundleConcurrentEditsKeepEveryChange(t *testing.T) {
	ctx := t.Context()
	b, _, _ := newTestBundle(t)
	const n = 20
	var content strings.Builder
	for i := range n {
		fmt.Fprintf(&content, "- item %d: todo\n", i)
	}
	if _, err := b.Write(ctx, "a.md", content.String()); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			item := fmt.Sprintf("item %d: ", i)
			if err := b.Edit(ctx, "a.md", item+"todo", item+"done", false); err != nil {
				t.Errorf("Edit() error = %v", err)
			}
		})
	}
	wg.Wait()

	got, _ := b.Read(ctx, "a.md")
	if strings.Contains(got, "todo") {
		t.Errorf("content after concurrent edits = %q, want every item done", got)
	}
}

func TestReindexDropsOrphans(t *testing.T) {
	ctx := t.Context()
	_, store, catalog := newTestBundle(t)
	catalog.Put(ctx, "gone.md", mustFrontmatter(t, "type: Metric"))

	if err := Reindex(ctx, store, catalog); err != nil {
		t.Fatalf("Reindex() error = %v", err)
	}
	if got := paths(catalog.Search(ctx, nil, PageRequest{})); got != nil {
		t.Errorf("Search() after Reindex = %v, want none", got)
	}
}

func TestEachPage(t *testing.T) {
	all := []string{"a.md", "b.md", "c.md"}
	var fetches int
	var got []string
	err := eachPage(func(req PageRequest) (Page, error) {
		fetches++
		req.Limit = 2
		return paginate(all, req), nil
	}, func(path string) error {
		got = append(got, path)
		return nil
	})
	if err != nil || !slices.Equal(got, all) || fetches != 2 {
		t.Errorf("eachPage() visited %v in %d fetches, err = %v; want %v in 2", got, fetches, err, all)
	}
}

func TestBundleDeleteDir(t *testing.T) {
	ctx := t.Context()
	b, store, _ := newTestBundle(t)
	// More than one page, so that the listing continues after the last
	// document of a subdirectory, and the subdirectory itself, are gone.
	var want []string
	for i := range listPageSize + 1 {
		p := fmt.Sprintf("drafts/%d/a.md", i)
		want = append(want, p)
		if _, err := b.Write(ctx, p, "---\ntype: Draft\n---\n"); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
	}
	slices.Sort(want)
	if _, err := b.Write(ctx, "drafts-kept.md", "---\ntype: Draft\n---\n"); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	if _, err := b.DeleteDir(ctx, "."); err == nil {
		t.Errorf("DeleteDir(.) succeeded, want an error")
	}
	deleted, err := b.DeleteDir(ctx, "drafts")
	if err != nil || !slices.Equal(deleted, want) {
		t.Fatalf("DeleteDir() = %d paths, %v, want %d", len(deleted), err, len(want))
	}
	if got := paths(b.Search(ctx, nil, PageRequest{})); !slices.Equal(got, []string{"drafts-kept.md"}) {
		t.Errorf("Search() after DeleteDir = %v, want [drafts-kept.md]", got)
	}
	if entries, _ := store.List(ctx, "."); !slices.Equal(entries, []Entry{{"drafts-kept.md", false}}) {
		t.Errorf("List(.) after DeleteDir = %v, want the emptied directory gone", entries)
	}
}

func TestBundleMove(t *testing.T) {
	ctx := t.Context()
	b, _, _ := newTestBundle(t)
	if _, err := b.Write(ctx, "a.md", "---\ntype: Metric\n---\n"); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := b.Move(ctx, "a.md", "metrics/a.md"); err != nil {
		t.Fatalf("Move() error = %v", err)
	}
	if got := paths(b.Search(ctx, map[string]any{"type": "Metric"}, PageRequest{})); !slices.Equal(got, []string{"metrics/a.md"}) {
		t.Errorf("Search(Metric) after Move = %v, want [metrics/a.md]", got)
	}
}
