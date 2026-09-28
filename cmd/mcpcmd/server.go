package mcpcmd

import (
	_ "embed"
	"io"
	"runtime/debug"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// bundle is the knowledge bundle the server exposes. Paths are
// slash-separated and relative to the bundle root.
type bundle interface {
	List(dir string) ([]string, error)
	Read(path string) (string, error)
	Write(path, content string) (created bool, err error)
	Edit(path, oldString, newString string, replaceAll bool) error
	Delete(path string) error
	Move(from, to string) error
	SearchFrontmatter(filter map[string]any) ([]string, error)
	SearchContent(pattern string) ([]string, error)
	Dump(w io.Writer) error
}

// instructions tells agents to consult and maintain the bundle on their own;
// clients typically add it to the model's system prompt.
//
//go:embed instructions.md
var instructions string

// newServer builds the MCP server. Register tools, resources and prompts here.
func newServer(b bundle) *mcp.Server {
	version := "(devel)"
	if info, ok := debug.ReadBuildInfo(); ok {
		version = info.Main.Version
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "okf-storage", Version: version}, &mcp.ServerOptions{
		Instructions: instructions,
	})
	server.AddReceivingMiddleware(withOverview(b))
	addTools(server, b)
	addResources(server)
	return server
}
