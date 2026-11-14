# Saved grants: "allow always in this project" (ADR-0055 section 8, decision D6; agent-questions PR4)

Status: active (2026-11-15). D6 is accepted (a daemon-owned `permission_grants` table, 30-day default expiry, revocable, never for floor, destructive, network, unparsed commands or questions).
Size: L (permissions, a human-only table, storage, wire), so split into two PRs, one at a time, each with a validation contract (ADR-0045) and a foreground two-axis review:

- **PR4a (this plan's first PR): the daemon.** Store table and audit, the grantable classification, evaluation in the permission path, `run.answerPermission` with `scope: project`, list and revoke RPCs, the human-only tables, the CLI.
- **PR4b: the surfaces.** The scoped control on the web and mobile permission cards (showing the exact scope), the web and mobile list and revoke screens, and the mobile question-card polish the Supervisor listed. The demo permission card stays without the control unless the scripted demo daemon supports it.

## Checked against origin/develop (what already exists, dropped from this work)

The name `run.answerPermission` is already reserved in `permpolicy.HumanOnlyRPCs` and `PendingHumanOnlyRPCs`; `smind permission ...` is already a human-only CLI group. Nothing else exists: no table, no offer on the card, no list. `allow_always` options exist only as the provider's own ACP option, and are stripped for git rules.

## Decisions

- **G1 scope = project + exact command.** A grant is keyed by `workspace_id` (the project) and the SHA-256 of the exact RAW command the engine judged (`PermissionDetail.PolicyCommand`, never the elided form), plus its byte length. No glob, no prefix, no argv0 widening, no "any arguments". The folder the card shows is the workspace's folder. A different command, a different project or a different byte is a different request and asks again.
- **G2 what may be offered and honoured.** All of: an `execute` request with a raw command of at most 2000 bytes and no elided heredoc; the policy engine is on and returned `ask` for a plain reason (no rule matched, or a segment not covered), flagged by a new `Decision.Grantable` set only at those two exits and only when every segment parsed and none holds a dynamic word; no `GitRule`; a known task worktree; an attended run (`purpose == ""`, no seat, origin not remote, not AutoAccept); and no segment whose program is on the never-grant list (network clients, destructive programs, privilege and wrapper programs, package-manager install verbs, the human-only `smind` commands). Everything else is asked every time, and the card does not offer the control. **Questions never.** The same predicate runs at offer time and at use time, so a command that stopped being grantable (the policy file changed, the list grew) asks again.
- **G3 expiry and revoke.** 30 days from creation (one constant), no renewal on use. A revoked or expired grant is never applied. Revoke takes effect for the next request.
- **G4 voided by an MCP config change.** The grant stores a fingerprint of the project's enabled MCP servers (id, name, transport, command, args, url, enabled, updated_at; never env or headers). A different fingerprint at use time makes the grant void: not applied, still listed as `void`, revocable.
- **G5 human only.** A grant is created only by `run.answerPermission` with `scope: "project"`, only for a pending request that carried the offer, only with that request's `allow_once` option. Listing is read-only; revoke is human-only. All three method names are on `permpolicy.HumanOnlyRPCs` (so the floor denies a shell command that names one next to a transport) and none is in `RunScopedMethods` or the MCP tool list. The CLI is under the already human-only `smind permission` group. The known limit stays (ADR-0056): an unidentified local connection counts as the human.
- **G6 audit.** Every creation, use, revocation and refusal-to-apply (expired, void, revoked is silent only because the row is gone from matching) writes a row in `permission_grant_audit`; a use also records the request and its resolution `grant` with the grant id in run history. The engine still audits the ask it escalated.
- **G7 order.** floor -> policy deny/ask rules -> (engine `ask` with `Grantable`) -> grant -> human. A grant can only turn a grantable `ask` into `allow_once`; it never overrides a floor or a rule deny, never applies under AutoAccept, a seat, a system run or a remote run, and never picks an `always` option.
- **G8 never-grant list is a deny-list (the open choice).** The never-grant list is a conservative deny-list of programs. An allow-list (grant only known local build and test programs) is stricter and fails closed on programs nobody listed, at the cost of a feature that offers less. Recommendation: ship the deny-list now, with the structural limits above (exact string, no dynamic words, no heredoc, no network or wrapper program), and move to an allow-list if the Human prefers; the engine, the table and the surfaces do not change.

## Acceptance criteria

- AC1 A `permission_grants` row exists only after a human answered a pending, grantable request with `scope: project`; an agent run (identified connection) cannot create or revoke one; the three method names are in the human-only table and in no agent-reachable list.
- AC2 A saved grant is applied to the same project and the same exact raw command, and to nothing else (a trailing space, another project, another argument, a different casing ask again).
- AC3 Floor hits, rule denies, `GitRule` commands, heredoc commands, dynamic words, truncated or unparsed commands, network, destructive, privilege and wrapper programs, questions, AutoAccept, seats, system runs and remote runs never get the offer and never match a stored grant (the stored grant is ignored at use time even if one exists).
- AC4 A grant is not applied once expired, revoked, void (MCP fingerprint changed) or when the policy engine is off; the command is asked again, with the same card as before.
- AC5 Every creation, use and revoke is audited; a use appears in run history as resolution `grant` with the grant id.
- AC6 List shows scope (command as displayed, project, folder), creation time, expiry, uses, status (active, expired, void, revoked); revoke by id works from the RPC and the CLI.
- AC7 Additive wire: the offer is a new optional field of the permission detail; old clients ignore it and answer with the existing `run.respondPermission`.

## Test scenarios (PR4a)

`internal/permpolicy`: `TestGrantable_PlainAskOnly`, `TestGrantable_NeverGrantPrograms` (table: network, destructive, privilege, wrappers, installers, human-only smind commands), `TestGrantable_NotDynamicHeredocRedirectFloor`, `TestGrantable_EquivalentToDecideAsk` (for a corpus of commands: Grantable implies Decide returned ask with no floor id).
`internal/store`: `TestGrants_CreateListRevoke`, `TestGrants_ExpireAndVoidNotApplied`, `TestGrants_AuditRows`, `TestGrants_UniquePerProjectAndCommand`.
`internal/runs`: `TestGrants_OnlyHumanScopeProject`, `TestGrants_EvaluatedAfterFloorAndPolicy` (a floor hit and a rule deny with a stored grant still deny), `TestGrants_PolicyDenyBeatsGrant`, `TestGrants_NeverOfferedForFloorDestructiveNetworkUnparsedQuestion`, `TestGrants_MatchIsExactCommandAndProject`, `TestGrants_ExpireAndRevoke`, `TestGrants_VoidOnMcpChange`, `TestGrants_UseIsAudited`, `TestGrants_NotUnderAutoAcceptSeatSystemRemote`, `TestGrants_EngineOffAsksEveryTime`, `TestGrants_AskedAgainAfterEveryInvalidation` (the equivalence test: same raw command, five invalidations, each asks again), `TestGrants_AnswerOnlyAllowOnceOfAnOfferedRequest`.
`internal/wsapi`: `TestWsapi_GrantMethodsAreHumanOnly` (handler map, MCP tool list, `RunScopedMethods`, identified connection refused), `TestHumanOnlyRPCTable_MatchesHandlers` (the pending entry is gone), `TestWsapiMethodMapGolden`.
`cmd/smind`: `TestCLI_PermissionGrants*` (list, revoke, refused inside an agent run), `TestHumanOnly*` extended.

Mutations (committed code, one at a time): a grant evaluated before the engine -> `TestGrants_EvaluatedAfterFloorAndPolicy`; the project id left out of the key -> `TestGrants_MatchIsExactCommandAndProject`; the expiry check removed -> `TestGrants_ExpireAndRevoke`; the fingerprint check removed -> `TestGrants_VoidOnMcpChange`; a network program removed from the never-grant list -> `TestGrantable_NeverGrantPrograms`; `scope` honoured without the offer -> `TestGrants_AnswerOnlyAllowOnceOfAnOfferedRequest`; a handler dropped from the human-only table -> `TestWsapi_GrantMethodsAreHumanOnly`; the audit write removed -> `TestGrants_UseIsAudited`.

## Progress

- 2026-11-15: spec written; PR4a in progress.

## Validation

(to be filled when PR4a lands)
