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

Use `--path` to serve the MCP endpoint at a different path (default: `/mcp`).

Set `OKF_STORAGE_TOKEN` to require `Authorization: Bearer <token>` on every request.
Clients that cannot send headers (e.g. ChatGPT connectors) can instead put the token in the URL, `/mcp/<token>`; it is only checked when the request has no `Authorization` header.
When the variable is unset, the server accepts unauthenticated requests, so always set it when the server is reachable from the network:

```bash
OKF_STORAGE_TOKEN=$(openssl rand -hex 32) go run . mcp
```

The server shuts down gracefully on `SIGINT` / `SIGTERM`.

## Docker

The image runs `mcp --addr :8080 --dir /data` as a non-root user:

```bash
docker build -t okf-storage .
docker run --rm -p 8080:8080 -v okf-data:/data -e OKF_STORAGE_TOKEN=... okf-storage
```

## Tools

| Tool     | Description                                                           |
| -------- | --------------------------------------------------------------------- |
| `read`   | Read a document                                                       |
| `write`  | Create or overwrite a document                                        |
| `edit`   | Replace an exact string in a document (`replace_all` for every match) |
| `delete` | Delete a document                                                     |
| `move`   | Move or rename a document; fails if the new path already exists       |
| `list`   | List documents and subdirectories directly under a directory          |
| `tree`   | List document paths under a directory recursively (optional `depth`)  |
| `search` | Find documents whose frontmatter matches every field in `filter`      |

## Resources

| URI                  | Description                                                                  |
| -------------------- | ---------------------------------------------------------------------------- |
| `okf://guide`        | Guide for agents on structuring, writing, linking and maintaining documents |
| `okf://spec`         | The full OKF v0.2 specification                                              |
| `okf://docs/{+path}` | A document in the bundle by path, e.g. `okf://docs/metrics/revenue.md`       |

The server also sends instructions on initialization (`cmd/mcpcmd/instructions.md`)
telling agents to search the bundle before answering and to record durable knowledge
in it on their own initiative; clients that support MCP server instructions add them
to the model's context.

The guide (`cmd/mcpcmd/guide.md`) and the specification (`cmd/mcpcmd/spec.md`) are embedded in the binary.
The specification is copied unmodified from
[GoogleCloudPlatform/knowledge-catalog](https://github.com/GoogleCloudPlatform/knowledge-catalog/blob/main/okf/SPEC.md)
and is licensed under the Apache License 2.0 (`cmd/mcpcmd/spec.LICENSE.md`).

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
│       ├── tools.go     # MCP tool definitions
│       ├── resources.go # MCP resource definitions
│       ├── instructions.md # server instructions sent on initialization
│       ├── guide.md     # agent guide served as okf://guide
│       └── spec.md      # OKF v0.2 specification served as okf://spec
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
