package mcpcmd

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestResources(t *testing.T) {
	cs, b := connect(t)
	const content = "---\ntype: Metric\n---\n# Revenue\n"
	if err := b.Write(t.Context(), "metrics/revenue.md", content); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	for uri, want := range map[string]string{
		guideURI:                          guide,
		specURI:                           spec,
		"okf://docs/metrics/revenue.md":   content,
		"okf://docs/metrics%2Frevenue.md": content,
	} {
		res, err := cs.ReadResource(t.Context(), &mcp.ReadResourceParams{URI: uri})
		if err != nil {
			t.Errorf("ReadResource(%s) error = %v", uri, err)
			continue
		}
		if got := res.Contents[0].Text; got != want {
			t.Errorf("ReadResource(%s) = %q, want %q", uri, got, want)
		}
	}

	for _, uri := range []string{"okf://docs/missing.md", "okf://docs/../secret.md"} {
		if _, err := cs.ReadResource(t.Context(), &mcp.ReadResourceParams{URI: uri}); err == nil {
			t.Errorf("ReadResource(%s) succeeded, want an error", uri)
		}
	}
}
