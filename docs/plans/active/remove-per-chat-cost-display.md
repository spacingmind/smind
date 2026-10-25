# Remove per-chat cost display; label subscription cost in Usage

Size: M (5) — scope 2 (web + one additive daemon field), PRs 1, risk 0, decisions 1, verify 1 (screenshots)

Human request 2026-10-25: "Cost is not shown per chat" (see memory: cost = API-price estimate on a Team plan, show only in Settings → Usage). Cost stays in the API/CLI (`task runs`, run usage payloads).

## Findings (before code)

- Per-chat cost surfaces on web: `RunUsagePill` (run footer) and `ChatUsageTotal` (task-detail header). Both are `UsagePillCard`s that ALSO carry the ADR-0028 plan-usage section (`ChatPlanUsage`/`useChatPlanUsage`) for the chat's provider. Removing the pills removes that per-chat hover path to plan windows; Settings → Usage "Plan usage" block still shows them. Reported to the Human as a decision.
- `components/account-usage/*` shows plan windows only, no cost → kept. `ChatPlanUsage` + `useChatPlanUsage` + `UsagePillCard` become dead once the pills go; deleted if grep shows no other importer.
- Mobile: `mobile/src/taskTree.ts` `costLabel` / `TaskTreeRow` show a cost per row of the read-only tree (#594). That is the tree view, not a chat/run surface → kept (decision for the Human). No other cost/usage UI in task screen, chat header, run footer or task list.
- Spaces: a space belongs to a workspace (`spaces.workspace_id`), tasks carry `space_id`. `usage.summary` cannot group by space (`runUsageGroupColumns`), but the shared workspace tree (`useWorkspaceTree`) maps task → space, so Space is a client-side roll-up of the `task`-grouped rows. No daemon change for it.
- Account kind: `usage.summary` rows carry no provider/account. Only a Provider group-by key identifies the provider. So the label needs one additive field: runs-scope rows get `providers: string[]` (distinct `run_usage.provider`), no change to accounting. The client maps provider → subscription via the shared provider list (`kind === "cli"`: claude-native, codex-native use the CLI's own login; GLM and other API-key providers are metered).
  Mapping rule: a row is subscription if ANY provider in `row.providers` is `kind: "cli"`. Proxy rows have no cost, so no label.

## Acceptance criteria

1. Run footer renders no cost/usage pill for a run with usage.
2. Task detail renders no chat usage total.
3. The tree budget chip/popover is unchanged and still renders when a budget is set.
4. `RunUsagePill`, `ChatUsageTotal`, their tests and helpers only they used are deleted; nothing else imports them. API/CLI/RPC usage fields untouched.
5. Settings → Usage offers Group by "Space" (task rows rolled up by the task's space; tasks in no space or not in the tree → "No space"); sums skip null cost (all null → em dash); no layout shift when switching group-by.
6. A Usage row whose runs include a subscription provider shows `≈ API price` next to the cost, tooltip "Estimated at API list prices; subscription plans are not billed per token"; API-key rows show the plain cost; mixed rows show the label; rows with no cost show no label.
7. Mobile: nothing per-chat to remove (grep evidence in PR body).
8. `task lint`, `task test`, `go test ./...`, web tsc + vitest pass; Windows cross-compile passes.

## Test scenarios

- `run-footer: renders no usage pill or cost text for a run with usage` (asserts no `$`, no token count, no `run-usage-pill` testid)
- `task-detail: renders no chat usage total`
- `task-detail: tree budget chip still renders when a budget is set`
- `usage-section: Space group-by rolls task rows up by space` (2 spaces + a task in none)
- `usage-section: Space roll-up cost is em dash when no task reported cost`
- `usage-section: subscription provider row shows ≈ API price with tooltip`
- `usage-section: API-key provider row shows plain cost`
- `usage-section: mixed row shows the label`
- `usage-section: row without cost shows no label`
- Go: `SummarizeRunUsage returns distinct providers per group`, wsapi `usage.summary exposes providers on runs rows`

## Decisions

- Space = client roll-up of task rows (no daemon change).
- Additive `providers` on runs-scope `usage.summary` rows (not an accounting change).

## Progress

- [ ] A: web deletion (Peer 1)
- [ ] B: providers field + Usage label + Space group-by (Peer 2)
- [ ] Frames, PR

## Validation

(to fill)
