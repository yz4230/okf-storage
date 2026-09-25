package mcpcmd

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/yz4230/okf-storage/internal/bundle"
)

func TestInstructions(t *testing.T) {
	cs, _ := connect(t)
	got := cs.InitializeResult().Instructions
	if !strings.HasPrefix(got, instructions) || instructions == "" {
		t.Errorf("Instructions = %q, want prefix %q", got, instructions)
	}
	if !strings.Contains(got, "no documents with frontmatter yet") {
		t.Errorf("Instructions = %q, want an empty-bundle overview", got)
	}
}

func TestInstructionsOverview(t *testing.T) {
	store, err := bundle.OpenDir(t.TempDir())
	if err != nil {
		t.Fatalf("OpenDir() error = %v", err)
	}
	t.Cleanup(func() { store.Close() })
	b := bundle.NewBundle(store, bundle.NewMemCatalog())
	docs := map[string]string{
		"a.md":         "---\ntype: Metric\ntags: [billing, finance]\n---\n",
		"b/c.md":       "---\ntype: Metric\ntags: billing\n---\n",
		"b/d.md":       "---\ntype: Playbook\n---\n",
		"notes/log.md": "# Update Log\n",
	}
	for path, content := range docs {
		if err := b.Write(t.Context(), path, content); err != nil {
			t.Fatalf("Write(%s) error = %v", path, err)
		}
	}

	connectClient := func() string {
		serverT, clientT := mcp.NewInMemoryTransports()
		ss, err := newServer(b).Connect(t.Context(), serverT, nil)
		if err != nil {
			t.Fatalf("server Connect() error = %v", err)
		}
		t.Cleanup(func() { ss.Close() })
		cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(t.Context(), clientT, nil)
		if err != nil {
			t.Fatalf("client Connect() error = %v", err)
		}
		t.Cleanup(func() { cs.Close() })
		return cs.InitializeResult().Instructions
	}
	got := connectClient()
	for _, want := range []string{
		"3 documents with frontmatter",
		"there is no root index.md",
		"- a.md\n- b/\n- notes/\n",
		"- `type`: Metric (2), Playbook (1)\n",
		"- `tags`: billing (2), finance (1)\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Instructions = %q, want it to contain %q", got, want)
		}
	}

	if err := b.Write(t.Context(), "index.md", "---\nokf_version: \"0.2\"\n---\n# Metrics\n\n* [A](a.md) - The A metric.\n"); err != nil {
		t.Fatalf("Write(index.md) error = %v", err)
	}
	got = connectClient()
	if want := "<index.md>\n# Metrics\n\n* [A](a.md) - The A metric.\n</index.md>"; !strings.Contains(got, want) {
		t.Errorf("Instructions = %q, want it to contain %q", got, want)
	}
	if strings.Contains(got, "okf_version") {
		t.Errorf("Instructions = %q, want index.md frontmatter left out", got)
	}
}
