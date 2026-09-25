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
- `cmd/mcpcmd/` — `mcp` command. `mcp.go` opens a `DirStore`, builds a `MemCatalog`, runs `bundle.Reindex` at startup, then serves. `server.go` (`newServer`) is where tools and resources get registered; `tools.go` maps each MCP tool to one `bundle.Bundle` method; `resources.go` serves the embedded `guide.md` (`okf://guide`), `spec.md` (`okf://spec`, vendored unmodified from upstream — do not edit), and documents via `okf://docs/{+path}`.
- `internal/bundle/` — the core. `Bundle` combines two pluggable interfaces:
  - `Store` (`store.go`): raw files keyed by slash-separated path. Is the source of truth. Every read returns an opaque `Version` (ETag-like; `DirStore` uses SHA-256 of content), and `Write` takes an `ifMatch` version for optimistic concurrency (`ErrConflict`). `DirStore` confines access with `os.Root`; only `.md` files count as documents and hidden dirs are skipped.
  - `Catalog` (`catalog.go`): path → frontmatter index for `Search`. The filter semantics (literal top-level keys, type-strict equality with numbers compared by value, list-contains) are specified on the interface and enforced by the shared conformance suite `catalogtest.Run` — any new Catalog implementation must pass it.
- `internal/okf/` — `ParseDocument` splits frontmatter/body. A document without a leading `---` has nil frontmatter (e.g. reserved `index.md`/`log.md`) and gets no catalog entry.

### Consistency model (read before touching `bundle.go`)

The catalog is kept in sync with the store **without a lock**, so multiple processes can share one store and catalog. Writes always parse first (reject invalid docs before touching the store), write to the store, then call `syncEntry`, which puts the catalog entry and re-checks `store.Stat` — re-deriving until the version it indexed matches what the store holds. `Edit` does read → replace → `Write(ifMatch)` and retries on `ErrConflict` up to `maxEditAttempts`. `Reindex` repairs catalogs after crashes and populates non-persistent ones. Preserve these invariants when adding operations.

### Listings and pagination

`Tree` and `Search` return a `Page` of paths sorted in **byte order** with keyset pagination (`PageRequest.After` = last path seen, `Page.Next`), not offsets. MCP tools default to 100 and cap at 1000 per page (`tools.go`). `List` is unpaginated, `ls`-style.

## Conventions

- Tests: MCP tools are tested end-to-end through `mcp.NewInMemoryTransports` against a `DirStore` in `t.TempDir()` (see `connect` in `tools_test.go`).
- Commits use Conventional Commits with a scope, e.g. `feat(bundle): ...`, `refactor(mcp): ...`.
- Keep the README's tool/resource tables in sync when adding or changing MCP tools or resources.
