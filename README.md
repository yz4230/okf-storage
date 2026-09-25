# okf-storage

OKF storage CLI with an MCP (Model Context Protocol) server served over HTTP.

## Requirements

- Go 1.27 or later

## Usage

Start the MCP server (Streamable HTTP transport):

```bash
go run . mcp
```

The server listens on `localhost:8080` by default and exposes the MCP endpoint at `/mcp`.
Use `--addr` to change the listen address:

```bash
go run . mcp --addr 0.0.0.0:9000
```

Use `--dir` to choose the knowledge bundle root directory (default: current directory).
Existing documents are indexed by frontmatter at startup:

```bash
go run . mcp --dir ./knowledge
```

Enable debug logging with the global `--verbose` / `-v` flag:

```bash
go run . -v mcp
```

The server shuts down gracefully on `SIGINT` / `SIGTERM`.

## Tools

| Tool     | Description                                                           |
| -------- | --------------------------------------------------------------------- |
| `read`   | Read a document                                                       |
| `write`  | Create or overwrite a document                                        |
| `edit`   | Replace an exact string in a document (`replace_all` for every match) |
| `delete` | Delete a document                                                     |
| `list`   | List documents and subdirectories directly under a directory          |
| `tree`   | List document paths under a directory recursively (optional `depth`)  |
| `search` | Find documents whose frontmatter matches every field in `filter`      |

## Build

```bash
mise run build
./.output/okf-storage mcp
```

## Project Structure

```text
.
├── cmd/
│   ├── root.go          # root command and logger setup
│   └── mcpcmd/
│       ├── mcp.go       # `mcp` command and HTTP bootstrap
│       ├── server.go    # MCP server construction
│       └── tools.go     # MCP tool definitions
├── internal/
│   ├── bundle/          # knowledge bundle: file store and frontmatter catalog
│   └── okf/             # OKF document and frontmatter parser
├── main.go
├── go.mod
└── go.sum
```

## Dependencies

- [`github.com/modelcontextprotocol/go-sdk`](https://github.com/modelcontextprotocol/go-sdk)
- [`github.com/spf13/cobra`](https://github.com/spf13/cobra)
- [`github.com/lmittmann/tint`](https://github.com/lmittmann/tint)
