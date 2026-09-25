package mcpcmd

import (
	"runtime/debug"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/yz4230/okf-storage/internal/bundle"
)

// newServer builds the MCP server. Register tools, resources and prompts here.
func newServer(b bundle.Bundle) *mcp.Server {
	version := "(devel)"
	if info, ok := debug.ReadBuildInfo(); ok {
		version = info.Main.Version
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "okf-storage", Version: version}, nil)
	addTools(server, b)
	addResources(server, b)
	return server
}
