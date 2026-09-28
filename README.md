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

### Dump

`GET /dump` downloads every document in the bundle as a `tar.gz` archive, with paths relative to the bundle root.
It requires the same token as the MCP endpoint (`Authorization: Bearer <token>`, or `/dump/<token>`):

```bash
curl -fOJ -H "Authorization: Bearer $OKF_STORAGE_TOKEN" http://localhost:8080/dump
```

## Docker

The image runs `mcp --addr :8080 --dir /data` as a non-root user:

```bash
docker build -t okf-storage .
docker run --rm -p 8080:8080 -v okf-data:/data -e OKF_STORAGE_TOKEN=... okf-storage
```

Run one server per bundle directory: writes are serialized within a single
process only, and nothing stops a second server on the same directory. On
Kubernetes, use `replicas: 1` with the `Recreate` deployment strategy.

## Tools

| Tool                 | Description                                                           |
| -------------------- | --------------------------------------------------------------------- |
| `read`               | Read a document                                                       |
| `write`              | Create a document, or overwrite one whole (no append or merge)        |
| `edit`               | Replace an exact string in a document (`replace_all` for every match) |
| `delete`             | Delete a document; directories left empty are removed                 |
| `move`               | Move or rename a document; fails if the new path already exists       |
| `list`               | List the entries directly under a directory (directories end in `/`)  |
| `search_frontmatter` | Find documents whose frontmatter matches every field in `filter`      |
| `search_content`     | Find documents whose body matches a regular expression                |

## Resources

| URI                  | Description                                                                  |
| -------------------- | ---------------------------------------------------------------------------- |
| `okf://guide`        | Guide for agents on structuring, writing, linking and maintaining documents |
| `okf://spec`         | The full OKF v0.2 specification                                              |

The server also sends instructions on initialization (`cmd/mcpcmd/instructions.md`)
telling agents to search the bundle before answering and to record durable knowledge
in it on their own initiative; clients that support MCP server instructions add them
to the model's context. Each initialization appends an overview of the bundle's
current contents (`cmd/mcpcmd/overview.go`): the document count, the root `index.md`
(or the top-level entries when there is none) and the `type` and `tags` values in use,
so agents can search with exact values from the start.

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
│       ├── dump.go      # GET /dump tar.gz download
│       ├── instructions.md # server instructions sent on initialization
│       ├── overview.go  # bundle overview appended to the instructions
│       ├── guide.md     # agent guide served as okf://guide
│       └── spec.md      # OKF v0.2 specification served as okf://spec
├── internal/
│   ├── localbundle/     # knowledge bundle on the local filesystem
│   └── okf/             # OKF document and frontmatter parser
├── main.go
├── go.mod
└── go.sum
```

## Dependencies

- [`github.com/modelcontextprotocol/go-sdk`](https://github.com/modelcontextprotocol/go-sdk)
- [`github.com/spf13/cobra`](https://github.com/spf13/cobra)
- [`github.com/lmittmann/tint`](https://github.com/lmittmann/tint)
