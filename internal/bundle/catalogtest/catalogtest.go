// Package catalogtest checks that a bundle.Catalog implementation follows the
// contract documented on the interface, so that implementations backed by
// different databases answer searches identically.
package catalogtest

import (
	"errors"
	"slices"
	"testing"

	"github.com/yz4230/okf-storage/internal/bundle"
	"github.com/yz4230/okf-storage/internal/okf"
)

// Run tests the catalog returned by newCatalog, which must be empty.
func Run(t *testing.T, newCatalog func(t *testing.T) bundle.Catalog) {
	t.Run("Search", func(t *testing.T) { testSearch(t, newCatalog(t)) })
	t.Run("InvalidFilter", func(t *testing.T) { testInvalidFilter(t, newCatalog(t)) })
	t.Run("PutReplacesAndDeleteRemoves", func(t *testing.T) { testPutDelete(t, newCatalog(t)) })
	t.Run("Pagination", func(t *testing.T) { testPagination(t, newCatalog(t)) })
}

func frontmatter(t *testing.T, yaml string) *okf.Frontmatter {
	t.Helper()
	doc, err := okf.ParseDocument("---\n" + yaml + "\n---\n")
	if err != nil {
		t.Fatalf("ParseDocument() error = %v", err)
	}
	return doc.Frontmatter
}

func put(t *testing.T, c bundle.Catalog, path, yaml string) {
	t.Helper()
	if err := c.Put(t.Context(), path, frontmatter(t, yaml)); err != nil {
		t.Fatalf("Put(%q) error = %v", path, err)
	}
}

func search(t *testing.T, c bundle.Catalog, filter map[string]any) []string {
	t.Helper()
	page, err := c.Search(t.Context(), filter, bundle.PageRequest{})
	if err != nil {
		t.Fatalf("Search(%v) error = %v", filter, err)
	}
	if page.Next != "" {
		t.Errorf("Search(%v) Next = %q without a limit", filter, page.Next)
	}
	return page.Paths
}

func testSearch(t *testing.T, c bundle.Catalog) {
	put(t, c, "b.md", "type: Metric\ntags: [finance, kpi]\nversion: 2\nscores: [1, 2.5]")
	put(t, c, "a.md", "type: Metric\ntags: [finance]\nversion: \"2\"\ndraft: true")
	put(t, c, "c.md", "type: Playbook\ndraft: \"true\"\nratio: 1.5")
	put(t, c, "d.md", "a.b: dotted\n$op: dollar\nowner: {name: x}\na: {b: nested}")
	put(t, c, "Z.md", "type: metric")

	tests := []struct {
		name   string
		filter map[string]any
		want   []string
	}{
		{"empty filter matches all in byte order", nil, []string{"Z.md", "a.md", "b.md", "c.md", "d.md"}},
		{"string", map[string]any{"type": "Metric"}, []string{"a.md", "b.md"}},
		{"strings are case sensitive", map[string]any{"type": "metric"}, []string{"Z.md"}},
		{"list contains", map[string]any{"tags": "kpi"}, []string{"b.md"}},
		{"every key must match", map[string]any{"type": "Metric", "tags": "finance"}, []string{"a.md", "b.md"}},
		{"missing key", map[string]any{"owner": "x"}, nil},
		{"integer from JSON", map[string]any{"version": float64(2)}, []string{"b.md"}},
		{"integer from Go", map[string]any{"version": 2}, []string{"b.md"}},
		{"number does not match string", map[string]any{"version": "2"}, []string{"a.md"}},
		{"float", map[string]any{"ratio": 1.5}, []string{"c.md"}},
		{"number in list", map[string]any{"scores": 2.5}, []string{"b.md"}},
		{"bool does not match string", map[string]any{"draft": true}, []string{"a.md"}},
		{"string does not match bool", map[string]any{"draft": "true"}, []string{"c.md"}},
		{"dotted key is literal", map[string]any{"a.b": "dotted"}, []string{"d.md"}},
		{"dotted key does not reach nested field", map[string]any{"a.b": "nested"}, nil},
		{"dollar key is literal", map[string]any{"$op": "dollar"}, []string{"d.md"}},
		{"map field never matches", map[string]any{"owner": "{name: x}"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := search(t, c, tt.filter); !slices.Equal(got, tt.want) {
				t.Errorf("Search(%v) = %v, want %v", tt.filter, got, tt.want)
			}
		})
	}
}

func testInvalidFilter(t *testing.T, c bundle.Catalog) {
	put(t, c, "a.md", "type: Metric")
	for _, v := range []any{nil, []any{"Metric"}, map[string]any{"a": 1}} {
		_, err := c.Search(t.Context(), map[string]any{"type": v}, bundle.PageRequest{})
		if !errors.Is(err, bundle.ErrInvalidFilter) {
			t.Errorf("Search(type: %#v) error = %v, want %v", v, err, bundle.ErrInvalidFilter)
		}
	}
}

func testPutDelete(t *testing.T, c bundle.Catalog) {
	put(t, c, "a.md", "type: Metric")
	put(t, c, "a.md", "type: Playbook")
	if got := search(t, c, map[string]any{"type": "Metric"}); got != nil {
		t.Errorf("Search(Metric) after re-Put = %v, want none", got)
	}
	if got := search(t, c, map[string]any{"type": "Playbook"}); !slices.Equal(got, []string{"a.md"}) {
		t.Errorf("Search(Playbook) = %v, want [a.md]", got)
	}

	if err := c.Delete(t.Context(), "a.md"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if got := search(t, c, nil); got != nil {
		t.Errorf("Search() after Delete = %v, want none", got)
	}
	if err := c.Delete(t.Context(), "a.md"); err != nil {
		t.Errorf("Delete() of missing entry error = %v, want nil", err)
	}
}

func testPagination(t *testing.T, c bundle.Catalog) {
	// "a.md" sorts before "a/b.md" in byte order since '.' < '/'.
	for _, path := range []string{"a/b.md", "a.md", "c.md", "b.md", "x.md"} {
		put(t, c, path, "type: Metric")
	}
	put(t, c, "other.md", "type: Playbook")
	filter := map[string]any{"type": "Metric"}

	var got [][]string
	req := bundle.PageRequest{Limit: 2}
	for {
		page, err := c.Search(t.Context(), filter, req)
		if err != nil {
			t.Fatalf("Search(%+v) error = %v", req, err)
		}
		got = append(got, page.Paths)
		if page.Next == "" {
			break
		}
		if len(got) > 5 {
			t.Fatalf("pagination did not terminate: %v", got)
		}
		req.After = page.Next
	}
	want := [][]string{{"a.md", "a/b.md"}, {"b.md", "c.md"}, {"x.md"}}
	if !slices.EqualFunc(got, want, slices.Equal) {
		t.Errorf("pages = %v, want %v", got, want)
	}

	page, err := c.Search(t.Context(), filter, bundle.PageRequest{After: "b.mdz", Limit: 2})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if want := []string{"c.md", "x.md"}; !slices.Equal(page.Paths, want) || page.Next != "" {
		t.Errorf("Search(after a path not in results) = %+v, want %v with no Next", page, want)
	}
}
