# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

A Go CLI (`okf-storage`) whose `mcp` subcommand serves an MCP server over Streamable HTTP (`/mcp`, default `localhost:8080`). The server manages a "knowledge bundle": a directory of markdown documents in the Open Knowledge Format (OKF v0.2), searchable by their YAML frontmatter. Requires Go 1.27 (uses `os.Root`, `new(expr)`, `strings.Lines`, `http.NewCrossOriginProtection`).

## Commands

```bash
go run . mcp --dir ./knowledge        # run the server (-v for debug logs, --addr to change listen address)
mise run build                        # build to .output/okf-storage
go test ./...                         # all tests
go test ./internal/bundle -run TestName/subtest   # single test
go vet ./...
```

## Architecture

Layers, top to bottom:

- `cmd/root.go` — cobra root; sets up the `tint` slog logger (`-v` → debug).
- `cmd/mcpcmd/` — `mcp` command. `mcp.go` opens a `LocalBundle` and serves. `server.go` defines the `bundle` interface the server consumes and `newServer`, where tools and resources get registered; `tools.go` maps each MCP tool to one `bundle` method; `resources.go` serves the embedded `guide.md` (`okf://guide`), and `spec.md` (`okf://spec`, vendored unmodified from upstream — do not edit); documents are read through the `read` tool only.
- `internal/localbundle/` — the core. `LocalBundle` works directly on a directory through `os.Root`, with no index: searches and `Dump` walk every `.md` file (skipping hidden files and directories, which `List` also leaves out and every path-taking method rejects with `ErrHiddenPath`) and parse it on each call (in parallel, results sorted in byte order, unparsable documents logged and skipped). Its `RWMutex` serializes writes and makes readers wait for an in-progress write. Writes parse first, rejecting invalid docs before touching the file. `Delete` and `Move` remove directories they leave empty. Errors carry paths relative to the bundle root.
- `internal/okf/` — `ParseDocument` splits frontmatter/body. A document without a leading `---` has nil frontmatter (e.g. reserved `index.md`/`log.md`) and never matches `SearchFrontmatter`.

**One process owns a bundle.** Locking is in-process only and nothing prevents a second process on the same directory, so deploy with one replica and a `Recreate` strategy. Listings and searches are unpaginated.

## Conventions

- Tests: MCP tools are tested end-to-end through `mcp.NewInMemoryTransports` against a `LocalBundle` in `t.TempDir()` (see `connect` in `tools_test.go`).
- Commits use Conventional Commits with a scope, e.g. `feat(bundle): ...`, `refactor(mcp): ...`.
- Keep the README's tool/resource tables in sync when adding or changing MCP tools or resources.
