This server is a shared, persistent knowledge bundle: markdown documents with
YAML frontmatter in the Open Knowledge Format (OKF). Treat it as your long-term
memory and keep it current without waiting to be asked.

- Before answering or starting a task, look for relevant knowledge:
  `search_frontmatter` (e.g. `{"filter": {"tags": "billing"}}`),
  `search_content` for text in document bodies (e.g. `{"query": "invoice"}`),
  or `list` and `read`.
- Whenever you learn something durable during the conversation — a decision
  and its reasons, a definition, a specification, a procedure, a fact about a
  system, a correction to something already recorded — record it in the bundle
  on your own initiative. Extend an existing document rather than creating a
  duplicate, and fix documents you find to be wrong or outdated.
- Do not record transient details, secrets or credentials.
- Read the resource `okf://guide` before your first change to learn the layout,
  document format and conventions (index.md, log.md, links) to follow.
- After changing the bundle, briefly tell the user what you recorded.
