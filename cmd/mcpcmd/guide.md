# Organizing knowledge in an OKF bundle

This server stores a knowledge bundle in the Open Knowledge Format (OKF v0.2):
a directory tree of markdown files with YAML frontmatter. This guide tells an
agent how to add to it and keep it organized with the server's tools. The full
specification is the resource `okf://spec`; read it for details this guide
leaves out, such as `Attested Computation` concepts and conformance rules.

## The bundle

- A **concept** is one unit of knowledge (a table, an API, a metric, a
  playbook, a decision) stored as one `.md` file. Its **concept ID** is its path
  without `.md`, e.g. `tables/orders`.
- Directories group concepts. Organize them however suits the domain; a common
  layout groups by kind:

  ```
  index.md              # what is in this directory
  log.md                # history of changes to this directory
  tables/orders.md
  metrics/revenue.md
  playbooks/freshness-alert.md
  references/event-parameters.md
  ```

- `index.md` and `log.md` are **reserved** at every level and must not be used
  for concepts. Every other `.md` file is a concept.
- Name files in lowercase kebab-case after the thing they describe
  (`customer-orders.md`, not `doc1.md` or `CustomerOrders.md`).

## Workflow

1. **Orient.** Read the root `index.md` if it exists, then `list` the
   directories relevant to your task. Use `search_frontmatter` to find
   concepts by frontmatter, e.g. `{"type": "Metric"}` or `{"tags": "revenue"}`,
   and `search_content` to find text in their bodies.
2. **Check before creating.** Search for an existing concept about the same
   thing (by `type`, `tags`, `resource`, likely file names). If one exists,
   extend it instead of creating a duplicate.
3. **Reuse the vocabulary.** Before choosing a `type` or a tag, look at the
   values already used by neighbouring concepts and reuse them verbatim.
   `Metric` and `metric` are different values to `search_frontmatter`.
4. **Write** the concept (see the document format below).
5. **Link** it: add links from the concepts that relate to it, and from it to
   them.
6. **Update `index.md`** of the directory you wrote to, and of its parent when
   you created a new directory.
7. **Append to `log.md`** of the directory you changed.

## Document format

```markdown
---
type: Metric
title: Revenue
description: Recognized revenue for a fiscal year, per Finance's definition.
tags: [finance, revenue]
status: stable
generated: { by: claude/claude-opus-5-5, at: 2026-09-26T12:00:00Z }
sources:
  - id: rev-policy
    resource: https://wiki.example.com/finance/revenue-recognition
    title: Revenue recognition policy
---

# Definition

Recognized revenue sums `amount` over rows of the
[recognized revenue table](/tables/recognized-revenue.md) booked to the fiscal
year.[^rev-policy]

[^rev-policy]: Revenue recognition policy
```

### Frontmatter

| Key           | Meaning                                                                 |
| ------------- | ----------------------------------------------------------------------- |
| `type`        | **Required.** Short kind of concept: `Metric`, `Playbook`, `API Endpoint`, `Reference`, ... |
| `title`       | Human-readable name.                                                    |
| `description` | **One sentence** summarizing the concept. Reused in `index.md`.         |
| `resource`    | URI of the underlying asset, if the concept describes one.              |
| `tags`        | YAML list of short, lowercase cross-cutting labels.                     |
| `sources`     | Materials the content derives from; each entry needs `resource`, and should have `id` and `title`. |
| `generated`   | `{ by, at }`: who wrote the current content and when.                   |
| `verified`    | List of `{ by, at }`: who confirmed the content against its sources.    |
| `status`      | `draft`, `stable` (default) or `deprecated`.                            |
| `stale_after` | Instant after which the content should be rechecked.                    |

Any other key may be added; keep keys that you do not recognize.

- **Actors** in `generated.by` and `verified[].by` are `<producer>/<version>`
  for agents (e.g. `claude/claude-opus-5-5`), `human:<id>` for people and
  `process:<id>` for automated jobs. Only record `human:` when a person
  actually wrote or confirmed the content.
- **Timestamps** are ISO 8601 with a UTC offset: `2026-09-26T12:00:00Z`.
- Set `generated` whenever you write or meaningfully change content. Do not
  add yourself to `verified` unless you actually checked the content against
  its sources.
- Record provenance in `sources`, never in a `# Citations` body section.
  Attribute individual claims with a footnote whose label is a `sources[].id`.
  Never invent a source.
- Mark unfinished concepts `status: draft`.

### Body

Prefer structure (headings, lists, tables, fenced code) over long prose. These
headings have conventional meaning; use them when they apply:

- `# Schema`: columns or fields of an asset.
- `# Examples`: concrete usage, usually fenced code.
- `# Computation`: the sanctioned computation of an `Attested Computation`.

Do not put preamble, reasoning or apologies in a document.

### Links

Link concepts with ordinary markdown links. Prefer bundle-relative links
starting with `/`, which survive moving the linking document:
`[orders](/tables/orders.md)`. Tools accept such a link target as a path
as is. The kind of relationship (joins with, depends on, replaces) goes in
the surrounding prose. Link the first mention per
section; do not link a concept to itself. A link to a concept that does not
exist yet is allowed and marks knowledge still to be written.

## Index and log files

`index.md` has **no frontmatter** (except `okf_version: "0.2"` in the root
index) and lists the directory's contents in sections:

```markdown
# Tables

* [Customer orders](tables/orders.md) - One row per completed customer order.

# Directories

* [Playbooks](playbooks/) - Runbooks for on-call incidents.
```

Use each concept's `description` as its entry text, and keep entries in sync
when titles or descriptions change.

`log.md` lists changes newest first under ISO 8601 date headings:

```markdown
# Update Log

## 2026-09-26
* **Creation**: Added [Revenue](/metrics/revenue.md).
* **Update**: Documented the `refunds` column of [orders](/tables/orders.md).
```

Conventional leading words are `**Creation**`, `**Update**`,
`**Deprecation**` and `**Deletion**`.

## Keeping the bundle healthy

- **One concept per file.** Split a document that covers several independent
  things; merge documents that describe the same thing.
- **Refine rather than rewrite.** When updating a concept, keep every existing
  frontmatter key and every top-level heading. Take the union of `tags` and of
  `sources`. Add new sections after existing ones.
- **Deprecate instead of deleting** a concept that others link to: set
  `status: deprecated` and link to its replacement. Delete only documents that
  are wrong, duplicated or unreferenced, and fix the links and index entries
  that pointed at them.
- **Moving** a concept: `move` it to the new path, then update every link and
  index entry that referred to the old path.
- Set `stale_after` on facts that are known to expire, and recheck concepts
  whose `stale_after` has passed.

## Tool notes

- `write` creates a document or replaces the whole existing one, frontmatter
  included; it never appends or merges. Read it first and carry over
  everything you want to keep.
- `edit` replaces an exact string; prefer it for small changes to large
  documents.
- `search_frontmatter` sees only documents with frontmatter and matches
  values exactly: a list field such as `tags` matches if it contains the
  value, and `2` does not match `"2"`.
- `search_content` takes a regular expression (RE2 syntax) and searches
  document bodies only, not frontmatter.
