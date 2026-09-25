package mcpcmd

import (
	"runtime/debug"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// newServer builds the MCP server. Register tools, resources and prompts here.
func newServer() *mcp.Server {
	version := "(devel)"
	if info, ok := debug.ReadBuildInfo(); ok {
		version = info.Main.Version
	}
	return mcp.NewServer(&mcp.Implementation{Name: "okf-storage", Version: version}, nil)
}
