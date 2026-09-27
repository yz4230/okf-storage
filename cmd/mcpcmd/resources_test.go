package mcpcmd

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestResources(t *testing.T) {
	cs, _ := connect(t)

	for uri, want := range map[string]string{
		guideURI: guide,
		specURI:  spec,
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
}
