package mcpcmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/yz4230/okf-storage/internal/localbundle"
)

func newBundle(t *testing.T) *localbundle.LocalBundle {
	t.Helper()
	return openBundle(t, t.TempDir())
}

func openBundle(t *testing.T, dir string) *localbundle.LocalBundle {
	t.Helper()
	b, err := localbundle.NewLocalBundle(dir)
	if err != nil {
		t.Fatalf("NewLocalBundle() error = %v", err)
	}
	t.Cleanup(func() { b.Close() })
	return b
}

func connect(t *testing.T) (*mcp.ClientSession, *localbundle.LocalBundle) {
	t.Helper()
	return connectDir(t, t.TempDir())
}

func connectDir(t *testing.T, dir string) (*mcp.ClientSession, *localbundle.LocalBundle) {
	t.Helper()
	b := openBundle(t, dir)
	serverT, clientT := mcp.NewInMemoryTransports()
	ss, err := newServer(b).Connect(t.Context(), serverT, nil)
	if err != nil {
		t.Fatalf("server Connect() error = %v", err)
	}
	t.Cleanup(func() { ss.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil)
	cs, err := client.Connect(t.Context(), clientT, nil)
	if err != nil {
		t.Fatalf("client Connect() error = %v", err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs, b
}

func write(t *testing.T, b *localbundle.LocalBundle, docs map[string]string) {
	t.Helper()
	for p, content := range docs {
		if _, err := b.Write(p, content); err != nil {
			t.Fatalf("Write(%q) error = %v", p, err)
		}
	}
}

func seed(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for p, content := range files {
		name := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func call(t *testing.T, cs *mcp.ClientSession, tool string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool(%s, %v) error = %v", tool, args, err)
	}
	return res
}

func callStructured[T any](t *testing.T, cs *mcp.ClientSession, tool string, args map[string]any) T {
	t.Helper()
	res := call(t, cs, tool, args)
	if res.IsError {
		t.Fatalf("CallTool(%s, %v) failed: %v", tool, args, res.Content)
	}
	data, _ := json.Marshal(res.StructuredContent)
	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("decoding %s output: %v", tool, err)
	}
	return out
}

func TestWrite(t *testing.T) {
	cs, b := connect(t)
	for _, tc := range []struct{ content, want string }{
		{"---\ntype: Metric\n---\nfirst\n", "created a.md"},
		{"---\ntype: Metric\n---\nsecond\n", "overwrote a.md"},
	} {
		res := call(t, cs, "write", map[string]any{"path": "a.md", "content": tc.content})
		if res.IsError {
			t.Fatalf("write failed: %v", res.Content)
		}
		if got := res.Content[0].(*mcp.TextContent).Text; got != tc.want {
			t.Errorf("write result = %q, want %q", got, tc.want)
		}
		if got, _ := b.Read("a.md"); got != tc.content {
			t.Errorf("content after write = %q, want %q", got, tc.content)
		}
	}
}

func TestList(t *testing.T) {
	cs, _ := connectDir(t, seed(t, map[string]string{"a.md": "# A\n", "sub/b.md": "# B\n", ".hidden.md": "# H\n", ".git/c.md": "# C\n"}))

	got := callStructured[listOutput](t, cs, "list", nil)
	if want := []string{"a.md", "sub/"}; !slices.Equal(got.Entries, want) {
		t.Errorf("list = %v, want %v", got.Entries, want)
	}
}

func TestDeleteRemovesEmptyDirs(t *testing.T) {
	cs, b := connect(t)
	write(t, b, map[string]string{"drafts/old/a.md": "# A\n", "drafts/b.md": "# B\n"})

	if res := call(t, cs, "delete", map[string]any{"path": "drafts/old/a.md"}); res.IsError {
		t.Fatalf("delete failed: %v", res.Content)
	}
	if got := callStructured[listOutput](t, cs, "list", map[string]any{"dir": "drafts"}); !slices.Equal(got.Entries, []string{"b.md"}) {
		t.Errorf("list(drafts) = %v, want [b.md]", got.Entries)
	}
	if res := call(t, cs, "delete", map[string]any{"path": "drafts/b.md"}); res.IsError {
		t.Fatalf("delete failed: %v", res.Content)
	}
	if got := callStructured[listOutput](t, cs, "list", nil); len(got.Entries) != 0 {
		t.Errorf("list = %v, want empty", got.Entries)
	}
}

func TestMove(t *testing.T) {
	cs, b := connect(t)
	write(t, b, map[string]string{"old/a.md": "---\ntype: Metric\n---\n", "b.md": "---\ntype: Metric\n---\n"})

	if res := call(t, cs, "move", map[string]any{"from": "old/a.md", "to": "b.md"}); !res.IsError {
		t.Errorf("move onto an existing document succeeded, want an error")
	}
	if res := call(t, cs, "move", map[string]any{"from": "old/a.md", "to": "metrics/a.md"}); res.IsError {
		t.Fatalf("move failed: %v", res.Content)
	}
	if got := callStructured[listOutput](t, cs, "list", nil); !slices.Equal(got.Entries, []string{"b.md", "metrics/"}) {
		t.Errorf("list after move = %v, want [b.md metrics/]", got.Entries)
	}
}

func TestSearch(t *testing.T) {
	cs, _ := connectDir(t, seed(t, map[string]string{
		"index.md":  "# Index\nMetric overview\n",
		"b.md":      "---\ntype: Metric\ntags: [billing]\n---\nRevenue per month.\n",
		"a/c.md":    "---\ntype: Metric\n---\nChurn rate.\n",
		"a/d.md":    "---\ntype: Playbook\n---\nOn-call steps for revenue alerts.\n",
		".git/x.md": "---\ntype: Metric\n---\nRevenue\n",
		"a/.x.md":   "---\ntype: Metric\n---\nRevenue\n",
		"notes.txt": "Revenue\n",
	}))

	tests := []struct {
		tool string
		args map[string]any
		want []string
	}{
		{"search_frontmatter", nil, []string{"a/c.md", "a/d.md", "b.md"}},
		{"search_frontmatter", map[string]any{"filter": map[string]any{"type": "Metric"}}, []string{"a/c.md", "b.md"}},
		{"search_frontmatter", map[string]any{"filter": map[string]any{"tags": "billing"}}, []string{"b.md"}},
		{"search_content", map[string]any{"query": "(?i)revenue"}, []string{"a/d.md", "b.md"}},
		{"search_content", map[string]any{"query": "Metric"}, []string{"index.md"}},
	}
	for _, tt := range tests {
		got := callStructured[pathsOutput](t, cs, tt.tool, tt.args)
		if !slices.Equal(got.Paths, tt.want) {
			t.Errorf("%s(%v) = %v, want %v", tt.tool, tt.args, got.Paths, tt.want)
		}
	}

	if res := call(t, cs, "search_content", map[string]any{"query": "("}); !res.IsError {
		t.Errorf("search_content with an invalid pattern succeeded, want an error")
	}
}
