package localbundle

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/yz4230/okf-storage/internal/okf"
)

func TestMatch(t *testing.T) {
	const fm = `
type: Concept
draft: false
year: 2024
offset: -2
ratio: 0.5
empty: null
tags: [go, mcp, 2]
owner:
  name: yz
  teams: [a, b]
items:
  - {id: 1}
  - {id: 2}
`
	tests := []struct {
		name   string
		filter string
		want   bool
	}{
		{"empty filter", `{}`, true},
		{"string", `{"type": "Concept"}`, true},
		{"string mismatch", `{"type": "Guide"}`, false},
		{"missing key", `{"status": "done"}`, false},
		{"all keys", `{"type": "Concept", "year": 2024}`, true},
		{"one key mismatch", `{"type": "Concept", "year": 2023}`, false},
		{"bool", `{"draft": false}`, true},
		{"bool mismatch", `{"draft": true}`, false},
		{"null", `{"empty": null}`, true},
		{"null vs value", `{"type": null}`, false},

		{"uint64 vs float64", `{"year": 2024}`, true},
		{"int64 vs float64", `{"offset": -2}`, true},
		{"float64", `{"ratio": 0.5}`, true},
		{"number vs string", `{"year": "2024"}`, false},
		{"string vs number", `{"type": 1}`, false},
		{"bool vs number", `{"draft": 0}`, false},

		{"list contains", `{"tags": "mcp"}`, true},
		{"list contains number", `{"tags": 2}`, true},
		{"list lacks", `{"tags": "rust"}`, false},
		{"list contains all", `{"tags": ["go", "mcp"]}`, true},
		{"list contains some", `{"tags": ["go", "rust"]}`, false},
		{"empty list filter", `{"tags": []}`, true},
		{"list filter vs scalar", `{"type": ["Concept"]}`, false},

		{"nested", `{"owner": {"name": "yz"}}`, true},
		{"nested mismatch", `{"owner": {"name": "other"}}`, false},
		{"nested missing key", `{"owner": {"email": "x"}}`, false},
		{"nested list", `{"owner": {"teams": "b"}}`, true},
		{"map filter vs scalar", `{"type": {"name": "yz"}}`, false},
		{"scalar filter vs map", `{"owner": "yz"}`, false},
		{"list of maps", `{"items": {"id": 2}}`, true},
		{"list of maps lacks", `{"items": {"id": 3}}`, false},
		{"scalar filter vs list of maps", `{"items": 1}`, false},
	}

	doc, err := okf.ParseDocument("---" + fm + "---\n")
	if err != nil {
		t.Fatalf("ParseDocument() error = %v", err)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var filter map[string]any
			if err := json.Unmarshal([]byte(tt.filter), &filter); err != nil {
				t.Fatalf("json.Unmarshal(%s) error = %v", tt.filter, err)
			}
			if got := match(map[string]any(doc.Frontmatter), filter); got != tt.want {
				t.Errorf("match(%s) = %v, want %v", tt.filter, got, tt.want)
			}
		})
	}
}

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
