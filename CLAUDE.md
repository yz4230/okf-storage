# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

A Go CLI (`okf-storage`) whose `mcp` subcommand serves an MCP server over Streamable HTTP (`/mcp`, default `localhost:8080`), or over stdin/stdout with `--stdio` (unauthenticated, for local use). The server manages a "knowledge bundle": a directory of markdown documents in the Open Knowledge Format (OKF v0.2), searchable by their YAML frontmatter. Requires Go 1.27 (uses `os.Root`, `new(expr)`, `strings.Lines`, `http.NewCrossOriginProtection`).

## Commands

```bash
go run . mcp --dir ./knowledge        # run the server (-v for debug logs, --addr to change listen address)
go run . mcp --stdio --dir ./knowledge  # run over stdio (no auth)
mise run build                        # build to dist/okf-storage
mise run tag <major|minor|patch>      # (or M|m|p) tag and push the next version; the tag triggers the GoReleaser release workflow
go test ./...                         # all tests
go test ./internal/bundle -run TestName/subtest   # single test
go vet ./...
```

## Architecture

Layers, top to bottom:

- `cmd/root.go` — cobra root; sets up the `tint` slog logger (`-v` → debug).
- `cmd/mcpcmd/` — `mcp` command. `mcp.go` opens a `LocalBundle` and serves over HTTP (`serve`) or stdio (`serveStdio`). `server.go` defines the `bundle` interface the server consumes and `newServer`, where tools and resources get registered; `tools.go` maps each MCP tool to one `bundle` method; `resources.go` serves the embedded `guide.md` (`okf://guide`), and `spec.md` (`okf://spec`, vendored unmodified from upstream — do not edit); documents are read through the `read` tool only.
- `internal/localbundle/` — the core. `LocalBundle` works directly on a directory through `os.Root`, with no index: searches and `Dump` walk every `.md` file (skipping hidden files and directories, which `List` also leaves out and every path-taking method rejects with `ErrHiddenPath`) and parse it on each call (sequentially, results sorted in byte order, unparsable documents logged and skipped). Every operation locks `.okf.lock` at the bundle root (`gofrs/flock`; shared for reads, exclusive for writes, retrying with git-style backoff for up to 10s, released by the kernel if the process dies). It opens the lock file anew each time, so the one lock orders goroutines and processes alike. Writes parse first, rejecting invalid docs before touching the file, then replace it atomically through `renameio` with `WithRoot` (temp file `.<name><random>` at the bundle root; plain `os.Root.WriteFile` on Windows, which renameio does not support). `Delete` and `Move` remove directories they leave empty. Errors carry paths relative to the bundle root.
- `internal/okf/` — `ParseDocument` splits frontmatter/body. A document without a leading `---` has nil frontmatter (e.g. reserved `index.md`/`log.md`) and never matches `SearchFrontmatter`.

Several processes may share a bundle; the lock file needs a local file system (not NFS). Listings and searches are unpaginated.

## Conventions

- Tests: MCP tools are tested end-to-end through `mcp.NewInMemoryTransports` against a `LocalBundle` in `t.TempDir()` (see `connect` in `tools_test.go`).
- Commits use Conventional Commits with a scope, e.g. `feat(bundle): ...`, `refactor(mcp): ...`.
- Keep the README's tool/resource tables in sync when adding or changing MCP tools or resources.
