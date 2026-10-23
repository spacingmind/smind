# `smind task tree` — task hierarchy in the CLI

Size: M (3) — scope 0, PRs 0, risk 1, decisions 0, verify 1

Today the CLI has only `task ls` (flat, PARENT column). The daemon already
serves `task.tree` (internal/wsapi/task_tree.go; MCP `task_tree`). This adds
`smind task tree` that prints it as an indented tree.

## Decisions

- Ids: `smind task tree <workspaceId>` (all roots) or
  `smind task tree --task <taskId>` (that subtree). A positional id would be
  ambiguous (workspace and task ids share a number space), and `task ls`
  already uses flags (`--parent`) for task ids. Exactly one of the two.
- `--json` prints `{"nodes":[...],"truncated":bool}` as the daemon sent it.
- No `--watch`.
- Header counts come from the returned nodes' own run state
  (running / waiting / failed / done), not the per-node descendant counts.

## Acceptance criteria

1. One line per task, indented by `depth`: id, title (truncated to 48 runes
   with "…"), tier (`supervisor`/`leader`/`coder`, or `-`), run state.
2. A failed run with failureKind `rate_limited` renders
   `error: <rateLimitedText>` (reuses `rateLimitedText`); another failureKind
   renders `error: <kind>`.
3. Pending permissions render `N permission(s)` only when > 0.
4. Subtree cost renders `$0.42` when `usage.subtree.costUsd` is set, `cost n/a`
   when null.
5. A header line with counts: `N tasks: R running, W waiting, F failed, D done`.
6. `--task` and workspace-id are mutually exclusive and one is required; bad
   input → usage on stderr, exit 2. `--json` works with both.
7. Truncated trees print a trailing note.

## Test scenarios

- `TestRenderTaskTree` (table): nesting/indent, failure hint (rate limited and
  other kind), cost n/a vs `$0.42`, title truncation, permissions, tier `-`,
  truncated note, header counts.
- `TestTaskTreeArgs`: arg validation (none, both, bad ids, unknown flag).
- `TestTaskTreeCLI_EndToEnd`: real daemon harness (as TestTaskNewLs_ParentFlag):
  nest a parent/child, `task tree <ws>`, `task tree --task <id>`, `--json`.

## Progress / Validation

(filled in during review)
