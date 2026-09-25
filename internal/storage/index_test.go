package storage

import (
	"slices"
	"testing"

	"github.com/yz4230/okf-storage/internal/okf"
)

func mustFrontmatter(t *testing.T, yaml string) *okf.Frontmatter {
	t.Helper()
	doc, err := okf.ParseDocument("---\n" + yaml + "\n---\n")
	if err != nil {
		t.Fatalf("ParseDocument() error = %v", err)
	}
	return doc.Frontmatter
}

func TestMemoryIndexSearch(t *testing.T) {
	ctx := t.Context()
	x := NewMemoryIndex()
	x.Put(ctx, "b.md", mustFrontmatter(t, "type: Metric\ntags: [finance, kpi]\nversion: 2"))
	x.Put(ctx, "a.md", mustFrontmatter(t, "type: Metric\ntags: [finance]"))
	x.Put(ctx, "c.md", mustFrontmatter(t, "type: Playbook"))
	x.Put(ctx, "index.md", nil)

	tests := []struct {
		name   string
		filter map[string]any
		want   []string
	}{
		{"empty filter", nil, []string{"a.md", "b.md", "c.md", "index.md"}},
		{"scalar", map[string]any{"type": "Metric"}, []string{"a.md", "b.md"}},
		{"list contains", map[string]any{"tags": "kpi"}, []string{"b.md"}},
		{"all keys", map[string]any{"type": "Metric", "tags": "finance"}, []string{"a.md", "b.md"}},
		{"json number", map[string]any{"version": float64(2)}, []string{"b.md"}},
		{"missing key", map[string]any{"owner": "x"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := x.Search(ctx, tt.filter)
			if err != nil {
				t.Fatalf("Search() error = %v", err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("Search() = %v, want %v", got, tt.want)
			}
		})
	}

	x.Delete(ctx, "a.md")
	if got, _ := x.Search(ctx, map[string]any{"type": "Metric"}); !slices.Equal(got, []string{"b.md"}) {
		t.Errorf("Search() after Delete = %v, want [b.md]", got)
	}
}
