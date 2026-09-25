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

Enable debug logging with the global `--verbose` / `-v` flag:

```bash
go run . -v mcp
```

The server shuts down gracefully on `SIGINT` / `SIGTERM`.

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
│       └── server.go    # MCP server construction (register tools here)
├── internal/
│   └── storage/         # storage and index interfaces
├── main.go
├── go.mod
└── go.sum
```

## Dependencies

- [`github.com/modelcontextprotocol/go-sdk`](https://github.com/modelcontextprotocol/go-sdk)
- [`github.com/spf13/cobra`](https://github.com/spf13/cobra)
- [`github.com/lmittmann/tint`](https://github.com/lmittmann/tint)
