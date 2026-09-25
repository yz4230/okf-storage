package mcpcmd

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/yz4230/okf-storage/internal/bundle"
)

func connect(t *testing.T) (*mcp.ClientSession, *bundle.Bundle) {
	t.Helper()
	store, err := bundle.OpenDir(t.TempDir())
	if err != nil {
		t.Fatalf("OpenDir() error = %v", err)
	}
	t.Cleanup(func() { store.Close() })
	b := bundle.NewBundle(store, bundle.NewMemCatalog())

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

func callPaths(t *testing.T, cs *mcp.ClientSession, tool string, args map[string]any) pathsOutput {
	t.Helper()
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool(%s, %v) error = %v", tool, args, err)
	}
	if res.IsError {
		t.Fatalf("CallTool(%s, %v) failed: %v", tool, args, res.Content)
	}
	data, _ := json.Marshal(res.StructuredContent)
	var out pathsOutput
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("decoding %s output: %v", tool, err)
	}
	return out
}

func TestPagedTools(t *testing.T) {
	cs, b := connect(t)
	for i := range 3 {
		if err := b.Write(t.Context(), fmt.Sprintf("d%d.md", i), "---\ntype: Metric\n---\n"); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
	}

	for _, tool := range []string{"tree", "search"} {
		t.Run(tool, func(t *testing.T) {
			var got []string
			args := map[string]any{"limit": 2}
			if tool == "search" {
				args["filter"] = map[string]any{"type": "Metric"}
			}
			first := callPaths(t, cs, tool, args)
			got = append(got, first.Paths...)
			if first.Next == "" {
				t.Fatalf("first page = %+v, want a next", first)
			}
			args["after"] = first.Next
			second := callPaths(t, cs, tool, args)
			got = append(got, second.Paths...)
			if want := []string{"d0.md", "d1.md", "d2.md"}; !slices.Equal(got, want) || second.Next != "" {
				t.Errorf("pages = %v then next %q, want %v and no next", got, second.Next, want)
			}
		})
	}
}

func TestPageLimitIsBounded(t *testing.T) {
	cs, _ := connect(t)
	for _, limit := range []int{0, maxLimit + 1} {
		res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: "tree", Arguments: map[string]any{"limit": limit}})
		if err == nil && !res.IsError {
			t.Errorf("tree(limit: %d) succeeded, want a validation error", limit)
		}
	}
	if got := (pageInput{}).request().Limit; got != defaultLimit {
		t.Errorf("default limit = %d, want %d", got, defaultLimit)
	}
}

func TestSearchRejectsInvalidFilter(t *testing.T) {
	cs, _ := connect(t)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: "search", Arguments: map[string]any{
		"filter": map[string]any{"tags": []any{"a"}},
	}})
	if err == nil && !res.IsError {
		t.Errorf("search with a list value succeeded, want an error")
	}
}
