# okf-storage

OKF storage CLI with an MCP (Model Context Protocol) server served over stdio or HTTP.
It manages a knowledge bundle: a directory of markdown documents in the
[Open Knowledge Format](https://github.com/GoogleCloudPlatform/knowledge-catalog/blob/main/okf/SPEC.md) (OKF v0.2).

## Installation

With [mise](https://mise.jdx.dev):

```bash
mise use -g github:yz4230/okf-storage@latest
```

With Go 1.27.4 or later:

```bash
go install github.com/yz4230/okf-storage@latest
```

Prebuilt archives for Linux, macOS and Windows are also available on
[GitHub Releases](https://github.com/yz4230/okf-storage/releases).

## Usage

The bundle lives in `~/.okf-storage/bundle` by default; use `--dir` to choose
another directory (a leading `~` is expanded).

### MCP server (stdio)

```bash
okf-storage mcp --stdio
```

`--stdio` serves the MCP server over stdin/stdout, for clients that launch it as a
local subprocess. No authentication is used. Logs go to stderr, and the server exits
when the client closes stdin.

#### Add to Claude Code

```bash
claude mcp add okf -s user -- okf-storage mcp --stdio
```

#### Add to Claude Desktop

Add the server to `claude_desktop_config.json` (Settings → Developer → Edit Config)
and restart Claude Desktop:

```json
{
  "mcpServers": {
    "okf": {
      "command": "/absolute/path/to/okf-storage",
      "args": ["mcp", "--stdio"]
    }
  }
}
```

Claude Desktop does not read your shell's `PATH`, so give the absolute path that
`which okf-storage` prints (with mise, `mise which okf-storage`).

### MCP server (HTTP)

```bash
OKF_STORAGE_PASSWORD=$(openssl rand -hex 16) okf-storage mcp --public-url https://okf.example.com
```

The server listens on `localhost:8080` by default (`--addr` to change it) and exposes
the MCP endpoint at `/mcp`.

Setting `OKF_STORAGE_PASSWORD` puts the server behind OAuth 2.1
([mcpauth](https://github.com/yz4230/mcpauth)): clients register themselves
(Dynamic Client Registration), send you to a login page, and get an access token
once you enter the password. `--public-url` is required with it and must be the
`https` base URL clients reach the server at (`http` only for localhost); behind a
TLS-terminating proxy, keep listening on plain HTTP. Registered clients and token
hashes are kept in `.okf-oauth.json` at the bundle root, so sessions survive
restarts. `GET /dump` needs the same token as `/mcp`.

Only ChatGPT's and Claude's redirect URIs are accepted at registration by default.
To allow another client, add its redirect URI with `--redirect-uri` (repeatable);
rejected registrations are logged with the URIs the client sent.

Set `OKF_STORAGE_TOKEN` to also accept a static token in `Authorization: Bearer <token>`,
for clients that cannot go through OAuth (scripts, `curl`). It works with or without
OAuth; the token is only accepted in the header:

```bash
claude mcp add okf -s user --transport http https://okf.example.com/mcp \
  --header "Authorization: Bearer $OKF_STORAGE_TOKEN"
curl -fOJ -H "Authorization: Bearer $OKF_STORAGE_TOKEN" https://okf.example.com/dump
```

When neither variable is set, the server accepts unauthenticated requests, so always set one when the server is reachable from the network.

### Web viewer

`view` serves a read-only web UI for browsing the bundle, on `localhost:8081` by default.
It takes the same `--dir` as `mcp`; `--addr` changes the listen address and `--open` opens
the default browser:

```bash
okf-storage view --open
```

There is no editing and no authentication, so keep `--addr` on localhost.

## Tools

Paths are relative to the bundle root and may start with `/`, matching
bundle-relative links: `/metrics/revenue.md` and `metrics/revenue.md` name the
same document, and `/` is the root. Results always use the form without `/`.

| Tool                 | Description                                                           |
| -------------------- | --------------------------------------------------------------------- |
| `read`               | Read a document                                                       |
| `write`              | Create a document, or overwrite one whole (no append or merge)        |
| `edit`               | Replace an exact string in a document (`replace_all` for every match) |
| `delete`             | Delete a document; directories left empty are removed                 |
| `move`               | Move or rename a document; fails if the new path already exists       |
| `list`               | List the entries directly under a directory (directories end in `/`)  |
| `search_frontmatter` | Find documents whose frontmatter matches every field in `filter`      |
| `search_content`     | Find documents whose body matches the regular expression in `query`   |
