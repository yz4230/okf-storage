package mcpcmd

import (
	_ "embed"
	"runtime/debug"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/yz4230/okf-storage/internal/bundle"
)

// instructions tells agents to consult and maintain the bundle on their own;
// clients typically add it to the model's system prompt.
//
//go:embed instructions.md
var instructions string

// newServer builds the MCP server. Register tools, resources and prompts here.
func newServer(b *bundle.Bundle) *mcp.Server {
	version := "(devel)"
	if info, ok := debug.ReadBuildInfo(); ok {
		version = info.Main.Version
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "okf-storage", Version: version}, &mcp.ServerOptions{
		Instructions: instructions,
	})
	addTools(server, b)
	addResources(server, b)
	return server
}
