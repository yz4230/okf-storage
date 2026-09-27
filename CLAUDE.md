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
- `cmd/mcpcmd/` — `mcp` command. `mcp.go` opens a `DirStore`, builds a `MemCatalog`, runs `bundle.Reindex` at startup, then serves. `server.go` (`newServer`) is where tools and resources get registered; `tools.go` maps each MCP tool to one `bundle.Bundle` method; `resources.go` serves the embedded `guide.md` (`okf://guide`), and `spec.md` (`okf://spec`, vendored unmodified from upstream — do not edit); documents are read through the `read` tool only.
- `internal/bundle/` — the core. `Bundle` combines two pluggable interfaces:
  - `Store` (`store.go`): raw files keyed by slash-separated path. Is the source of truth. `DirStore` confines access with `os.Root`; only `.md` files count as documents and hidden dirs are skipped. Its `RWMutex` serializes writes and makes `Read` wait for an in-progress write, so readers never see partial content; and `OpenDir` holds a non-blocking `flock` on the directory until `Close`, so a second process on the same bundle fails to start.
  - `Catalog` (`catalog.go`): path → frontmatter index for `Search`. The filter semantics (literal top-level keys, type-strict equality with numbers compared by value, list-contains) are specified on the interface and enforced by the shared conformance suite `catalogtest.Run` — any new Catalog implementation must pass it.
- `internal/okf/` — `ParseDocument` splits frontmatter/body. A document without a leading `---` has nil frontmatter (e.g. reserved `index.md`/`log.md`) and gets no catalog entry.

### Consistency model (read before touching `bundle.go`)

**One process owns a bundle.** The target is small self-hosting (local filesystem + in-memory catalog), so `Bundle` assumes it is the only writer to its store and catalog: `Bundle.mu` is held across each store change and the catalog update that follows, which keeps them in step (`Edit` does read → replace → write under it, so it cannot lose a concurrent edit). Readers take no `Bundle` lock. Writes always parse first, rejecting invalid docs before touching the store. `Reindex` populates the catalog at startup (and drops entries of missing documents, for persistent catalogs). Deploy with one replica and a `Recreate` strategy; supporting several processes would need a shared catalog and the lock-free sync that `a521574` introduced and this design removed.

### Listings and pagination

`Tree` and `Search` return a `Page` of paths sorted in **byte order** with keyset pagination (`PageRequest.After` = last path seen, `Page.Next`), not offsets. MCP tools default to 100 and cap at 1000 per page (`tools.go`). `List` is unpaginated, `ls`-style.

## Conventions

- Tests: MCP tools are tested end-to-end through `mcp.NewInMemoryTransports` against a `DirStore` in `t.TempDir()` (see `connect` in `tools_test.go`).
- Commits use Conventional Commits with a scope, e.g. `feat(bundle): ...`, `refactor(mcp): ...`.
- Keep the README's tool/resource tables in sync when adding or changing MCP tools or resources.
