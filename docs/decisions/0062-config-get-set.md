# 0062: `config.get` and `config.set`: global switches from the UI, human-only, local-only, allowlisted

## Status

**Proposed — 2026-11-27.** Nothing is built for this record. The Human decided the direction on 2026-11-27 (H3): **yes** to a `config.get` / `config.set` pair, **ADR first**, human-only,
local-only (the relay and remote links refuse it), an allowlist of keys, and never an agent. What is still open is the allowlist itself and six smaller choices, collected in
[Questions for the Human](#questions-for-the-human); the table in [section 3](#3-the-allowlist) is a proposal, not a decision. When the Human accepts it, the amendment notes in
[section 4](#4-what-this-amends-and-the-exact-wording) are applied in the first implementation PR.

Builds on [ADR-0031](0031-homelab-cli-distribution.md) D10 (`smind config get|set|unset`, the comment-preserving edit this record reuses),
[ADR-0056](0056-human-only-rpc-principal.md) (who "the human" is on the socket), and the guards of [ADR-0042](0042-plugin-system.md) (plugins), [ADR-0044](0044-automations.md)
(automations), [ADR-0048](0048-fleet-connected-hosts.md) (fleet) and [ADR-0061](0061-browser-for-agents.md) (the browser kill switch).

## Context

**The global switches have no RPC, on purpose.** `plugins.enabled`, `automations.enabled` and `automations.paused`, `fleet.enabled`, `browser.disabled` and `learning.enabled` are
read from `config.yaml` (and for two of them an environment variable). Their doc comments say "no RPC, agent or relay session can set it". The intent was narrow and good: nothing that
reaches the daemon through a wire (an agent's MCP connection, a paired phone, a peer host, a plugin) should be able to switch on the things that spawn unattended agents, load plugin
code, dial other machines or turn off a kill switch. The only door was the file.

**What that costs today.** A person who wants automations, plugins or hosts has to leave the app, find `config.yaml`, edit it and (for hosts) restart. The UI shows a read-only alert that
says "edit config.yaml" (Automations, Plugins, Hosts) and nothing else. The CLI is not much better: `smind config set` accepts only `server.port`, `server.listen`,
`server.hostnames`, `server.passwordHash` (removal only), `automations.enabled` and `providers.<id>.defaultModel`, so `plugins.enabled`, `fleet.enabled` and `learning.enabled` can only be
hand-edited. The Human asked for the switches in Settings (the Settings audit of 2026-11-26, section 3) and agreed the direction above.

**What the on-purpose design never protected against.** An agent with a shell runs as the same OS user, can read `config.yaml` and run `smind config set` (the `config` command is not in
the human-only CLI table of `internal/permpolicy/humanonly.go`). ADR-0042 already says this plainly ("Honest limit"), and ADR-0056 carries the general argument. So the RPC-less rule
is a drift guard against the wire, not a boundary against a same-user process. That shapes this record: adding an RPC must not give any wire caller more than a person at the keyboard has,
and it should not pretend to close the shell hole it does not close.

What exists to build on:

- `config.Set(path, value)` (`internal/config/edit.go`): a node-level YAML edit (comments, key order and unrelated sections survive), validated per key type, refused if the resulting file
  would not load, written atomically. Its `knownKeys` table is already an allowlist. `config.Get` returns the stored value and says it does not apply environment overrides.
- Three guards for human-only calls: `HumanOnlyRPCs` (`internal/permpolicy/humanonly.go`, refused to a run-scoped connection, with a drift test that keeps them off `RunScopedMethods`),
  `localOnly` (`relay.device.*`: refused when `rc.local` is false, i.e. over the relay) and `humanLocalOnly` (plugins: also refused for a non-loopback direct connection unless
  `plugins.allowRemoteManage`).
- Two closed lists keep a caller class away from a method: `RunScopedMethods` (what an agent's MCP connection may call; everything else is refused before its handler) and
  `pluginNeverMethods` / `pluginNeverPrefixes` (what a plugin session may never call).
- The fleet link serves its own closed method namespace (`fleet.*` wire methods in `internal/fleet/wire` and `internal/fleet/serve`), not the daemon's handler table.

Runtime behaviour of each switch, read from the code (this decides "takes effect" in the table):

| Switch | Read where | Effect of a change |
|---|---|---|
| `automations.enabled`, `automations.paused` | `automationConfig()` on every engine tick | live, next tick; an unreadable file is OFF |
| `plugins.enabled` | `configPluginsEnabled()` on every call | live; an unreadable file is OFF |
| `learning.enabled` | captured once at startup by `wireLearning` (`lc := w.Cfg.Learning`): with it off the collector and delivery are not constructed; `SMIND_LEARNING=0` and a file read error force it off | **restart needed** as wired (ADR-0026 and the config comment say "instantly"; the wiring does not re-read it, see B4a) |
| `browser.disabled` | `BrowserDisabled()` on every decision, fails closed | live; `SMIND_BROWSER_DISABLED` wins |
| `fleet.enabled` | `newFleet()` once at startup; with it off the `fleet.*` methods are not registered | **restart needed** |

## Decision

### 1. `config.get {keys?}`: read-only

- Returns, for each requested allowlisted key (default: all of them), `{key, value, default, source, restart, writable}`. `value` is the effective value (file, then environment, as the daemon
  uses it); `source` is `"default"`, `"file"` or `"env"` (with the variable's name, never its value when the key is secret-adjacent); `restart` says a change needs a daemon restart;
  `writable` says `config.set` would accept this key for this connection (so the UI can show a switch or a read-only row without guessing the channel, which ADR-0042 noted it cannot).
- A key outside the allowlist is an error naming the method, never silently omitted. Secrets are not in the allowlist at all (section 3), so there is nothing to redact on this path; the handler
  still passes every value through the same redactor the diagnostics use, so a future key added by mistake cannot print a credential.
- **Who may call it:** any authenticated connection that is **not run-scoped**, including relay sessions and password sessions over TLS. The keys it can return are feature switches and numeric
  limits, which a paired phone needs in order to show "Automations are off, change this on the daemon's machine" instead of a dead button; none is a secret. Two things keep this narrow:
  a run-scoped (agent) connection is refused, and the `server.*` keys (listen address, port, hostnames: network topology) are returned only to a **loopback** connection, not over the relay or a
  remote login. The call is not in `RunScopedMethods`, not served on the fleet link, and under the plugin never-prefix.

### 2. `config.set {key, value}`: human-only, local-only, one key per call

- **Guard, all of these, checked before the handler runs:** not run-scoped (`refuseAgent`); `rc.local` true (refused over the relay, as `relay.device.*`); the channel is loopback (refused for a
  password session over a network, plain or TLS: a stricter rule than `humanLocalOnly`, because there is no `allowRemoteManage` escape hatch here); in `HumanOnlyRPCs`, which puts it under the
  existing drift test and in the floor's table (`config.set` named as a human-only method next to the CLI form, below); added to `pluginNeverPrefixes` as `config.`; never in `RunScopedMethods`
  and never an MCP tool; not part of the fleet wire namespace (a peer host cannot reach the daemon's handler table; a test pins that no `config.*` name appears in `fleet/wire`).
- **Behaviour:** the key must be in the writable subset (section 3); the value is validated by `config.Set`'s own rules; the write is `config.Set`: comment-preserving, atomic, refused when the
  result would not parse. One key per call (so an audit line, a confirmation and a failure each concern one switch). Setting a key whose effective source is `env` is refused with
  "overridden by SMIND_x, unset it first", because the file write would have no effect and the UI would lie.
- **Serialisation and concurrent edits:** the daemon takes one in-process mutex around read-modify-write, so two RPCs cannot lose each other's change. An editor open on the same file is a
  separate writer the daemon cannot lock; the atomic rename means the file is never torn, and the loser's edit is overwritten as with any editor. `config.set` re-reads the file inside the
  mutex (it never writes a cached copy), and the response carries the value as re-read after the write.
- **Audit:** every attempt that reaches the handler appends one line to an append-only, owner-only file in the daemon home (rotated by size): time, key, old value, new value, outcome
  (`ok` or the refusal reason), and the connection's channel. No secrets exist in the allowlist, so values are logged as they are. Refusals by the guard (agent, relay, plugin) are logged
  with the caller class, because an agent probing this method is worth seeing. On success the daemon emits a `config.updated {key}` event so open UIs refetch.
- **CLI parity:** `smind config set|get` gains the same keys (`fleet.enabled`, `plugins.enabled`, `learning.enabled`, `automations.paused` are missing from `knownKeys` today). The CLI path
  is unchanged in power: it is the same `config.Set`. See [Questions for the Human](#questions-for-the-human) Q6 about making the CLI refuse these keys inside an agent run.

### 3. The allowlist

One table drives `config.get`, `config.set`, the CLI and the UI. Everything not listed is invisible to both RPCs (the generic "edit any key" door is rejected below).

**Proposed writable (`config.set` and `config.get`):**

| Key | Type | Default | Takes effect | What it does | Risk | Over the relay (`get`) |
|---|---|---|---|---|---|---|
| `automations.enabled` | bool | false | live, next tick | the engine polls and fires | arms unattended spawners; local human only | read |
| `automations.paused` | bool | false | live, next tick | the engine records signals as paused and never fires (a brake) | low: stops firing, loses nothing | read |
| `plugins.enabled` | bool | false | live, next call | plugins can be installed and enabled | enables the plugin system (consent per plugin stays) | read |
| `fleet.enabled` | bool | false | **restart** | the fleet layer starts; `fleet.*` methods register | opens links to peer hosts (each still needs pairing and approval) | read |
| `learning.enabled` | bool | true | **restart** as wired today (B4a either re-reads it per decision, making it live, or labels it restart); the environment wins | global kill switch of the learning subsystem | turning it **off** is a brake; turning it on re-enables collection (per-workspace opt-in still decides) | read |

**Proposed read-only: `config.get` only, never `config.set` over any RPC** (a human edits the file or uses the CLI; the UI shows the value and "change this in config.yaml"):

| Key | Why it is not writable over RPC | Over the relay (`get`) |
|---|---|---|
| `browser.disabled` | the kill switch whose point is that **no RPC can change it** (ADR-0061); a person at the machine has the file and the CLI. Recommended read-only; see Q3 | read |
| `automations.allowUnattendedModes`, `automations.maxBudgetUsd`, `automations.allowRemoteManage` | each raises what an unattended agent may do or who may manage automations: a privilege lever, not a convenience switch | read |
| `plugins.allowRemoteManage` | widens who may manage plugins | read |
| `relay.legacyDevices`, `relay.maxDevices` | security-lowering (`legacyDevices`) or a pairing ceiling | read |
| `accountUsage.enabled`, `.ttlSeconds`, `.routing`, `.exhaustThresholdPct`, `.passive` | gate outbound vendor calls and routing; the call budget is ADR-0028's, not a toggle | read |
| `validation.maxBudgetUsd`, `.defaultBudgetUsd`, `orchestration.maxDepth`, `.maxConcurrentRunsPerTree`, `.notifyParent`, `.stallAlertMinutes` | spend and fan-out ceilings an agent must not raise | read |
| `server.port`, `server.listen`, `server.hostnames` | change where and to whom the daemon is reachable; restart; already CLI-editable | **loopback only** |
| `steward.enabled`, `steward.twoAxis` | the review engine: not asked for yet; revisit with its own UI | read |

**Never exposed by either RPC:** `server.passwordHash`, `push.expoAccessToken`, every relay secret and host secret, the daemon token, any key whose value is a credential, and every `SMIND_*`
environment variable's value (only whether a key is overridden, and the variable's name).

### 4. What this amends, and the exact wording

These sentences say "no RPC, by design / edit config.yaml". Each gets a dated amendment note appended **at the end of its section** (not a rewrite of the original text), applied in the
first implementation PR once the Human accepts this ADR. Doc comments in `internal/config/{config,fleet}.go` and `internal/wsapi/{plugins,automations}.go` ("no RPC, agent or relay session
can set it", "The global switches ... have no RPC at all") are corrected in the same PR.

| Where | Sentence today | Amendment note (proposed text) |
|---|---|---|
| ADR-0042, the AC2 bullet ("Global switch") | "There is no RPC for the switch." | "Amended by ADR-0062 (2026-11-27): `plugins.enabled` is writable through `config.set`, human-only and loopback-only, from Settings; it is still read on every call and still refused to agents, plugin sessions, relay sessions and non-loopback logins. Editing the file stays valid." |
| ADR-0042, Settings paragraph | "the page has **no control for the global switch** (only a human editing `config.yaml` turns plugins on ...)" | "Amended by ADR-0062: the page carries one switch for the global setting (`ConfigSwitchRow`), shown as a switch only when `config.get` reports it writable for this connection, otherwise read-only with 'change this on the daemon's machine'." |
| ADR-0042, "Honest limit" | (unchanged) | "ADR-0062 does not change this limit: a same-user shell could already edit the switch." |
| ADR-0044, section 4 ("Global switch") | "There is no RPC to change it." | "Amended by ADR-0062: `automations.enabled` and `automations.paused` are writable through `config.set` (human-only, loopback-only, audited); `allowUnattendedModes`, `maxBudgetUsd` and `allowRemoteManage` stay file-only and are readable through `config.get`." |
| ADR-0044, the recommendation under the unattended-modes question | "Same shape as `plugins.enabled`: no RPC can arm unattended agents." | "Amended by ADR-0062: the unattended-modes opt-in is still not writable over any RPC; `plugins.enabled` is the comparison only for the on/off switch." |
| ADR-0044, line "`config.yaml` (default false, no RPC)" in the automation-spawned-task paragraph | "(default false, no RPC)" | "Amended by ADR-0062: the switch has a human-only, loopback-only RPC; it is still default false." |
| ADR-0048, the feature-off paragraph and S2 | "Feature **off by default** (`fleet.enabled: false`; off = today's behaviour byte for byte, host key not created, no `fleet.*` method served)." (the code comment on `FleetConfig` adds "only a human edit of config.yaml turns it on") | "Amended by ADR-0062: `fleet.enabled` is writable through `config.set` (a restart applies it) and is added to the CLI key list. Off is still today's behaviour byte for byte." |
| ADR-0026 (learning) | "a global kill switch (`learning.enabled` in `config.yaml`, `SMIND_LEARNING=0`, and a Settings toggle) that stops collection, jobs and **delivery** instantly" | "Amended by ADR-0062: the Settings toggle is `config.set learning.enabled`; the environment variable still wins and the toggle then says so. The switch is read at startup today, so a change applies after a restart unless the implementation re-reads it per decision, in which case 'instantly' holds." |
| ADR-0061 | "no RPC can change it" (the kill switch) | none: `browser.disabled` stays read-only over RPC under the recommended answer to Q3; if the Human chooses otherwise, ADR-0061 gets an amendment note instead. |
| ADR-0031 D10 | `smind config get|set|unset` | "Amended by ADR-0062: the key list grows (section 3) and the same edit is reachable as `config.set`." |

### 5. UI

One component, `ConfigSwitchRow`: a `SettingsRow` with a `Switch`, a `ConfirmDialog` that states what turning it on enables (the same sentence the CLI prints), an `Alert` for a refusal
or failure, a pending state, and a restart notice for a key whose `restart` is true ("Takes effect after the daemon restarts"; a desktop-managed daemon also offers its existing restart). It reads
`config.get` and renders the Switch only when `writable` is true; a relay or remote client gets the read-only row and "change this on the daemon's machine". Browser's switch row is the template.
Homes: Settings → Automations (engine on/off, and Pause/Resume on the Automations page), Settings → Plugins (global switch), Settings → Hosts (fleet switch plus the restart notice),
Settings → Skills (learned) (the learning kill switch row, replacing the read-only alert). Read-only limit rows (budget ceilings, orchestration, usage polling) come from `config.get` in later,
separate UI work.

### 6. Failure modes and tests

Named, written with the implementation PRs (each must fail when the guard it names is removed):

- `config_set_refuses_run_scoped_caller`, `config_get_refuses_run_scoped_caller`
- `config_set_refuses_relay_caller`, `config_set_refuses_non_loopback_login`
- `config_set_not_served_on_the_fleet_link` (no `config.*` in `fleet/wire`), `config_set_refused_for_plugin_session`, `config_methods_not_in_run_scoped_or_mcp`
- `config_set_refuses_key_outside_allowlist`, `config_set_refuses_read_only_key` (every table row marked read-only), `config_set_refuses_secret_keys`
- `config_set_refuses_invalid_value`, `config_set_refuses_env_overridden_key`
- `config_set_preserves_comments_and_unrelated_keys`, `config_set_concurrent_calls_do_not_lose_updates`, `config_set_result_is_reread_after_write`
- `config_set_writes_audit_line_with_old_and_new`, `config_refusal_is_audited_with_caller_class`
- `config_get_returns_source_and_restart_flags`, `config_get_hides_server_keys_over_relay`
- `config_allowlist_drift`: the allowlist table in this ADR, `knownKeys`, the UI's switch list and `HumanOnlyRPCs` stay in step (one source of truth in code; the test walks them).
- `human_only_table_names_config_set`: `permpolicy` drift test for the floor/CLI table entry.

### 7. PR split

- **B4a: daemon.** `config.get` / `config.set`, the allowlist table, the guard (including `HumanOnlyRPCs`, `pluginNeverPrefixes`, the floor table entry), audit, the `config.updated` event, CLI key
  parity (`fleet.enabled` and the others), the amendment notes of section 4, and every test of section 6. Size M, security review required, validation contract with mutation checks (remove each guard).
- **B5: UI.** `ConfigSwitchRow` and its four homes (Automations including Pause/Resume, Plugins, Hosts, Learning), with light and dark frames, a relay-style read-only frame and the restart notice.
  Size S to M, one UI PR open at a time. It starts only after B4a is merged.

## Options considered

1. **A generic config editor RPC (any key, any value).** Rejected: it would turn every spend ceiling, security-lowering flag and credential-adjacent key into something a wire caller could try to
   change, and the UI does not need it. The allowlist is the entire point.
2. **Per-feature RPCs (`automation.setEnabled`, `plugin.setGlobal`, `fleet.setEnabled`, ...).** Considered. Each is clearer in isolation, but it multiplies guards (every new switch has to
   re-implement the human-only, local-only, audited path and its own drift test), and the CLI already has `config set` as the one door. One allowlisted pair gives one guard, one audit and one test
   surface. Revisit only if one switch needs behaviour `config.Set` cannot express (a restart the daemon performs itself, say).
3. **Keep `config.yaml` as the only door.** Rejected by the Human on 2026-11-27: the cost is a hand-edit and a restart for the features the product is built around.
4. **Writable over the relay for brakes** (`automations.paused`, `learning.enabled: false`), as `automation.disable` is today. Not proposed: one rule ("every `config.set` is loopback-only") is easier
   to audit and to explain; a phone can disable an individual automation already. Listed as Q4.

## What we are NOT doing

- No generic key editor, no wildcard, no batch set, no `unset` over RPC.
- No agent, MCP, plugin, relay or fleet-link access to `config.set`, ever; and no new secret or per-boot proof of presence (that is ADR-0056's open question, not this record's).
- No change to who the human is on the socket (ADR-0056), and no claim that this closes the same-user shell hole: a shell can still edit the file or run the CLI. This record adds no power over it.
- No UI for `browser.disabled`, `allowUnattendedModes`, `allowRemoteManage`, `legacyDevices`, the password, `pprof` or any ceiling; they stay file or CLI only.
- No hot reload of `fleet.enabled`; it needs the restart it needs today and the UI says so.
- No secrets in `config.get`, and no environment variable values.

## Consequences

- One guard, one audit file and one drift test cover every switch, and a new switch is a table row plus a test row, not a new RPC.
- The Settings pages stop telling people to edit a file for the five switches; a remote or relay client sees the state and where to change it.
- The amendment notes keep ADR-0042/0044/0048 honest: the old "no RPC" lines were true when written and are corrected rather than silently contradicted.
- `fleet.enabled` and the other missing keys become settable from the CLI, which is a behaviour change for scripts only in the sense that a refusal becomes an accepted write.
- The honest limit stays: a same-user process can already change these switches. If the Human later wants a presence proof for the arming switches (ADR-0056 option 1 or 2), it applies to `config.set`
  as one more human-only method, with no redesign.

## Questions for the Human

The allowlist (section 3) is the part still open; these are the choices inside it, each with a recommendation.

1. **The writable set.** Proposed: `automations.enabled`, `automations.paused`, `plugins.enabled`, `fleet.enabled`, `learning.enabled`. Recommendation: accept; `learning.enabled` (a restart-applied brake, see the table) and `automations.paused`
   are brakes, the other three arm something and are guarded by loopback-only plus a confirmation.
2. **Who may READ.** Recommendation: any non-agent connection, including the relay, for the feature switches and limits; `server.*` loopback only (section 1). Alternative: loopback only for everything
   (the phone then cannot show state).
3. **`browser.disabled`.** Recommendation: **read-only over RPC** (kill switch that no wire can flip, ADR-0061). Alternative: writable for a loopback human, which lets the UI offer a one-click "stop all
   agent browsers" brake but makes the kill switch reachable by anything that holds the daemon token on loopback.
4. **Brakes over the relay.** Recommendation: no, one rule. Alternative: allow `automations.paused: true` and `learning.enabled: false` (only toward the safe value) over the relay, like `automation.disable`.
5. **Loopback only, or any direct login.** Recommendation: loopback only for `config.set`, no `allowRemoteManage`-style escape hatch (the switches arm things; a LAN password login is not a person
   at the machine). Alternative: reuse `humanLocalOnly`'s rule, which allows a non-loopback login when a config flag says so.
6. **The CLI inside an agent run.** Today `smind config set` is not in the human-only CLI table, so an agent in a shell can run it. Recommendation: **not in this ADR** (it would also stop
   orchestrating agents from setting `server.port` in a scratch daemon), noted as a known gap with ADR-0056. Alternative: make `config set` human-only for the five writable keys only (needs a key filter the
   table does not have today; a small extension in B4a).
