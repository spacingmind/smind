# Changelog

## [0.16.0](https://github.com/spacingmind/smind/compare/v0.15.0...v0.16.0) (2026-10-27)

### Upgrade notes

* **automations** (ADR-0044) and **plugins** (ADR-0042 Wave A+B) ship switched off: `automations.enabled` and `plugins.enabled` are `false` and can only be turned on in `config.yaml`; plugin management needs a loopback connection unless `plugins.allowRemoteManage` is set.
* **orchestration:** the seeded role pack now has a `validator` seat instead of `reviewer` (ADR-0045). Seeding never touches existing profiles: add the seat with `smind profile seed roles` and delete an old `reviewer` yourself if you want.
* **mobile push:** stall alerts arrive only after the phone re-registers for push (open the app's notification settings once).
* **claude-native on Windows:** the deny rules for smind's own secrets are written in the CLI's POSIX path form (`/c/...`); not yet verified against a real Windows `claude`.


### Features

* **automation:** GitHub workflow_run source by polling gh under the forge budget (ADR-0044 PR 2) ([#657](https://github.com/spacingmind/smind/issues/657)) ([03882a9](https://github.com/spacingmind/smind/commit/03882a903d774c7ac3588b87dcd14f5438d5165d))
* **automation:** pilot example, live isolated-daemon proof and two fixes (ADR-0044 PR 4) ([#663](https://github.com/spacingmind/smind/issues/663)) ([3671710](https://github.com/spacingmind/smind/commit/3671710d5baacd5a6cbc5e6bd1a9d65722461b60))
* **automation:** signal-to-task engine, rails, RPCs and CLI (ADR-0044 PR 1) ([#653](https://github.com/spacingmind/smind/issues/653)) ([5234c1d](https://github.com/spacingmind/smind/commit/5234c1d5d8d08e35e2c2dfe75b0c4a7ceca9ac7d))
* **inbox:** derived Human inbox read model, RPCs and inbox.changed (ADR-0045 P3) ([#665](https://github.com/spacingmind/smind/issues/665)) ([cf29c05](https://github.com/spacingmind/smind/commit/cf29c05942b30d662f452696fbd1c46323c6ceea))
* **mobile:** Human inbox screen, Tasks-header entry and live badge ([#671](https://github.com/spacingmind/smind/issues/671)) ([9244353](https://github.com/spacingmind/smind/commit/92443538e62f4f5ba81ab522fc7610631bf12f0c))
* **mobile:** read-only automations list, history and Pause (ADR-0044 PR 3b) ([#660](https://github.com/spacingmind/smind/issues/660)) ([00c6124](https://github.com/spacingmind/smind/commit/00c61240a271fa39f4b2a5592eef228af0154932))
* **plugins:** plugin-scoped session, events.observe and typed settings (ADR-0042 PB.2) ([#654](https://github.com/spacingmind/smind/issues/654)) ([6d37159](https://github.com/spacingmind/smind/commit/6d371592f2455ff25c265f080d6ca51f60006ea1))
* **plugins:** plugin.call, RPC-backed slash commands and hello-process example (ADR-0042 PB.4) ([#667](https://github.com/spacingmind/smind/issues/667)) ([8a3c021](https://github.com/spacingmind/smind/commit/8a3c021d24ce524d085280d7f7508bc3ee4b24f9))
* **plugins:** run plugin processes: scrubbed env, framed JSON-RPC, supervisor, logs (ADR-0042 PB.1) ([#643](https://github.com/spacingmind/smind/issues/643)) ([08f278b](https://github.com/spacingmind/smind/commit/08f278bc4c0a9b16ddaacfb28b09c503d725ac8a))
* **plugins:** run.beforeStart/run.finished hooks and task.send (ADR-0042 PB.3) ([#661](https://github.com/spacingmind/smind/issues/661)) ([4dbfce3](https://github.com/spacingmind/smind/commit/4dbfce317db269d04a59e1a149e734dbc5cc1523))
* **push:** an auto-pause with no task is pushed and opens the automation (ADR-0044 question 7) ([#669](https://github.com/spacingmind/smind/issues/669)) ([a197ee0](https://github.com/spacingmind/smind/commit/a197ee01d650b52df4eb2230338a92dfd459de7c))
* **push:** notify when an automation auto-pauses (ADR-0044 question 7) ([#664](https://github.com/spacingmind/smind/issues/664)) ([6a073e4](https://github.com/spacingmind/smind/commit/6a073e47dc560b9d956749272d291954e606621b))
* **validation:** contract, verdict and merge-ready model with RPCs and CLI (ADR-0045 P1) ([#659](https://github.com/spacingmind/smind/issues/659)) ([052fb22](https://github.com/spacingmind/smind/commit/052fb2239456d229eace950bdc865f44808401f8))
* **validation:** QA frame harness as the validator's bundled skill (ADR-0045 Q11) ([#677](https://github.com/spacingmind/smind/issues/677)) ([d444e6d](https://github.com/spacingmind/smind/commit/d444e6def59723fbb0cb495cf757af52e14bca4a))
* **validation:** validator seat, request flow and verdict wiring ([#662](https://github.com/spacingmind/smind/issues/662)) ([436a0f6](https://github.com/spacingmind/smind/commit/436a0f6136fbf3798fbcc8cb35118b4dec7a1681))
* **web:** Human inbox page, sidebar badge and live updates ([#670](https://github.com/spacingmind/smind/issues/670)) ([bdb9e43](https://github.com/spacingmind/smind/commit/bdb9e435f50dc21f60316b4c0659eefff5145010))
* **web:** Settings -&gt; Automations with run history (ADR-0044 PR 3a) ([#658](https://github.com/spacingmind/smind/issues/658)) ([fffef9b](https://github.com/spacingmind/smind/commit/fffef9b88c9bc625a898f43d57499e3e6ecfcb64))
* **workspace:** agent-readiness check (RPC, CLI, web panel) ([#678](https://github.com/spacingmind/smind/issues/678)) ([b8bee67](https://github.com/spacingmind/smind/commit/b8bee6731f6501fbad4124b4610676d9584133e9))


### Bug Fixes

* **config:** use absolute temp dirs for provider env paths in tests on Windows ([#651](https://github.com/spacingmind/smind/issues/651)) ([a9e2b13](https://github.com/spacingmind/smind/commit/a9e2b13e7227955d5e34355d08b434aa0e05f444))
* **devharness:** seed-live waits until the agent is in its scenario (flaky queue-interrupt test) ([#666](https://github.com/spacingmind/smind/issues/666)) ([4592e88](https://github.com/spacingmind/smind/commit/4592e88f16627bcf4a327df801658aa934da0f93))
* **learning:** make probe/export/loads tests and the arm diff work on Windows ([#649](https://github.com/spacingmind/smind/issues/649)) ([913ed8a](https://github.com/spacingmind/smind/commit/913ed8ae7912da61cdaeb591716f1969acc88284))
* **mobile:** accept and subscribe to stalled pushes ([#668](https://github.com/spacingmind/smind/issues/668)) ([d68fd0a](https://github.com/spacingmind/smind/commit/d68fd0a0f5e2b9218f22faafc28ddc8906a23575))
* **plugins:** stop the plugin process before swapping its directory (Windows rename failure) ([#679](https://github.com/spacingmind/smind/issues/679)) ([2322ba9](https://github.com/spacingmind/smind/commit/2322ba9dfd57792d207564b21adfeef14121a810))
* **qaframes:** portable test fixtures on Windows; supervisor kill test on all OSes ([#681](https://github.com/spacingmind/smind/issues/681)) ([95d20b3](https://github.com/spacingmind/smind/commit/95d20b3838023ea5d8e9576866dc7d69201eea31))
* **repo:** check text files out with LF on Windows ([#644](https://github.com/spacingmind/smind/issues/644)) ([c5dba60](https://github.com/spacingmind/smind/commit/c5dba60bdd7bb52979ae15f08a8f0d22a7fef47e))
* **routing:** on equal timestamps the later routing decision wins ([#648](https://github.com/spacingmind/smind/issues/648)) ([ef4a341](https://github.com/spacingmind/smind/commit/ef4a341f92423f47579d57bee0f29aba8846580a))
* **runs:** bump claude-agent-sdk-go to v0.4.2 and use its exit status ([#640](https://github.com/spacingmind/smind/issues/640)) ([b7d5959](https://github.com/spacingmind/smind/commit/b7d5959e194f82611a02904dd974327d73431f68))
* **runs:** make the unpinned-model and cli-exit tests Windows-safe ([#650](https://github.com/spacingmind/smind/issues/650)) ([49d7a95](https://github.com/spacingmind/smind/commit/49d7a95698eb55f4fcc37419768eb48ee30a5c95))
* **secretdeny:** write deny-rule paths in the CLI's POSIX form on Windows ([#645](https://github.com/spacingmind/smind/issues/645)) ([e6510e9](https://github.com/spacingmind/smind/commit/e6510e9e6842e77a2936e538aee74f53f9ead1e6))
* **smind:** isolate USERPROFILE as well as HOME in learning CLI tests ([#647](https://github.com/spacingmind/smind/issues/647)) ([382174f](https://github.com/spacingmind/smind/commit/382174f538c9634bb1547bb75a4a4ed3ed9c52d5))
* **taskrunner:** make two claude-native tests Windows-safe ([#646](https://github.com/spacingmind/smind/issues/646)) ([5562c06](https://github.com/spacingmind/smind/commit/5562c06680228393f81af28766fa6f9d639833a1))
* **validation:** scope check no longer rewrites the lead's index, names what changed; fix the seat test's baseline race ([#674](https://github.com/spacingmind/smind/issues/674)) ([d9d9bbc](https://github.com/spacingmind/smind/commit/d9d9bbcdbace1940e3ef7783eed2876ac09a4ca4))
* **wsapi:** publish forge.status before releasing the status read ([#655](https://github.com/spacingmind/smind/issues/655)) ([7e32c58](https://github.com/spacingmind/smind/commit/7e32c5881c2854959f1435815d461fbc5ff70c7d))
* **wsapi:** use absolute temp paths in provider env tests on Windows ([#641](https://github.com/spacingmind/smind/issues/641)) ([a3994aa](https://github.com/spacingmind/smind/commit/a3994aa41cc5450df62af30d247c1e94ec016d16))

## [0.15.0](https://github.com/spacingmind/smind/compare/v0.14.0...v0.15.0) (2026-10-25)

### ⚠ BREAKING CHANGES

* **runs:** a claude-native run always passes an explicit `--model`; with no requested, profile or `providers.claude-native.defaultModel` model the run is refused with `no_model` instead of silently using the CLI's own default. Set `providers.claude-native.defaultModel` before upgrading if any client sends without a model ([#633](https://github.com/spacingmind/smind/issues/633)).
* **orchestration:** the seeded role pack is now `lead`, `peer-sonnet` and `reviewer`; the `supervisor`, `supervisor-sonnet` and `peer-glm` seats are no longer seeded, and the `lead`/`peer-sonnet` instructions changed. Seeding never touches profiles that already exist: delete the old seats or update them with `smind profile edit` to adopt the new role pack ([#617](https://github.com/spacingmind/smind/issues/617)).
* **providers:** provider ids are plain strings in the wire catalogs (`provider.list` is the source); custom ACP providers and provider instances come from `providers.custom` ([#627](https://github.com/spacingmind/smind/issues/627), [#628](https://github.com/spacingmind/smind/issues/628)).
* **web:** per-run and per-chat cost chips are gone; costs live in Settings → Usage, labelled "≈ API price" for subscription logins ([#608](https://github.com/spacingmind/smind/issues/608)).


### Features

* ≈ marker on an inferred rate-limit reset time (web, mobile, CLI) ([#600](https://github.com/spacingmind/smind/issues/600)) ([5e2d98f](https://github.com/spacingmind/smind/commit/5e2d98f025dcabe6db7137e09715e8e43626325c))
* agent git-operation approval rules on the permission card (G13) ([#595](https://github.com/spacingmind/smind/issues/595)) ([bf7285b](https://github.com/spacingmind/smind/commit/bf7285bf627e39509dbd01fa07f30cfca9b973ea))
* **archive:** look up a squash-merged PR on demand for tasks with no stored PR (ADR-0039 O6) ([#599](https://github.com/spacingmind/smind/issues/599)) ([a400c4a](https://github.com/spacingmind/smind/commit/a400c4ab1d1bb3dcfbf6c5a5ac793048786cad2c))
* **attach:** fetch PR/issue attachments at accept time through forge.Provider (ADR-0038 PR 5b) ([#479](https://github.com/spacingmind/smind/issues/479)) ([cec6c63](https://github.com/spacingmind/smind/commit/cec6c637d0be2c7a758d0ead820d2422ba1a2d03))
* **attach:** native image delivery and claude-agent-sdk-go v0.4.0 (ADR-0038 PR 3) ([#473](https://github.com/spacingmind/smind/issues/473)) ([5b9b0ca](https://github.com/spacingmind/smind/commit/5b9b0ca9510c69dd9166492899109823004e9c5f))
* built-in smind MCP entry for seat runs, run identity and a fail-closed method allowlist (ADR-0041 S2b) ([#565](https://github.com/spacingmind/smind/issues/565)) ([d0cd6c4](https://github.com/spacingmind/smind/commit/d0cd6c4a40cad688c790edee205f1a81f37f76e1))
* child tasks branch from the parent (G18) ([#610](https://github.com/spacingmind/smind/issues/610)) ([fbe6834](https://github.com/spacingmind/smind/commit/fbe6834751e3a69556dc0f8203d328f691fedf28))
* child updates in the timeline, queue strip and task runs (ADR-0041 N3) ([#558](https://github.com/spacingmind/smind/issues/558)) ([c4e80b9](https://github.com/spacingmind/smind/commit/c4e80b9468d66b989201a5322c9cb10ea60737c1))
* **cli:** quiet run output (hide raw, one-line auto-accept), note honours defaultModel ([#500](https://github.com/spacingmind/smind/issues/500)) ([8079cf1](https://github.com/spacingmind/smind/commit/8079cf1e4b07c918878fd619670f2625d200c7a2))
* **cli:** read-only archived-task history in task ls, runs and tree ([#618](https://github.com/spacingmind/smind/issues/618)) ([42289cf](https://github.com/spacingmind/smind/commit/42289cf7774fe7a87895e6fb6ecc6b4d87719b3a))
* **cli:** smind profile edit (ADR-0041 S1w) ([#574](https://github.com/spacingmind/smind/issues/574)) ([39cc7b8](https://github.com/spacingmind/smind/commit/39cc7b85140287ec1b51a453127ae86d7c001ed5))
* **cli:** smind status shows the daemon the CLI is talking to ([#509](https://github.com/spacingmind/smind/issues/509)) ([3f03fcc](https://github.com/spacingmind/smind/commit/3f03fcc59b945c27cb62902256d30ce1056f8406))
* **cli:** smind task archive with archive preview, --delete-branch and --force ([#586](https://github.com/spacingmind/smind/issues/586)) ([5193ae8](https://github.com/spacingmind/smind/commit/5193ae8af36b54f0c77d9b5a6fce2b0ccd41298b))
* **cli:** smind task tree ([#578](https://github.com/spacingmind/smind/issues/578)) ([a2d9922](https://github.com/spacingmind/smind/commit/a2d9922629aa5d091907df04048b197cb5886685))
* **cli:** task ls shows PARENT and SETUP; task wait names the task (built entirely inside smind: Supervisor → Lead → 2 Peers) ([#570](https://github.com/spacingmind/smind/issues/570)) ([de8a18e](https://github.com/spacingmind/smind/commit/de8a18e21245497b26aa0ca9b833e890962a48af))
* **cli:** task new --base/--branch/--pr start options ([#494](https://github.com/spacingmind/smind/issues/494)) ([edf2d6d](https://github.com/spacingmind/smind/commit/edf2d6da18402e4a577154bcc1b5b22c742fe664))
* **cli:** task runs shows started, duration, usage, commits and the error cause ([#507](https://github.com/spacingmind/smind/issues/507)) ([f3573d6](https://github.com/spacingmind/smind/commit/f3573d6cd60f7097fe33510adf3abf6b4f988ec0))
* **cli:** task send --detach prints item:&lt;id&gt; for a queued prompt; task wait follows it ([#524](https://github.com/spacingmind/smind/issues/524)) ([34881bd](https://github.com/spacingmind/smind/commit/34881bddc0225ab11806a44b1100da8fbc58ac0e))
* **cli:** task send --detach/--thinking, MODEL column in task runs ([#495](https://github.com/spacingmind/smind/issues/495)) ([e91cfa7](https://github.com/spacingmind/smind/commit/e91cfa7e6ec5951f9b79564441794aaf1c4d25bf))
* **cli:** task send queues by itself while the setup script runs ([#508](https://github.com/spacingmind/smind/issues/508)) ([7e3512e](https://github.com/spacingmind/smind/commit/7e3512e5feeb57c342399c5835b617cb8f6505a4))
* **cli:** task wait &lt;runId&gt; blocks until the run ends, exit code = outcome ([#506](https://github.com/spacingmind/smind/issues/506)) ([733b6d0](https://github.com/spacingmind/smind/commit/733b6d0e0480b7c9f2c271388626dc76bd0d3d0e))
* **config:** per-provider default model (providers.&lt;id&gt;.defaultModel) ([#496](https://github.com/spacingmind/smind/issues/496)) ([93a8c83](https://github.com/spacingmind/smind/commit/93a8c83eb03b45de30c333cb768a94861a1f3195))
* deliver a seat's role instructions to the agent on every spawn (ADR-0041 S2a) ([#564](https://github.com/spacingmind/smind/issues/564)) ([394729f](https://github.com/spacingmind/smind/commit/394729f78507353c7cd94cef82d9a906f9f1e6f7))
* **gitstate:** forge PR poller, PR identity and task.gitRefresh (ADR-0039 G2) ([#503](https://github.com/spacingmind/smind/issues/503)) ([dfdc536](https://github.com/spacingmind/smind/commit/dfdc536fbbe8c9f96e3c77621a5c10ed71b97bd3))
* **gitstate:** task.git status, conflict token, merged verdict and event topic (ADR-0039 G5) ([#482](https://github.com/spacingmind/smind/issues/482)) ([b89c1d2](https://github.com/spacingmind/smind/commit/b89c1d2e8a77322595540a8286e80316b3a89cb0))
* **install:** install.sh --desktop for the un-notarized macOS app, and document it ([#478](https://github.com/spacingmind/smind/issues/478)) ([f6524c8](https://github.com/spacingmind/smind/commit/f6524c8a6673090b2360fb59c5a827c6074c6da9))
* **mobile:** create a task from the phone ([#480](https://github.com/spacingmind/smind/issues/480)) ([e666a22](https://github.com/spacingmind/smind/commit/e666a22012352d658f4545b3b56890e3e98c1563))
* **mobile:** prompt attachments (ADR-0038 mobile half) ([#581](https://github.com/spacingmind/smind/issues/581)) ([6e78a4e](https://github.com/spacingmind/smind/commit/6e78a4e03bb0d2a9b3768b2df8fa1f47f0114f52))
* **mobile:** push logic — decrypt, key store, opt-in registration, tap target (ADR-0040 PR 5) ([#493](https://github.com/spacingmind/smind/issues/493)) ([c344de6](https://github.com/spacingmind/smind/commit/c344de6fd4388cb266e33a745219a4f182d5c47f))
* **mobile:** push native wiring — background task, Notifications row, taps (ADR-0040 PR 6) ([#517](https://github.com/spacingmind/smind/issues/517)) ([7ecd5d8](https://github.com/spacingmind/smind/commit/7ecd5d8aa01d69956667e81be493b992c20fe83e))
* **mobile:** read-only plugin list, prompt commands and themes (ADR-0042 PA.5) ([#636](https://github.com/spacingmind/smind/issues/636)) ([3bc38a1](https://github.com/spacingmind/smind/commit/3bc38a1af80a2d8a98a7f3852febac96c334e800))
* **mobile:** read-only task tree on the task screen (ADR-0041 M1) ([#594](https://github.com/spacingmind/smind/issues/594)) ([cbea73d](https://github.com/spacingmind/smind/commit/cbea73d69c70d835b9626d15c20693d047772914))
* opt-in tree budget, daemon half (ADR-0041 A5) ([#602](https://github.com/spacingmind/smind/issues/602)) ([c6a1071](https://github.com/spacingmind/smind/commit/c6a1071f254dda9a7aa4f19fb7d0f3fbb98ee64a))
* **orchestration:** fold supervisor seats and peer-glm, Lead as single loop, Peer json handoff ([#617](https://github.com/spacingmind/smind/issues/617)) ([b998e70](https://github.com/spacingmind/smind/commit/b998e70b5cccb2918e927888d2b46b532ee30375))
* per-provider launch env (CLAUDE_CONFIG_DIR / CODEX_HOME) in Settings → Providers ([#603](https://github.com/spacingmind/smind/issues/603)) ([5c0e737](https://github.com/spacingmind/smind/commit/5c0e737a64e4e5829f29633cfc26164bc874fd72))
* **plugins:** host foundation, pinned install sources, safe extraction, consent and local-only guards (ADR-0042 PA.1) ([#630](https://github.com/spacingmind/smind/issues/630)) ([69c8908](https://github.com/spacingmind/smind/commit/69c8908ad23060f0d5ef5e880173397c6050d315))
* **plugins:** MCP, skill and profile contributions with owned rows and live propagation (ADR-0042 PA.2) ([#631](https://github.com/spacingmind/smind/issues/631)) ([8ea5d7e](https://github.com/spacingmind/smind/commit/8ea5d7e35abd0265b0d4533d6882023b874a47d6))
* **plugins:** prompt slash commands and themes (ADR-0042 PA.3) ([#634](https://github.com/spacingmind/smind/issues/634)) ([f1e0d8a](https://github.com/spacingmind/smind/commit/f1e0d8aeda2949f7f3a1dad019414f15e534ceda))
* **profiles:** claude-native seats wait out an account limit; the Lead defaults to peer-sonnet ([#577](https://github.com/spacingmind/smind/issues/577)) ([b326d7d](https://github.com/spacingmind/smind/commit/b326d7dd423147bd2e81bfe0e055a2ac8d86ce17))
* **profiles:** Lead and Supervisor never end a turn waiting on a background command ([#584](https://github.com/spacingmind/smind/issues/584)) ([5e301f3](https://github.com/spacingmind/smind/commit/5e301f3ff9f71a58e2ef97fdf06ae767dc634734))
* **providers:** provider registry, providers.custom and provider instances (ADR-0042 Wave 0, daemon) ([#627](https://github.com/spacingmind/smind/issues/627)) ([744b48c](https://github.com/spacingmind/smind/commit/744b48c9f747e3581efdc7e8e88ebc5ba164ea30))
* **push:** daemon wiring — device id, push.* RPCs, notifier, config (ADR-0040 PR 3) ([#499](https://github.com/spacingmind/smind/issues/499)) ([d41d6d3](https://github.com/spacingmind/smind/commit/d41d6d3e10b77ca952813cc6225d8dc9d76939af))
* **push:** internal/push core — seal, registry, Expo sender (ADR-0040 PR 2) ([#487](https://github.com/spacingmind/smind/issues/487)) ([773f0d2](https://github.com/spacingmind/smind/commit/773f0d2d8eab704cccdbfad5d2bb27254b0d4e09))
* **push:** push.test, smind push test, devices PUSH column, homelab guide (ADR-0040 PR 4) ([#520](https://github.com/spacingmind/smind/issues/520)) ([1cd3120](https://github.com/spacingmind/smind/commit/1cd312014eab16e20cb45ee07b0514837f7343d3))
* rate-limit failure in task wait (exit 6), task runs, MCP and task.tree (ADR-0041 R2) ([#546](https://github.com/spacingmind/smind/issues/546)) ([782d3bc](https://github.com/spacingmind/smind/commit/782d3bcfeafd93817e9b14e4328a79c6d3e3ce65))
* **runs:** deny rules for smind's own secrets in claude-native --settings ([#632](https://github.com/spacingmind/smind/issues/632)) ([2780f37](https://github.com/spacingmind/smind/commit/2780f374709fbbd65a01d4cbde2e73d345e25808))
* **runs:** offer Continue on resumable failed runs (cli_exited, rate limit past its reset) ([#620](https://github.com/spacingmind/smind/issues/620)) ([f533981](https://github.com/spacingmind/smind/commit/f533981e135b884da7c54178c5d98899ccbd8084))
* **runs:** onRateLimit=fallback continues the work on the fallback profile (ADR-0041 F3) ([#557](https://github.com/spacingmind/smind/issues/557)) ([617a72e](https://github.com/spacingmind/smind/commit/617a72eb35ec547a9af028b86a2326e53fa7f48e))
* **runs:** parent notifier — a Lead is woken when a child's subtree is done (ADR-0041 N2) ([#552](https://github.com/spacingmind/smind/issues/552)) ([53bb181](https://github.com/spacingmind/smind/commit/53bb1810817c7013e4ba09ae974fbd2d863f5f22))
* **runs:** profile failover policy data and onRateLimit=wait (ADR-0041 F2) ([#555](https://github.com/spacingmind/smind/issues/555)) ([f200bda](https://github.com/spacingmind/smind/commit/f200bda1a820aeb9afcd5744ebecd4cdead2d680))
* **runs:** provider cooldown after a classified rate limit (ADR-0041 F1) ([#549](https://github.com/spacingmind/smind/issues/549)) ([1fa2445](https://github.com/spacingmind/smind/commit/1fa24451d1a6d71db3ab382510a388b362aa6bcb))
* **runs:** reply chat on child runs, notifyOnFinish, and held queue items (ADR-0041 N1) ([#551](https://github.com/spacingmind/smind/issues/551)) ([b9e3e3d](https://github.com/spacingmind/smind/commit/b9e3e3de19022149f42cfe33fb3bbe739eb06691))
* **runs:** typed rate_limited failure reason with resetAt (ADR-0041 R1) ([#541](https://github.com/spacingmind/smind/issues/541)) ([34a13ec](https://github.com/spacingmind/smind/commit/34a13ece1695e18191576123eb328a734c7ab756))
* set a provider's default model in Settings, and Ready (CLI login) on the Providers page (U15, U29) ([#563](https://github.com/spacingmind/smind/issues/563)) ([0e75d91](https://github.com/spacingmind/smind/commit/0e75d911f56b8842fc9fe163f247fab8c5596d40))
* **skills:** add single-agent-loop skill + step-0 audit of the in-smind orchestration ([#616](https://github.com/spacingmind/smind/issues/616)) ([1250ec1](https://github.com/spacingmind/smind/commit/1250ec17a3b0bfec54ccb46d5bfd245e17eba462))
* stalled-tree alert (ADR-0041 M2) ([#605](https://github.com/spacingmind/smind/issues/605)) ([a11c415](https://github.com/spacingmind/smind/commit/a11c415f7c4e2a13c99ff3e7516c3a7ca991faa0))
* sticky seats — a follow-up that omits the profile inherits the chat's last profile (ADR-0041) ([#571](https://github.com/spacingmind/smind/issues/571)) ([1a51841](https://github.com/spacingmind/smind/commit/1a51841b431b8847cf51273b08df231519b90197))
* **taskrunner:** claude-native asks before always-ask git/gh operations via --settings permissions.ask (G13) ([#596](https://github.com/spacingmind/smind/issues/596)) ([1390aef](https://github.com/spacingmind/smind/commit/1390aef2e6b04efc651e009179c2d70655be1f96))
* **taskrunner:** stream claude-native text and thinking, keep the partial answer on Stop ([#528](https://github.com/spacingmind/smind/issues/528)) ([f2226d1](https://github.com/spacingmind/smind/commit/f2226d1272c4d5724ee3ebdea8f00c9e4f729c66))
* the seat contract on agent profiles — data, validation, CLI, MCP (ADR-0041 S1) ([#561](https://github.com/spacingmind/smind/issues/561)) ([a1cfd46](https://github.com/spacingmind/smind/commit/a1cfd468b79d3da93a251b1d0e400a91f07be18a))
* the seat role pack, `smind profile seed roles`, `task send --profile` (ADR-0041 S4a) ([#566](https://github.com/spacingmind/smind/issues/566)) ([c0aafc8](https://github.com/spacingmind/smind/commit/c0aafc845d32337ac3e8aee5edd37d34f1e51fa3))
* **ui:** @ file mention, PR/issue picker and Attach menu (ADR-0038 6b/6c) ([#476](https://github.com/spacingmind/smind/issues/476)) ([54b79b8](https://github.com/spacingmind/smind/commit/54b79b8e5e6ef12710628574d0c3d5598363c5ca))
* **ui:** a rate-limited run in the web footer, sidebar, lineage and mobile (ADR-0041 R3) ([#550](https://github.com/spacingmind/smind/issues/550)) ([a23b264](https://github.com/spacingmind/smind/commit/a23b264f7603eb5783d699b3c60edb7c8e89d2b5))
* **ui:** Add to chat for changed files, hunks and Files rows (ADR-0038 G10) ([#498](https://github.com/spacingmind/smind/issues/498)) ([235d22e](https://github.com/spacingmind/smind/commit/235d22e9f52d929bd634928200225f6b018a36fa))
* **ui:** archive risk dialog, Archived view and Restore (ADR-0039 G8 web) ([#545](https://github.com/spacingmind/smind/issues/545)) ([7196e8a](https://github.com/spacingmind/smind/commit/7196e8a55129d3f5ed5e9f6d81a630b31168d2f4))
* **ui:** New Task Start-from branch/PR/issue and the fork-PR setup gate (ADR-0039 G4) ([#537](https://github.com/spacingmind/smind/issues/537)) ([ad36404](https://github.com/spacingmind/smind/commit/ad36404746b325d5a4251df3a6d425fe41dc149c))
* **ui:** previewed Create PR, Update PR and the no-origin blocker (ADR-0039 G3) ([#538](https://github.com/spacingmind/smind/issues/538)) ([60904c1](https://github.com/spacingmind/smind/commit/60904c1d0af9c625722268b6851a185f4d3ffd5f))
* **ui:** Settings → GitHub card with token paste (ADR-0039 G12 web) ([#548](https://github.com/spacingmind/smind/issues/548)) ([5d5cb78](https://github.com/spacingmind/smind/commit/5d5cb78233c5f82f5ab02083dae8dcba56a9e413))
* **ui:** task git and PR state in the sidebar, header and composer; remove task.stats (ADR-0039) ([#497](https://github.com/spacingmind/smind/issues/497)) ([0a2ee25](https://github.com/spacingmind/smind/commit/0a2ee252f57df17fbf579df84b0306e53995ff51))
* **web,mobile:** Retry on fallback, Wait until, and continued-on badge for a rate-limited run (ADR-0041 AC17/AC20) ([#580](https://github.com/spacingmind/smind/issues/580)) ([1bbed51](https://github.com/spacingmind/smind/commit/1bbed51dc850bb3623a1e65083bb934b7cdb96b7))
* **web:** composer chip resolves an unresolved Default model from the last run ([#579](https://github.com/spacingmind/smind/issues/579)) ([77a30d2](https://github.com/spacingmind/smind/commit/77a30d209a0b5d39264a50a42ee0b0dfffb4555b))
* **web:** Draft with agent button in the commit bar (G17) ([#606](https://github.com/spacingmind/smind/issues/606)) ([7e2e233](https://github.com/spacingmind/smind/commit/7e2e233d61e351cdbda339e5273db773ca9694a6))
* **web:** drop per-chat cost display; subscription label + Space group-by in Usage ([#608](https://github.com/spacingmind/smind/issues/608)) ([6b6836b](https://github.com/spacingmind/smind/commit/6b6836b44342434548a922270f5e484d36d3935d))
* **web:** PR panel tab with checks, reviews, threads and send to chat (G9 web) ([#593](https://github.com/spacingmind/smind/issues/593)) ([225c8e1](https://github.com/spacingmind/smind/commit/225c8e171852585c7377f127288aeae3fc2a851e))
* **web:** provider ids as strings, Settings instances and one-click Add instance (ADR-0042 P0.2, AC37) ([#628](https://github.com/spacingmind/smind/issues/628)) ([182b6c6](https://github.com/spacingmind/smind/commit/182b6c653670433ce167134e8f0b211314471371))
* **web:** Push and Update from base card and menu (G7 web) ([#583](https://github.com/spacingmind/smind/issues/583)) ([748daf8](https://github.com/spacingmind/smind/commit/748daf8cbade196d97fbd05c086f51a1aaa01f06))
* **web:** seat-contract and failover fields in the agent editor (ADR-0041 S1w) ([#576](https://github.com/spacingmind/smind/issues/576)) ([1e717cb](https://github.com/spacingmind/smind/commit/1e717cb3f42a8b24282d4ad855d75161a24c8a9a))
* **web:** Settings → Plugins page and from-plugin owner badges (ADR-0042 PA.4) ([#635](https://github.com/spacingmind/smind/issues/635)) ([d56cc83](https://github.com/spacingmind/smind/commit/d56cc83bd86c16ce31ea6f434581c478767de40e))
* **web:** sidebar stage view (Working / Ready to review / Done) ([#597](https://github.com/spacingmind/smind/issues/597)) ([311ad6a](https://github.com/spacingmind/smind/commit/311ad6a63f01c0a41c09ffb78d5b57a1f2fd5bef))
* **web:** the composer sends profileId when an applied agent profile is unchanged (ADR-0041 S2w) ([#568](https://github.com/spacingmind/smind/issues/568)) ([d12c21a](https://github.com/spacingmind/smind/commit/d12c21a30514150624d0c5d3d27f945f5540fbab))
* **web:** tree budget chip and popover in the root task header (ADR-0041 A5) ([#604](https://github.com/spacingmind/smind/issues/604)) ([b49b04d](https://github.com/spacingmind/smind/commit/b49b04da21f9e420806c110342a4e9fbae4f3619))
* **workspace:** archive risk preview, merged-branch cleanup and Restore (ADR-0039 G8 daemon) ([#544](https://github.com/spacingmind/smind/issues/544)) ([0079063](https://github.com/spacingmind/smind/commit/0079063395d5ec139aec99ccbf569b510679801e))
* **workspace:** stored task base, default base branch, no reflog/develop constant (ADR-0039 G1) ([#469](https://github.com/spacingmind/smind/issues/469)) ([bbea7b0](https://github.com/spacingmind/smind/commit/bbea7b0576db3f8f743a6ad9b5a3de0ef9257430))
* **wsapi:** admission policy for identified seat callers (ADR-0041 S3) ([#567](https://github.com/spacingmind/smind/issues/567)) ([a60a201](https://github.com/spacingmind/smind/commit/a60a2012faa72e1f0950f430765133f468a56f61))
* **wsapi:** forge status and GitHub token login over a secure channel (ADR-0039 G12 daemon) ([#547](https://github.com/spacingmind/smind/issues/547)) ([1e3a56f](https://github.com/spacingmind/smind/commit/1e3a56fce18df856596413d7d01f61122c5409aa))
* **wsapi:** run.start takes a human-only notBefore (holds the prompt in the queue) ([#573](https://github.com/spacingmind/smind/issues/573)) ([55e9106](https://github.com/spacingmind/smind/commit/55e910656cb85c969630379dd8e496f4d05f65da))
* **wsapi:** task.prDetails, task.prDetailsFocus, task.prCheckLog (G9 daemon) ([#588](https://github.com/spacingmind/smind/issues/588)) ([76148c8](https://github.com/spacingmind/smind/commit/76148c894276500aa56cdc149b8706b96c0d43d8))
* **wsapi:** task.push and task.updateFromBase (G7 daemon) ([#582](https://github.com/spacingmind/smind/issues/582)) ([192af31](https://github.com/spacingmind/smind/commit/192af3145e68aaa912de95baf529974b9f5b5611))


### Bug Fixes

* **attach:** full decode for png/jpeg/gif bodies within a pixel budget; cleanup tests for space/workspace delete ([#474](https://github.com/spacingmind/smind/issues/474)) ([42e3c86](https://github.com/spacingmind/smind/commit/42e3c86a69f2ec0b74043de1aab79fa8782475e9))
* **cli:** drop runSummaryRow fields declared twice after [#506](https://github.com/spacingmind/smind/issues/506) and [#507](https://github.com/spacingmind/smind/issues/507) ([#513](https://github.com/spacingmind/smind/issues/513)) ([e9583ec](https://github.com/spacingmind/smind/commit/e9583ec585cba539b0e1976b728372e17b2fe9c1))
* **cli:** stop concurrent task wait tests racing on os.Stdout/os.Stderr ([#626](https://github.com/spacingmind/smind/issues/626)) ([f5ae697](https://github.com/spacingmind/smind/commit/f5ae697d23be8f61b91c9be38732aa02745cbecd))
* **desktop:** an unavailable global shortcut no longer stops the app starting ([#591](https://github.com/spacingmind/smind/issues/591)) ([0f0b91b](https://github.com/spacingmind/smind/commit/0f0b91bbafd36423816acfb96d9928ece3b7f032))
* **desktop:** keep the proxy port across launches so UI preferences persist ([#590](https://github.com/spacingmind/smind/issues/590)) ([75db627](https://github.com/spacingmind/smind/commit/75db627b5eb553ce855e71fb812a778d64480250))
* **desktop:** log at info in release builds, never the WS frames or token ([#589](https://github.com/spacingmind/smind/issues/589)) ([d53797f](https://github.com/spacingmind/smind/commit/d53797f1f4234937f974fa835a72e0e971058327))
* **desktop:** Restart/Update wait for the old daemon to exit; reap the daemon child ([#601](https://github.com/spacingmind/smind/issues/601)) ([c3fa1ee](https://github.com/spacingmind/smind/commit/c3fa1eeeaa63d0029b02aab32f2da0f729357546))
* **desktop:** start the managed daemon with the user's login-shell PATH ([#484](https://github.com/spacingmind/smind/issues/484)) ([dcdcd30](https://github.com/spacingmind/smind/commit/dcdcd300612f102a3ccde28cbf590e0304dbe762))
* **install:** --desktop refuses only when the app being replaced is running ([#592](https://github.com/spacingmind/smind/issues/592)) ([0c8aa21](https://github.com/spacingmind/smind/commit/0c8aa2112ed654f729dc16ec5580e31b3b847b3d))
* **mobile:** every run attach shows the run's own prompt ([#501](https://github.com/spacingmind/smind/issues/501)) ([9473336](https://github.com/spacingmind/smind/commit/94733360ab504a62bbfca6961cb0646243042089))
* **mobile:** first-prompt bubble on a live run, expo-dev-client, SDK 58 patch drift ([#490](https://github.com/spacingmind/smind/issues/490)) ([b0999d9](https://github.com/spacingmind/smind/commit/b0999d9c10bfcce24cc526482984a7038402e284))
* **runs:** a claude-native launch always passes an explicit --model, or is refused (ADR-0025) ([#633](https://github.com/spacingmind/smind/issues/633)) ([40d4919](https://github.com/spacingmind/smind/commit/40d49195dee441a872ce504f2ddbcd53fef2bac2))
* **runs:** a failed run's error leads with the agent's own cause ([#505](https://github.com/spacingmind/smind/issues/505)) ([88eda2a](https://github.com/spacingmind/smind/commit/88eda2aee8beb2cb2afcc5678a4ab9679ff2feef))
* **runs:** a GLM rate-limit reset time is Asia/Shanghai (UTC+8) wall time, not the daemon's zone ([#553](https://github.com/spacingmind/smind/issues/553)) ([629a84e](https://github.com/spacingmind/smind/commit/629a84e0d294d9649a4785667a7c7f4e857df06b))
* **runs:** a run cut off by a daemon restart says so and can be continued ([#529](https://github.com/spacingmind/smind/issues/529)) ([d7a6de5](https://github.com/spacingmind/smind/commit/d7a6de587f1fe9d35e965d6adedb613f7fc762b9))
* **runs:** a run woken by a child event keeps its parent chat's profile ([#569](https://github.com/spacingmind/smind/issues/569)) ([64b520a](https://github.com/spacingmind/smind/commit/64b520a1a89d7f33b486f86a9d470fedff8949d8))
* **runs:** Claude Code's 'You've hit your session limit' refusal is a rate-limited error, not a done run ([#575](https://github.com/spacingmind/smind/issues/575)) ([f445bb3](https://github.com/spacingmind/smind/commit/f445bb38bef6d704c8c643a4fb2c6d26610e937c))
* **runs:** drain the claude CLI's stderr before building the cli_exited error ([#637](https://github.com/spacingmind/smind/issues/637)) ([45ed031](https://github.com/spacingmind/smind/commit/45ed031e438982967e257510d48ab4922f6add50))
* **runs:** persist how a permission was resolved ([#522](https://github.com/spacingmind/smind/issues/522)) ([69e8aff](https://github.com/spacingmind/smind/commit/69e8affab970dc86cc221f47b8bc51ebbeac4522))
* **runs:** say a failed run's cause once ([#525](https://github.com/spacingmind/smind/issues/525)) ([492847f](https://github.com/spacingmind/smind/commit/492847f6166321b252ed66c4efecd9b9555871fe))
* **runs:** the chat/provider binding error names the fix ([#510](https://github.com/spacingmind/smind/issues/510)) ([9f50d43](https://github.com/spacingmind/smind/commit/9f50d439b36717ad6fc754c5706bc3901740d58c))
* **runs:** type a claude CLI exit without a result as cli_exited, keep its partial usage ([#619](https://github.com/spacingmind/smind/issues/619)) ([3e53851](https://github.com/spacingmind/smind/commit/3e53851f5a769b1197d48f6af420247a34d836f5))
* show the reconnect state at once and freeze live affordances while disconnected (U14) ([#560](https://github.com/spacingmind/smind/issues/560)) ([53b916e](https://github.com/spacingmind/smind/commit/53b916e13192139797fc8fb730a036ba2f86188c))
* **sync:** G7 follow-ups (sentences, rebase reason, sync latch, PR-head push) ([#585](https://github.com/spacingmind/smind/issues/585)) ([2871c31](https://github.com/spacingmind/smind/commit/2871c31eb8e905ca88c43553d5628411c6e26793))
* **taskrunner:** a resumed claude-native run no longer ends at a stale background-task result ([#523](https://github.com/spacingmind/smind/issues/523)) ([b5b628e](https://github.com/spacingmind/smind/commit/b5b628eae76864adae34a2382bd90c6a7816bace))
* **taskrunner:** claude-native permission prompts reach the daemon ([#502](https://github.com/spacingmind/smind/issues/502)) ([e63f2a2](https://github.com/spacingmind/smind/commit/e63f2a2378688f98aaf47c099080c80ae14c2aab))
* **taskrunner:** claude-native reports each turn's own usage, not the session's running total ([#540](https://github.com/spacingmind/smind/issues/540)) ([5c2749b](https://github.com/spacingmind/smind/commit/5c2749bcb973b6620591467e36d47f45441c0044))
* **test:** keep the probe provider-env test off Windows ([#607](https://github.com/spacingmind/smind/issues/607)) ([6fe6064](https://github.com/spacingmind/smind/commit/6fe6064ff851ae63eef093dbcc3de3926ccaab78))
* **ui:** a denied permission no longer reads 'You approved' ([#518](https://github.com/spacingmind/smind/issues/518)) ([d8a6eb5](https://github.com/spacingmind/smind/commit/d8a6eb5c9e62cd3abaf59a55787d7cd5981acbf4))
* **ui:** a new terminal takes keyboard focus and uses the UI monospace font ([#526](https://github.com/spacingmind/smind/issues/526)) ([71ffa69](https://github.com/spacingmind/smind/commit/71ffa69e880029d8599d60218eb5bf2a1d39f000))
* **ui:** a run blocked on a permission says it is waiting for the user ([#519](https://github.com/spacingmind/smind/issues/519)) ([d40dda7](https://github.com/spacingmind/smind/commit/d40dda76a301133cfafc647f1544324d1907f2c2))
* **ui:** keep a followed chat pinned to its tail when content grows ([#514](https://github.com/spacingmind/smind/issues/514)) ([f44c440](https://github.com/spacingmind/smind/commit/f44c440e226cb0eafd46f946266952c4bf65ea6d))
* **ui:** one header per file in the by-file diff; no commit bar in whole-diff mode (dogfood U4, U5) ([#533](https://github.com/spacingmind/smind/issues/533)) ([e341c55](https://github.com/spacingmind/smind/commit/e341c55d2e35dfa60cfbf33bb54fd87a2382753c))
* **ui:** picking a task in the compact sidebar sheet closes it ([#511](https://github.com/spacingmind/smind/issues/511)) ([149cc16](https://github.com/spacingmind/smind/commit/149cc164d7094451c045fcdecdb6febb8840190a))
* **ui:** provider health asks whether a run can start, not whether an account exists ([#536](https://github.com/spacingmind/smind/issues/536)) ([b26dfe3](https://github.com/spacingmind/smind/commit/b26dfe3f5538472f3120e50c17af089477eac49d))
* **ui:** review comments sit under the line they are about ([#534](https://github.com/spacingmind/smind/issues/534)) ([4518e47](https://github.com/spacingmind/smind/commit/4518e472f8878068392ff6bf6a65e0c2feb53947))
* **ui:** submitting a review confirms it and shows the chat that received it ([#535](https://github.com/spacingmind/smind/issues/535)) ([c5bc30b](https://github.com/spacingmind/smind/commit/c5bc30b314aaa0d66c794ae0ad8523dd53f87c1c))
* **ui:** terminal header title, hover-card status, one-row usage chips (U23, U28, U30) ([#562](https://github.com/spacingmind/smind/issues/562)) ([e43fd65](https://github.com/spacingmind/smind/commit/e43fd65e7b693f0d916c03db1da188b60d00b406))
* **ui:** the Attach menu hands off to the picker and mention list with no gap ([#491](https://github.com/spacingmind/smind/issues/491)) ([8a788bf](https://github.com/spacingmind/smind/commit/8a788bfa193e7bc77aa1095f2644583d8be78f63))
* **ui:** the composer chip says Waiting for you while a permission is unanswered (U26) ([#542](https://github.com/spacingmind/smind/issues/542)) ([ca473ce](https://github.com/spacingmind/smind/commit/ca473ce70f39a143962885bfd311dc098d03a504))
* **ui:** the diff defaults to unified on a phone-width window ([#527](https://github.com/spacingmind/smind/issues/527)) ([18bac01](https://github.com/spacingmind/smind/commit/18bac01817b6781fb5e9a787df8b525d05032b51))
* **ui:** the lightbox close button is visible on any image; a subtler drop cue (dogfood U8, U27) ([#532](https://github.com/spacingmind/smind/issues/532)) ([1f04bcc](https://github.com/spacingmind/smind/commit/1f04bccd4e53d7e27d8a0c998d8900c1d2e963d4))
* **ui:** workspace/space row menus close without an exit animation when an item opens a dialog ([#475](https://github.com/spacingmind/smind/issues/475)) ([164594a](https://github.com/spacingmind/smind/commit/164594a126cb7c42a0970dc5e55f9bac94306f4e))
* **web:** built-in GLM and Kimi keep the agent glyph; generic icon only for unknown providers ([#629](https://github.com/spacingmind/smind/issues/629)) ([5ed6bc2](https://github.com/spacingmind/smind/commit/5ed6bc259ddf682cd8a155b3749d55a644edd2a0))
* **web:** evidence-pass follow-ups: narrow header, quiet PR refresh, vanished PR, probe table/estimate, picker, dialog ([#614](https://github.com/spacingmind/smind/issues/614)) ([18feb7f](https://github.com/spacingmind/smind/commit/18feb7f155baadeae9e8700c61ed594c4a54fe4b))
* **web:** G18 stacked-task UX polish (base switch, PR base, hover, mobile, provider readiness) ([#611](https://github.com/spacingmind/smind/issues/611)) ([458541c](https://github.com/spacingmind/smind/commit/458541c2ea867814731f5e71a0ca607ab73718f0))
* **web:** Push card Remote row shows the real push target (origin.pushTarget) ([#587](https://github.com/spacingmind/smind/issues/587)) ([31db15c](https://github.com/spacingmind/smind/commit/31db15ce2e9e45313523604ef593f87473113044))
* **web:** screenshot evidence pass for attachments, probe card, task tiers (+3 390px/flash fixes) ([#613](https://github.com/spacingmind/smind/issues/613)) ([0702d8c](https://github.com/spacingmind/smind/commit/0702d8c6ec426a7bccc39889daa2d35219a546e6))
* **web:** screenshot evidence pass for pr-panel, tree-budget, profile editor (+2 fade-out fixes) ([#612](https://github.com/spacingmind/smind/issues/612)) ([770fcce](https://github.com/spacingmind/smind/commit/770fcce05742c0a0ec46f069444c799573b31272))
* **workspace:** cap task slug length at 40 ([#492](https://github.com/spacingmind/smind/issues/492)) ([e57a64e](https://github.com/spacingmind/smind/commit/e57a64ec516359719177f920deb5590684d5f277))

## [0.14.0](https://github.com/spacingmind/smind/compare/v0.13.1...v0.14.0) (2026-10-22)


### Features

* **attach:** prompt attachments on the daemon (ADR-0038 PR 2) ([#463](https://github.com/spacingmind/smind/issues/463)) ([8cb01c1](https://github.com/spacingmind/smind/commit/8cb01c1cdb9d7589951dc925dfaed34e9bdd92fe))
* **forge:** gh client and forge.search RPC (ADR-0038 §4) ([#459](https://github.com/spacingmind/smind/issues/459)) ([c023a15](https://github.com/spacingmind/smind/commit/c023a15496e575300b008cc27dcc20641899abc9))
* **learning:** one-shot reflector retry with gate findings (ADR-0037 D11) ([#442](https://github.com/spacingmind/smind/issues/442)) ([06e9e84](https://github.com/spacingmind/smind/commit/06e9e84b75a67be0efbf98bec69dd399a3bcd624))
* **learning:** prevalence scan for pending proposals (ADR-0037 stage 0) ([#441](https://github.com/spacingmind/smind/issues/441)) ([4aa4282](https://github.com/spacingmind/smind/commit/4aa4282d70128f3b3fc07c606b3f48658278c9df))
* **learning:** probe arm, sandbox and attested projection (ADR-0037 4a) ([#450](https://github.com/spacingmind/smind/issues/450)) ([f9ade77](https://github.com/spacingmind/smind/commit/f9ade77f4769804e7459a95573919979e194346a))
* **learning:** probe service, wire API and CLI (ADR-0037 4b) ([#451](https://github.com/spacingmind/smind/issues/451)) ([#452](https://github.com/spacingmind/smind/issues/452)) ([2735e52](https://github.com/spacingmind/smind/commit/2735e52d5f43348e3e1bdea7260ef1a6bd56f751))
* **mobile:** composer, queue strip and Stop/Send in the ZCode look (M5) ([#460](https://github.com/spacingmind/smind/issues/460)) ([2dc4f11](https://github.com/spacingmind/smind/commit/2dc4f11daec567d237d336deefb846eb3a038aaf))
* **mobile:** Expo Router native stack (ADR-0036) ([#436](https://github.com/spacingmind/smind/issues/436)) ([a36d543](https://github.com/spacingmind/smind/commit/a36d543210ecfc3dca55c559221efd8f4ccafb25))
* **mobile:** foundations — Zai tokens, UiHost tint, UI primitives, icon registry, modular smoke ([#449](https://github.com/spacingmind/smind/issues/449)) ([a865784](https://github.com/spacingmind/smind/commit/a865784f380076a43367f933f4ca3e0032cba6d3))
* **mobile:** pairing, status cards and password prompt in the ZCode look (M2) ([#454](https://github.com/spacingmind/smind/issues/454)) ([f0b157f](https://github.com/spacingmind/smind/commit/f0b157f19a5df820cbef73fed4cec1475b8b2bdc))
* **mobile:** task list in the ZCode look with a connection sheet (M3) ([#455](https://github.com/spacingmind/smind/issues/455)) ([1c0edd0](https://github.com/spacingmind/smind/commit/1c0edd08096dffb51f1861608325b3b231170a80))
* **mobile:** task transcript in the ZCode look (M4) ([#453](https://github.com/spacingmind/smind/issues/453)) ([0ca95de](https://github.com/spacingmind/smind/commit/0ca95de2bb77dd755974694a5b8954cd11bba4aa))
* **ui:** learning probe card (ADR-0037 4c) ([#448](https://github.com/spacingmind/smind/issues/448)) ([c0efd7e](https://github.com/spacingmind/smind/commit/c0efd7e3aed7921fab09870bcecadeb0d910bb12))
* **ui:** prompt attachments in the composer and timeline (ADR-0038 6a) ([#465](https://github.com/spacingmind/smind/issues/465)) ([15ebcd0](https://github.com/spacingmind/smind/commit/15ebcd06ca2b6915230d20a62a17612a8dea0569))


### Bug Fixes

* **desktop:** clippy --all-targets -D warnings passes; run it in desktop-check ([#470](https://github.com/spacingmind/smind/issues/470)) ([dac1a67](https://github.com/spacingmind/smind/commit/dac1a671329d1e5eb469e34e9e8f743175d0e9b5))
* **desktop:** let the webview receive dropped files ([#468](https://github.com/spacingmind/smind/issues/468)) ([03019ec](https://github.com/spacingmind/smind/commit/03019eceb0529a8ad4680c517b8ed1e2a4af4b61))
* **learning:** announce a job as queued before its goroutine can announce running ([#447](https://github.com/spacingmind/smind/issues/447)) ([121edd0](https://github.com/spacingmind/smind/commit/121edd066da1ad99956b19b51e8f1cf8fff4d523))
* **learning:** probe failed by restart explains itself; one dollar format on the probe card ([#456](https://github.com/spacingmind/smind/issues/456)) ([faf155e](https://github.com/spacingmind/smind/commit/faf155e7af60967d18bb8c89c17e8bf2fa1d4b0a))
* **ui:** diff pane quality — gutters, dark syntax, narrow header, errors, review routing ([#462](https://github.com/spacingmind/smind/issues/462)) ([5b90afa](https://github.com/spacingmind/smind/commit/5b90afae47769b54a0016783a02721bd242dcb5e))

## [0.13.1](https://github.com/spacingmind/smind/compare/v0.13.0...v0.13.1) (2026-10-20)


### Bug Fixes

* **docker:** pin the relay fingerprint in the image's relay-connect test ([#443](https://github.com/spacingmind/smind/issues/443)) ([9fe47e2](https://github.com/spacingmind/smind/commit/9fe47e2ca06af8ffbfbf70c25142db39abb74b65))

## [0.13.0](https://github.com/spacingmind/smind/compare/v0.12.0...v0.13.0) (2026-10-20)


### ⚠ BREAKING CHANGES

* **mobile:** E2EE v2 in the TypeScript relay client (ADR-0035) ([#434](https://github.com/spacingmind/smind/issues/434))
* **desktop:** E2EE v2 in the Rust relay client (ADR-0035) ([#433](https://github.com/spacingmind/smind/issues/433))
* **relay:** E2EE v1 is removed; v1 and v2 peers refuse each other.

### Features

* **accounts:** loginId and account.oauthComplete paste-the-redirect (ADR-0033 B2) ([#389](https://github.com/spacingmind/smind/issues/389)) ([011a8ec](https://github.com/spacingmind/smind/commit/011a8ecefb93d5bbe4c064f24302dd3e97c75826))
* **accounts:** OpenAI device-code login, listener hardening and paste-only fallback (ADR-0033 B3) ([#396](https://github.com/spacingmind/smind/issues/396)) ([a0cba25](https://github.com/spacingmind/smind/commit/a0cba254b1dd6364b46de54c037cd67118d8b6ec))
* **accounts:** stable account identity and in-place re-login (ADR-0033 B1) ([#385](https://github.com/spacingmind/smind/issues/385)) ([cace011](https://github.com/spacingmind/smind/commit/cace0114fbbac89bd96d5f7c38bbef2d9cedd860))
* **cli:** smind service for systemd and launchd (ADR-0031 P8) ([#348](https://github.com/spacingmind/smind/issues/348)) ([0714031](https://github.com/spacingmind/smind/commit/07140310cc635aa516b5649e47669aebb7cd023a))
* **cli:** smind service for Windows via a per-user scheduled task (ADR-0031 P9) ([#353](https://github.com/spacingmind/smind/issues/353)) ([f82dceb](https://github.com/spacingmind/smind/commit/f82dceb6faddddf866c22ca5b1e44627ed6a435c))
* **cli:** smind task mode for humans (D12) ([#369](https://github.com/spacingmind/smind/issues/369)) ([96aa2ae](https://github.com/spacingmind/smind/commit/96aa2ae872d332d32186b97b6ebe2cf919fcb0ee))
* daemon.info and an older-daemon notice (ADR-0031 P10) ([#356](https://github.com/spacingmind/smind/issues/356)) ([a2a73b5](https://github.com/spacingmind/smind/commit/a2a73b5d13434ae665b5e1b3f1175deee2a69764))
* **desktop:** connect to relays with a CA-issued certificate (ADR-0031 P12c follow-up) ([#363](https://github.com/spacingmind/smind/issues/363)) ([3a0d6f5](https://github.com/spacingmind/smind/commit/3a0d6f5cb990729df52cd14d6b5f6540e35078d4))
* **desktop:** E2EE v2 in the Rust relay client (ADR-0035) ([#433](https://github.com/spacingmind/smind/issues/433)) ([8437454](https://github.com/spacingmind/smind/commit/8437454bd2f6c0c1f58981a7e520ae9b35bf65d8))
* **desktop:** first-run and connection onboarding (ADR-0031 P5) ([#357](https://github.com/spacingmind/smind/issues/357)) ([b517e53](https://github.com/spacingmind/smind/commit/b517e53d22988c5bc7f89fa6229fb3db7108f70a))
* **desktop:** relay device identity, keepalive and multi-device (ADR-0031 P13a) ([#351](https://github.com/spacingmind/smind/issues/351)) ([b59cc8a](https://github.com/spacingmind/smind/commit/b59cc8a3bc51bcadbae9683aecbce80a0795e2a1))
* direct connections with a daemon password and smind config (ADR-0031 P4) ([#349](https://github.com/spacingmind/smind/issues/349)) ([9bc05fb](https://github.com/spacingmind/smind/commit/9bc05fbe4e918533436fcc93dd10cff89c755dc8))
* Docker image for homelab daemons (ADR-0031 P11) ([#358](https://github.com/spacingmind/smind/issues/358)) ([769c18a](https://github.com/spacingmind/smind/commit/769c18ae3a9ac6709ce5cddcf75bb454d2b85be4))
* **learning:** approval service and landing re-check (ADR-0026 B4a) ([#405](https://github.com/spacingmind/smind/issues/405)) ([9f1cf4e](https://github.com/spacingmind/smind/commit/9f1cf4e2e8b4674bdf7a5569d6b37420b5f38544))
* **learning:** collector, reflect job pipeline and daemon wiring (ADR-0026 B2d) ([#413](https://github.com/spacingmind/smind/issues/413)) ([8d1fc4b](https://github.com/spacingmind/smind/commit/8d1fc4b1cdf1d6344bf8dde1f2eb3ebf9aeb471d))
* **learning:** connect collector, delivery, wsapi; budget in status; export RPC and CLI (ADR-0026 B6) ([#418](https://github.com/spacingmind/smind/issues/418)) ([06a0500](https://github.com/spacingmind/smind/commit/06a050021593fc1563a838ac5c3b78494d51111c))
* **learning:** curator job and library snapshots (ADR-0026 B5b) ([#417](https://github.com/spacingmind/smind/issues/417)) ([2da418b](https://github.com/spacingmind/smind/commit/2da418baad2ed8a069dbffbfc9cabc9d80a110a0))
* **learning:** deterministic gate, similarity check, human-skill loader (ADR-0026 B1c) ([#394](https://github.com/spacingmind/smind/issues/394)) ([adbd009](https://github.com/spacingmind/smind/commit/adbd009d010cee1201223ef90e5c5f4405a2aac3))
* **learning:** explicit skill export with symlink-safe install (ADR-0026 B5a) ([#410](https://github.com/spacingmind/smind/issues/410)) ([88a590e](https://github.com/spacingmind/smind/commit/88a590e42fc44470273ac3196f6ae1b9080862e4))
* **learning:** learned-skill delivery, load counting and lifecycle application (ADR-0026 B3b) ([#414](https://github.com/spacingmind/smind/issues/414)) ([ddf7d8c](https://github.com/spacingmind/smind/commit/ddf7d8ca402de82ac314072d56439fdd4e8efc43))
* **learning:** read-only projection, ACP preface and load matcher (ADR-0026 B3a) ([#406](https://github.com/spacingmind/smind/issues/406)) ([4203507](https://github.com/spacingmind/smind/commit/42035074dcf726c9638ee95bc628fd5440192d1d))
* **learning:** redactor and learning config section (ADR-0026 B1b) ([#393](https://github.com/spacingmind/smind/issues/393)) ([162dbaf](https://github.com/spacingmind/smind/commit/162dbafc43439ab79661e4899c2ae99f1608520d))
* **learning:** reflector/curator prompts and strict output parser (ADR-0026 B2e) ([#403](https://github.com/spacingmind/smind/issues/403)) ([f2f544c](https://github.com/spacingmind/smind/commit/f2f544cddc1627f981ff5d4a9947b00061b8c8bc))
* **learning:** run origin persistence, isolated start option and executor (ADR-0026 B2f) ([#408](https://github.com/spacingmind/smind/issues/408)) ([e340314](https://github.com/spacingmind/smind/commit/e340314337c78efc29bea0779aa80f0866deb375))
* **learning:** run purpose, hidden system tasks, run-finished observer (ADR-0026 B2b) ([#401](https://github.com/spacingmind/smind/issues/401)) ([897920a](https://github.com/spacingmind/smind/commit/897920a9d1f086bca0ae2be95531d3666ab2a148))
* **learning:** store schema, ledger/versions, lifecycle function (ADR-0026 B1a) ([#392](https://github.com/spacingmind/smind/issues/392)) ([152c67c](https://github.com/spacingmind/smind/commit/152c67ca4e081480429d30111b6519c9fb9b93c3))
* **learning:** window builder and eligibility predicates (ADR-0026 B2a) ([#400](https://github.com/spacingmind/smind/issues/400)) ([90a6750](https://github.com/spacingmind/smind/commit/90a675060fb4e7a727aa6b5b940a8ebec9f54890))
* **learning:** wsapi RPCs, lifecycle events and smind learn CLI (ADR-0026 B4b) ([#411](https://github.com/spacingmind/smind/issues/411)) ([6809aed](https://github.com/spacingmind/smind/commit/6809aed82c004c5c345dc70001fe14337930bdec))
* **mobile:** E2EE v2 in the TypeScript relay client (ADR-0035) ([#434](https://github.com/spacingmind/smind/issues/434)) ([5f3f352](https://github.com/spacingmind/smind/commit/5f3f35208ac9e28bfba84f9c3c743a540c267149))
* **mobile:** prompt for the daemon password on relay connections (ADR-0031 P13c) ([#362](https://github.com/spacingmind/smind/issues/362)) ([9d83182](https://github.com/spacingmind/smind/commit/9d831827d7f835b7ff79944086703a2353284d87))
* **mobile:** relay device identity, keepalive and multi-device (ADR-0031 P13b) ([#350](https://github.com/spacingmind/smind/issues/350)) ([fa22f59](https://github.com/spacingmind/smind/commit/fa22f594613d375c234257e795b86cf9b18466d0))
* **mobile:** show the needs-continue state and a Continue action for capped runs (ADR-0027 D13) ([#379](https://github.com/spacingmind/smind/issues/379)) ([b8b26ca](https://github.com/spacingmind/smind/commit/b8b26caf044868932ba04e624cb9bb7fc395da95))
* **mobile:** stop a live run; show delivered queued prompts ([#387](https://github.com/spacingmind/smind/issues/387)) ([d4b3647](https://github.com/spacingmind/smind/commit/d4b364766cf0f14a780b2b6428df83682cfe0b7f))
* **mobile:** use the daemon prompt queue while a run is live ([#383](https://github.com/spacingmind/smind/issues/383)) ([46d71c8](https://github.com/spacingmind/smind/commit/46d71c83f069b3e9de57032d66cacb07b9479393))
* **palette:** open the daemon settings sections from the palette ([#375](https://github.com/spacingmind/smind/issues/375)) ([5c9ddce](https://github.com/spacingmind/smind/commit/5c9ddceda4f1cc9a4c842962cb93d8b1d5a4ca41))
* **relay:** CA-verified TLS for relay clients and no silent trust-on-first-use (ADR-0031 P12c) ([#361](https://github.com/spacingmind/smind/issues/361)) ([eecd40b](https://github.com/spacingmind/smind/commit/eecd40bc30619de6bc7321813a19d786859a9b51))
* **relay:** E2EE v2 Go core — fresh daemon ephemeral key (ADR-0035) ([#431](https://github.com/spacingmind/smind/issues/431)) ([87b7496](https://github.com/spacingmind/smind/commit/87b7496d09a5ba0a21d75124597c0c43f06773bd))
* **relay:** require the daemon password from relay devices (ADR-0031 P13c) ([#360](https://github.com/spacingmind/smind/issues/360)) ([660080a](https://github.com/spacingmind/smind/commit/660080adba6fa0a2e909b5e071112132d5d7dcfc))
* **runs:** derived needs-continue disposition (D13) ([#373](https://github.com/spacingmind/smind/issues/373)) ([ff5a182](https://github.com/spacingmind/smind/commit/ff5a18220000b64279472a3516c39c2688606c74))
* **runs:** record base/head sha and dirty state for each run (O1) ([#367](https://github.com/spacingmind/smind/issues/367)) ([b230b83](https://github.com/spacingmind/smind/commit/b230b8303e4ddabf6c70f15e987942cfa321826b))
* **store:** SQLite WAL mode with synchronous=NORMAL ([#424](https://github.com/spacingmind/smind/issues/424)) ([ff7e3fc](https://github.com/spacingmind/smind/commit/ff7e3fca311aa8181ff865e80da4b4056c567a37))
* **taskrunner:** isolated no-tools claude-native run (ADR-0026 B2c) ([#402](https://github.com/spacingmind/smind/issues/402)) ([db2ef6a](https://github.com/spacingmind/smind/commit/db2ef6acaa675661e059d54168f15ae9a4a6917a))
* **tasks:** archive finished descendants and stop a whole task tree (H11) ([#376](https://github.com/spacingmind/smind/issues/376)) ([bc37b09](https://github.com/spacingmind/smind/commit/bc37b09f982f4425872a315bab251e24b6b96353))
* **ui:** export a learned skill and show the learning budget (ADR-0026 B4d) ([#420](https://github.com/spacingmind/smind/issues/420)) ([12bd4a0](https://github.com/spacingmind/smind/commit/12bd4a016ed4aa179c02c61f295ef580c7ced518))
* **ui:** learned skills inbox, detail and settings with the possible-duplicate view (ADR-0026 B4c) ([#412](https://github.com/spacingmind/smind/issues/412)) ([b94d5dd](https://github.com/spacingmind/smind/commit/b94d5ddc91dee5f3020d2b98478c4e7ac8ab87d4))
* **ui:** provider catalog, profiles and health on the shared query cache (ADR-0032 S3) ([#407](https://github.com/spacingmind/smind/issues/407)) ([2a783d6](https://github.com/spacingmind/smind/commit/2a783d64f6bd3236cbff2b4430708757bc30e519))
* **ui:** shared run-config pickers and one describeProfile (ADR-0032 S4) ([#415](https://github.com/spacingmind/smind/issues/415)) ([e0ba6af](https://github.com/spacingmind/smind/commit/e0ba6afa5432bbcaf6fe50a5d88907c441c5c869))
* **ui:** shared status, error, confirm and empty/loading primitives (ADR-0032 S5) ([#422](https://github.com/spacingmind/smind/issues/422)) ([9a6cf4a](https://github.com/spacingmind/smind/commit/9a6cf4a923379b3eeced079c6195adb88168a024))
* **ui:** TanStack Query foundation; accounts and plan usage share one cache (ADR-0032) ([#386](https://github.com/spacingmind/smind/issues/386)) ([4822d30](https://github.com/spacingmind/smind/commit/4822d302df8a1ef97a569f32a4f640108271fd31))
* **ui:** zustand preference stores, one formatRelativeTime and a shared workspace tree (ADR-0032 S6) ([#425](https://github.com/spacingmind/smind/issues/425)) ([a97f373](https://github.com/spacingmind/smind/commit/a97f373b39236d716e5875fc717adf2d5832f20b))
* **web:** connect form with sign-in methods, device code and paste-the-redirect (ADR-0033 B4) ([#409](https://github.com/spacingmind/smind/issues/409)) ([215e6fb](https://github.com/spacingmind/smind/commit/215e6fb0215435d7dae5cf59e74c382cc1659ed1))
* **web:** Providers tab redesign — master/detail, health badges, connect and re-login (ADR-0033 A1) ([#416](https://github.com/spacingmind/smind/issues/416)) ([682d3eb](https://github.com/spacingmind/smind/commit/682d3ebc6fc6ae33956c7a3be3ead8f9effa1e24))


### Bug Fixes

* **accounts:** Anthropic identity includes the organization uuid (ADR-0033 D6) ([#404](https://github.com/spacingmind/smind/issues/404)) ([c0be48c](https://github.com/spacingmind/smind/commit/c0be48c21559f6e88c2582e04505dd386abe99af))
* **cli:** race-free stdBuffer in serve tests ([#364](https://github.com/spacingmind/smind/issues/364)) ([3d0d7c9](https://github.com/spacingmind/smind/commit/3d0d7c93f92fb764da1ea24c2622e59fd6815468))
* **desktop:** keep connections the app doesn't understand instead of resetting the file ([#382](https://github.com/spacingmind/smind/issues/382)) ([55198f4](https://github.com/spacingmind/smind/commit/55198f40574bf6a6cbffd2697f1d950122566e43))
* **desktop:** open provider login pages and external links in the system browser ([#370](https://github.com/spacingmind/smind/issues/370)) ([6da752f](https://github.com/spacingmind/smind/commit/6da752f0b591d0f7dc78e1f709d520ef6a875021))
* **desktop:** verify an SSH tunnel's local port belongs to our ssh ([#381](https://github.com/spacingmind/smind/issues/381)) ([9ad9838](https://github.com/spacingmind/smind/commit/9ad98385147d13221878b4a5ac3172a5fb66db7c))
* **learning:** isolated run accepts the CLI's built-in plugins at init ([#426](https://github.com/spacingmind/smind/issues/426)) ([12d9ebd](https://github.com/spacingmind/smind/commit/12d9ebdc9bb3aee63b091a04c326aafd3dd505ad))
* **learning:** pin the Haiku model id; meter failed runs and cache tokens ([#428](https://github.com/spacingmind/smind/issues/428)) ([e7ae521](https://github.com/spacingmind/smind/commit/e7ae5217a03d11d8ef3904c238e7a97b4cbc6a50))
* **mobile:** explicit CSPRNG for relay keys and nonces ([#437](https://github.com/spacingmind/smind/issues/437)) ([3e6d030](https://github.com/spacingmind/smind/commit/3e6d030d4589ddfe8af4e442d9710fe7e44aa103))
* **relay:** bind device_hello to a daemon challenge; answer unknown methods ([#354](https://github.com/spacingmind/smind/issues/354)) ([f25fafb](https://github.com/spacingmind/smind/commit/f25fafb1773ef35b4446ade0ee260a5ee061ef45))
* **relay:** pin the relay's leaf certificate, not any certificate in the chain ([#355](https://github.com/spacingmind/smind/issues/355)) ([41d1154](https://github.com/spacingmind/smind/commit/41d115498721d03e5e6fa154533e376df5918ec5))
* **runs:** tree guard concurrency flakes ([#423](https://github.com/spacingmind/smind/issues/423)) ([eee600f](https://github.com/spacingmind/smind/commit/eee600f0a7d1adf6309b1ab7d8c491a02f4fa306))
* **service:** per-user Windows task, query errors, launchd enabled state, crash output in daemon.log ([#391](https://github.com/spacingmind/smind/issues/391)) ([cb5154e](https://github.com/spacingmind/smind/commit/cb5154ee1e5027577c2b6e8749775a39f823ba19))
* **test:** relay hello-replay and real-shell terminal flakes ([#397](https://github.com/spacingmind/smind/issues/397)) ([c78aaa6](https://github.com/spacingmind/smind/commit/c78aaa6ad1a1d6dd680d8f063e5811c8d4a83976))
* **test:** remove the root causes of recurring CI flakes ([#388](https://github.com/spacingmind/smind/issues/388)) ([8d5336d](https://github.com/spacingmind/smind/commit/8d5336df3d766bd547f795ed01191e80ab9b7c02))
* **windows:** make the nightly full suite pass on windows-latest ([#366](https://github.com/spacingmind/smind/issues/366)) ([9a773e5](https://github.com/spacingmind/smind/commit/9a773e52502793a90314df1f79175c32d28c0325))

## [0.12.0](https://github.com/spacingmind/smind/compare/v0.11.1...v0.12.0) (2026-10-15)


### Features

* **cli:** smind update with checksum verification (ADR-0031 P6) ([#341](https://github.com/spacingmind/smind/issues/341)) ([e25846c](https://github.com/spacingmind/smind/commit/e25846cf4c96be1dfa4a0997044743d6b3951840))
* **desktop:** add a connection from a relay pairing link (ADR-0031 P3) ([#342](https://github.com/spacingmind/smind/issues/342)) ([65906e5](https://github.com/spacingmind/smind/commit/65906e50fcf91949d2b1aa9f88e4008d11f604cd))
* **desktop:** bundle the native daemon in the Windows app, like macOS ([#334](https://github.com/spacingmind/smind/issues/334)) ([94c43b4](https://github.com/spacingmind/smind/commit/94c43b4a3c0209ccd3f7fbd7dc169126f95b7acb))
* install script fixes and install.ps1 for Windows (ADR-0031 P1) ([#339](https://github.com/spacingmind/smind/issues/339)) ([098299e](https://github.com/spacingmind/smind/commit/098299e16e4eacfc01cc24229b4c0647b8ffa89d))
* **relay:** "relay host" replaces the relay-side "workspace" wording (ADR-0031 P0) ([#338](https://github.com/spacingmind/smind/issues/338)) ([0e357b4](https://github.com/spacingmind/smind/commit/0e357b425a2d8e40782ef898b1627d04f9beeec2))
* **relay:** multi-device relay server, protocol additions and 3-device harness (ADR-0031 P12a) ([#345](https://github.com/spacingmind/smind/issues/345)) ([9a06ee4](https://github.com/spacingmind/smind/commit/9a06ee4ff39b7783c47aa4fa446701ce16f45c0f))
* **relay:** per-device sessions in the daemon bridge, device identity and smind relay devices (ADR-0031 P12b) ([#346](https://github.com/spacingmind/smind/issues/346)) ([b284c3c](https://github.com/spacingmind/smind/commit/b284c3c9b0d62e2944e4570149f0db01e6b311ae))
* **relay:** QR and PNG pairing offers + homelab quickstart (ADR-0031 P2) ([#340](https://github.com/spacingmind/smind/issues/340)) ([dadbedd](https://github.com/spacingmind/smind/commit/dadbedda6df1be6b1e0fa483342e801da6a9770b))
* **release:** sign checksums and verify them in smind update (ADR-0031 P7) ([#343](https://github.com/spacingmind/smind/issues/343)) ([d61c1a2](https://github.com/spacingmind/smind/commit/d61c1a234a305f3ae7021e21806b6ae2440a8b66))
* sMind as the product display name ([#336](https://github.com/spacingmind/smind/issues/336)) ([a9bcbf3](https://github.com/spacingmind/smind/commit/a9bcbf3d66b54c00e878738ead45eefa56592c9a))


### Bug Fixes

* **desktop:** relay pairings get a Windows-safe file name ([#344](https://github.com/spacingmind/smind/issues/344)) ([a0084ae](https://github.com/spacingmind/smind/commit/a0084aeb3970c1bab0de74b1282c7a3c2f6229b7))
* **desktop:** set the new relay Frame.keepalive field; build desktop on proto changes ([#347](https://github.com/spacingmind/smind/issues/347)) ([1d815cc](https://github.com/spacingmind/smind/commit/1d815cc59210b6626cb4cc23460b00b8238e3110))

## [0.11.1](https://github.com/spacingmind/smind/compare/v0.11.0...v0.11.1) (2026-10-13)


### Bug Fixes

* **desktop:** render update release notes as markdown ([#333](https://github.com/spacingmind/smind/issues/333)) ([9e76db5](https://github.com/spacingmind/smind/commit/9e76db53f10b96df56e91fe778e817c7e9068b86))
* **desktop:** traffic lights and caption buttons stay clear when the sidebar toggles ([#332](https://github.com/spacingmind/smind/issues/332)) ([f0cd6a2](https://github.com/spacingmind/smind/commit/f0cd6a217f849cb6ae58a2050143a14dba77583f))
* **web:** consistent icons via one registry ([#330](https://github.com/spacingmind/smind/issues/330)) ([e8f3064](https://github.com/spacingmind/smind/commit/e8f3064050719f6e52f2a319ca1a74ed18636801))

## [0.11.0](https://github.com/spacingmind/smind/compare/v0.10.0...v0.11.0) (2026-10-13)


### Features

* bundled orchestration skills + smind skill install|ls|uninstall ([#279](https://github.com/spacingmind/smind/issues/279)) ([90469fb](https://github.com/spacingmind/smind/commit/90469fbc3b1eb78280108cee6dd8fb67dbc833dd))
* default home dir is ~/.smind, with one-time migration from ~/.spacingmind ([#294](https://github.com/spacingmind/smind/issues/294)) ([9f71b49](https://github.com/spacingmind/smind/commit/9f71b49e618bbcbfc531ff8d47a3c06057d4a4e7))
* **desktop:** manage a native Windows daemon instead of WSL2 ([#311](https://github.com/spacingmind/smind/issues/311)) ([aaf0cd9](https://github.com/spacingmind/smind/commit/aaf0cd940e756439eda766f28b206bb72f3aaf44))
* **desktop:** signed auto-update via the Tauri 2 updater ([#274](https://github.com/spacingmind/smind/issues/274)) ([8b6ecf6](https://github.com/spacingmind/smind/commit/8b6ecf6c28cbf3adaab5463cec5dfcccb8f09c48))
* **mcp:** discovery tools (workspace/task/run/provider/profile) + server instructions ([#264](https://github.com/spacingmind/smind/issues/264)) ([6b4d15b](https://github.com/spacingmind/smind/commit/6b4d15b02cc9c072288cf9a063f85ebd082c8ff6))
* **mcp:** task_set_mode, task_options, task_set_option, task_wait wait-any (ADR-0019 [#6](https://github.com/spacingmind/smind/issues/6) guarded) ([#269](https://github.com/spacingmind/smind/issues/269)) ([45d7933](https://github.com/spacingmind/smind/commit/45d793331961e3e1ae5e361a08122f3c7bf98cc5))
* model selection on runs and profiles (ADR-0025) ([#272](https://github.com/spacingmind/smind/issues/272)) ([e4ec6b4](https://github.com/spacingmind/smind/commit/e4ec6b46e946e53fe0c96e2cd08c66f3db262338))
* native Windows daemon runtime parity (setup scripts, shutdown API, secret ACLs, claude.exe launcher) ([#310](https://github.com/spacingmind/smind/issues/310)) ([7a1dff2](https://github.com/spacingmind/smind/commit/7a1dff22f0c75a997198ed7f6f2b38129f0e6e22))
* per-run MCP servers indicator, light-theme logo tile, plan housekeeping ([#258](https://github.com/spacingmind/smind/issues/258)) ([5302f89](https://github.com/spacingmind/smind/commit/5302f8936dd446ce78f6b6557fc3e62b0de7c683))
* **queue:** structured cancel_kind on chat_queue items + Step 3 dogfood gap log ([#265](https://github.com/spacingmind/smind/issues/265)) ([c2d516d](https://github.com/spacingmind/smind/commit/c2d516d9f2a47fc34774518ef45f6b449b03895c))
* **quota:** account usage report types and Anthropic/ChatGPT fetchers (ADR-0028 PR-A1) ([#298](https://github.com/spacingmind/smind/issues/298)) ([3ea8f3b](https://github.com/spacingmind/smind/commit/3ea8f3b1140a9e872dbb1d435dc667a116774098))
* **quota:** passive rejection cooldown from upstream rate-limit headers (ADR-0028 PR-F) ([#308](https://github.com/spacingmind/smind/issues/308)) ([36aa220](https://github.com/spacingmind/smind/commit/36aa220770a2908427a54aaff39d532ace455849))
* **quota:** surface cooldowns in account.usage, CLI and UI; keep affinity on transient 429 ([#317](https://github.com/spacingmind/smind/issues/317)) ([86b7ca2](https://github.com/spacingmind/smind/commit/86b7ca25b6014998b07924db844f92bf539a7fc9))
* **quota:** usage poller with daemon-enforced call budget (ADR-0028 PR-A2) ([#302](https://github.com/spacingmind/smind/issues/302)) ([978a841](https://github.com/spacingmind/smind/commit/978a841944652f7055710d69e6a4923ae78bbe14))
* **release:** ship native Windows daemon binaries with every release ([#313](https://github.com/spacingmind/smind/issues/313)) ([38c2dd4](https://github.com/spacingmind/smind/commit/38c2dd4751a9f8ac55cdfadcf0ebe4574c79d7a5))
* **routing:** route around exhausted accounts from cached usage windows (ADR-0028 PR-B) ([#304](https://github.com/spacingmind/smind/issues/304)) ([15b89ca](https://github.com/spacingmind/smind/commit/15b89ca26baa07f73161b628854463387b17f768))
* **runs:** per-tree concurrent-run guard (orchestration.maxConcurrentRunsPerTree) ([#289](https://github.com/spacingmind/smind/issues/289)) ([26a31e1](https://github.com/spacingmind/smind/commit/26a31e1f53d482b51642007a1bc6c0483c03ab3f))
* task from base ref/branch/PR + task_archive with active-run guard ([#266](https://github.com/spacingmind/smind/issues/266)) ([d6dcd2a](https://github.com/spacingmind/smind/commit/d6dcd2a2456a44dc94bed965a39fc18c55fe0239))
* **web:** chat timeline rail and borderless messages ([#318](https://github.com/spacingmind/smind/issues/318)) ([f09df1b](https://github.com/spacingmind/smind/commit/f09df1b00d1b3870d76a73e5c44df49dce7988f7))
* **web:** combined provider + model picker (Paseo-style) ([#303](https://github.com/spacingmind/smind/issues/303)) ([4c8526c](https://github.com/spacingmind/smind/commit/4c8526cc591a6438b5282f5e4189160fb0180e7c))
* **web:** composer uses the server-side prompt queue (ADR-0021 §9) ([#259](https://github.com/spacingmind/smind/issues/259)) ([081c3a4](https://github.com/spacingmind/smind/commit/081c3a461549712d4ffec94e5a581fcd11170122))
* **web:** hover actions on chat messages ([#322](https://github.com/spacingmind/smind/issues/322)) ([8b06bfe](https://github.com/spacingmind/smind/commit/8b06bfeb2c1565d5d081e9fe2fbb74b74346f5a1))
* **web:** model picker, profile model field, run model chip, live model option (ADR-0025) ([#276](https://github.com/spacingmind/smind/issues/276)) ([683d14d](https://github.com/spacingmind/smind/commit/683d14dad4dddc1abeaeed4418a4a91615656fad))
* **web:** nested, collapsible task tree in the sidebar with attention rollup ([#293](https://github.com/spacingmind/smind/issues/293)) ([50b2c78](https://github.com/spacingmind/smind/commit/50b2c786644391d0cfd41ec0971304143488ff6e))
* **web:** optional plan-usage summary in the sidebar footer (ADR-0028 PR-D2) ([#315](https://github.com/spacingmind/smind/issues/315)) ([d55e654](https://github.com/spacingmind/smind/commit/d55e65470f7711d9882caa3ddd9a9a4b7b91d738))
* **web:** plan usage in each chat's context-usage hover (ADR-0028 PR-E) ([#309](https://github.com/spacingmind/smind/issues/309)) ([a4de74e](https://github.com/spacingmind/smind/commit/a4de74e1e2400760029568a3962f299580530f89))
* **web:** plan usage in Settings and a Usage modal (ADR-0028 PR-D1) ([#307](https://github.com/spacingmind/smind/issues/307)) ([4bf8edd](https://github.com/spacingmind/smind/commit/4bf8eddf46482e6a9c7d8881cdffc6c576c68f5e))
* **web:** settings as a full-screen screen with its own sidebar ([#297](https://github.com/spacingmind/smind/issues/297)) ([a8542ff](https://github.com/spacingmind/smind/commit/a8542ffc039013c55275bd5fe81da8b0c20c6cbc))
* **web:** sidebar footer actions, smoother resize/toggle, themed scrollbars ([#306](https://github.com/spacingmind/smind/issues/306)) ([5f614fb](https://github.com/spacingmind/smind/commit/5f614fb43d3275444e30da6bce1237d85fddb407))
* **web:** sidebar polish — single-line rows, scoped actions, keyboard, persistence ([#300](https://github.com/spacingmind/smind/issues/300)) ([6ea1985](https://github.com/spacingmind/smind/commit/6ea1985930971b62da86c12488c82eaa80bccecc))
* **web:** task header lineage (spawned-by, children strip, tree rollups) and archive confirm ([#295](https://github.com/spacingmind/smind/issues/295)) ([90409bc](https://github.com/spacingmind/smind/commit/90409bc296a4467e9542498be77d179a9ac1e2e3))
* **web:** usage in the UI — run pill, chat total, Settings → Usage (ADR-0020) ([#262](https://github.com/spacingmind/smind/issues/262)) ([2041b0f](https://github.com/spacingmind/smind/commit/2041b0f986b800268e344be4a862e9b09474e0f5))
* **web:** worktree setup scripts editor, task setup badge/log/retry, port chip (ADR-0024) ([#275](https://github.com/spacingmind/smind/issues/275)) ([6308f34](https://github.com/spacingmind/smind/commit/6308f34240b221134870a356d2a16d9c3bc258f1))
* **web:** ZCode-style app header and Paseo-style tabs ([#316](https://github.com/spacingmind/smind/issues/316)) ([1cbe6de](https://github.com/spacingmind/smind/commit/1cbe6de31cf0f54eeb9ced93e9576ae7f86bde2b))
* **web:** ZCode/Paseo-style composer ([#301](https://github.com/spacingmind/smind/issues/301)) ([3ffad8e](https://github.com/spacingmind/smind/commit/3ffad8ec9093fc4894b68daa725b9781cad59266))
* worktree setup/teardown scripts + per-task ports (ADR-0024) ([#270](https://github.com/spacingmind/smind/issues/270)) ([80823fb](https://github.com/spacingmind/smind/commit/80823fb0f1659a3e10e28d1b92d1710382fc0c08))
* **wsapi,mcp:** task.tree read model, task_tree and task_wait childrenOf ([#291](https://github.com/spacingmind/smind/issues/291)) ([7167736](https://github.com/spacingmind/smind/commit/71677361e9e0a9a7abebdbee80d5bffa8492e9db))
* **wsapi:** account.usage RPC/event, smind account usage, MCP account_usage (ADR-0028 PR-C) ([#305](https://github.com/spacingmind/smind/issues/305)) ([65059f6](https://github.com/spacingmind/smind/commit/65059f6d3e56c9523db5c1a8f68bd4da70498206))


### Bug Fixes

* **cli:** open the OAuth URL intact on Windows; stub the opener in tests ([#281](https://github.com/spacingmind/smind/issues/281)) ([e1b78a0](https://github.com/spacingmind/smind/commit/e1b78a0623f12cf3bbb12e5fff4de4560c8e6fb2))
* **desktop:** Windows tray/menu/Find/Ctrl+Q + devtools, theme, scrollbars, VS Code on WSL (QA W3/W4) ([#285](https://github.com/spacingmind/smind/issues/285)) ([1ec1e25](https://github.com/spacingmind/smind/commit/1ec1e256c5d9e9471fdbc9244475d457b68df28c))
* **desktop:** WSL daemon start + badge clears on permission resolved (Windows QA W1/W2) ([#283](https://github.com/spacingmind/smind/issues/283)) ([70dc67d](https://github.com/spacingmind/smind/commit/70dc67da361a9385976e9063d24565cc7aecc728))
* GLM pin 1.14.0, permission detail (daemon/CLI/MCP/web), task_wait state, stop note ([#271](https://github.com/spacingmind/smind/issues/271)) ([c09ee15](https://github.com/spacingmind/smind/commit/c09ee15287c2c80a9b104bf913e5af40c497b86c))
* **mcp:** dogfood fixes — task_status/task_logs blocker, task_wait ignorePermissionIds, review tools, CLI deny, promptFile ([#273](https://github.com/spacingmind/smind/issues/273)) ([0626d14](https://github.com/spacingmind/smind/commit/0626d14ce8b7918373b83616d9bb4aa40dbe66d6))
* terminal keeps shell keys, live runs via permission.pending, workspace context menu, CLI task approve (Windows QA) ([#284](https://github.com/spacingmind/smind/issues/284)) ([21ea3ad](https://github.com/spacingmind/smind/commit/21ea3ad063b6d08dfcd920998d50b9bc20dbf9e3))
* **ui:** send the terminal's fitted size on attach ([#282](https://github.com/spacingmind/smind/issues/282)) ([17d0c6a](https://github.com/spacingmind/smind/commit/17d0c6acc33a8df3f3dc66dfdbdcd8c5226cb508))
* **web:** distinguishable workspaces in the collapsed sidebar rail ([#314](https://github.com/spacingmind/smind/issues/314)) ([17caa07](https://github.com/spacingmind/smind/commit/17caa073c8c857369c02dcf92b94e8f87f2c5627))
* **web:** hide the normal end_turn stop reason in the run footer ([#326](https://github.com/spacingmind/smind/issues/326)) ([913448e](https://github.com/spacingmind/smind/commit/913448e66a85464d0da4183a3bf3b283448dd5ff))
* **web:** model/profile picker parity with Paseo ([#323](https://github.com/spacingmind/smind/issues/323)) ([6a8ba27](https://github.com/spacingmind/smind/commit/6a8ba2750e49daf8f1808ac4ed508d5bdba13e76))
* **web:** no gutter between the sidebar and the inset panel ([#321](https://github.com/spacingmind/smind/issues/321)) ([bafa216](https://github.com/spacingmind/smind/commit/bafa216ce91ed851e64d5319accc71e2b46287d8))
* **web:** one meta row above the chat (task total + lineage chips merged into the chat meta row) ([#324](https://github.com/spacingmind/smind/issues/324)) ([3a0c51a](https://github.com/spacingmind/smind/commit/3a0c51abcc9b092376e7cc03da650b6fa3329259))
* **web:** readable run footers ([#328](https://github.com/spacingmind/smind/issues/328)) ([2b25a3d](https://github.com/spacingmind/smind/commit/2b25a3de270cbacbedce6b0154426eeac7ffe429))
* **web:** sticky sidebar header, tighter indentation, quieter unread dots ([#320](https://github.com/spacingmind/smind/issues/320)) ([37abdf6](https://github.com/spacingmind/smind/commit/37abdf6973150729816e982c6802c46e7a130c32))
* **web:** unified live-run composer controls, quiet ACP noise, borderless permission dock ([#319](https://github.com/spacingmind/smind/issues/319)) ([594f372](https://github.com/spacingmind/smind/commit/594f3725acdc1c1c6fc400a03a47021076c8e7d0))

## [0.10.0](https://github.com/spacingmind/smind/compare/v0.9.1...v0.10.0) (2026-10-11)


### Features

* **cli:** smind mcp serve task_send with permission guard (ADR-0017 step 3) ([#246](https://github.com/spacingmind/smind/issues/246)) ([9be4864](https://github.com/spacingmind/smind/commit/9be48640a74eba43a1adef534ca590dc7247d8ff))
* **desktop:** black squircle macOS app icon ([#241](https://github.com/spacingmind/smind/issues/241)) ([e4a1aac](https://github.com/spacingmind/smind/commit/e4a1aacbb140ac9452f0124e7fed983e3799899a))
* **desktop:** desktop shortcuts and native menu actions (D3) ([#247](https://github.com/spacingmind/smind/issues/247)) ([ecf2f1a](https://github.com/spacingmind/smind/commit/ecf2f1ab53556623c91f964a56225428927c3605))
* **desktop:** macOS app — dmg, bundled daemon, Dock lifecycle ([#238](https://github.com/spacingmind/smind/issues/238)) ([0aff47c](https://github.com/spacingmind/smind/commit/0aff47cc10e4c97823912298f1b9483886a1941e))
* **desktop:** reveal/open-in-editor + badge (D4) ([#252](https://github.com/spacingmind/smind/issues/252)) ([c7d4afe](https://github.com/spacingmind/smind/commit/c7d4afef8fc2ec54c5e1150e81fcbf81173628f4))
* **desktop:** update the managed daemon only when no runs are in flight ([#243](https://github.com/spacingmind/smind/issues/243)) ([b37a9fd](https://github.com/spacingmind/smind/commit/b37a9fdc6df4a7a6a348219167056a101051e192))
* **desktop:** ZCode-style window chrome + no launch flash (D1) ([#237](https://github.com/spacingmind/smind/issues/237)) ([986bcc0](https://github.com/spacingmind/smind/commit/986bcc043bd7217d07e58d5fef7c82f8d6ad9035))
* per-chat prompt queue with whenBusy reject|queue|interrupt (ADR-0021) ([#248](https://github.com/spacingmind/smind/issues/248)) ([a499fa7](https://github.com/spacingmind/smind/commit/a499fa7c63a5ff51884152d68ff5e7b4a589db8a))
* per-run usage from agent events (ADR-0020, AC 1–6 + 8) ([#254](https://github.com/spacingmind/smind/issues/254)) ([8513efe](https://github.com/spacingmind/smind/commit/8513efe953994a9495eac4a856fb8cfeb9e34e0f))
* **proxy:** base-URL semantics, count_tokens, model-aware routing (ADR-0020 §5) ([#253](https://github.com/spacingmind/smind/issues/253)) ([6bfdeae](https://github.com/spacingmind/smind/commit/6bfdeae63af0166ea87ee64129c858370218d4a1))
* **taskrunner:** hand configured MCP servers to ACP and Claude-native agents (ADR-0018 steps 3-5, 8) ([#251](https://github.com/spacingmind/smind/issues/251)) ([55ef1da](https://github.com/spacingmind/smind/commit/55ef1dafd93633c50963d5fd52bef8e7e199997a))
* **terminal:** native Windows terminal backend with own ConPTY wrapper (ADR-0022) ([#245](https://github.com/spacingmind/smind/issues/245)) ([da05483](https://github.com/spacingmind/smind/commit/da054831707b33ed3d45ed74116e99cb86669092))
* **ui:** Settings section for MCP servers (ADR-0018 step 7) ([#250](https://github.com/spacingmind/smind/issues/250)) ([6d9aa04](https://github.com/spacingmind/smind/commit/6d9aa0438f37ed349ab6e726d74282b88bed8a46))
* **web:** desktop build drops webview-isms (D2) ([#240](https://github.com/spacingmind/smind/issues/240)) ([0c42566](https://github.com/spacingmind/smind/commit/0c42566bcbd17b486460cdc42468ecdeb2562f35))


### Bug Fixes

* **taskrunner:** pass Claude-native MCP config via 0600 temp file, not argv ([#255](https://github.com/spacingmind/smind/issues/255)) ([9e4432f](https://github.com/spacingmind/smind/commit/9e4432fe4cf76538cf2391179de9fe37533ed1bd))

## [0.9.1](https://github.com/spacingmind/smind/compare/v0.9.0...v0.9.1) (2026-10-08)


### Bug Fixes

* **relay:** SCRAM-style admission proof, bounded relay state, Go handshake key pin ([#235](https://github.com/spacingmind/smind/issues/235)) ([fdac9f4](https://github.com/spacingmind/smind/commit/fdac9f40dc1b35b92a71218294aeb255bbe26998))

## [0.9.0](https://github.com/spacingmind/smind/compare/v0.8.0...v0.9.0) (2026-10-08)


### ⚠ BREAKING CHANGES

* the approvalPolicy wire field, run.setApprovalPolicy, and the --approval-policy CLI flag are removed.

### Features

* add install.sh for one-line Linux/macOS install ([#233](https://github.com/spacingmind/smind/issues/233)) ([4dc51e4](https://github.com/spacingmind/smind/commit/4dc51e4f1692f5d17f0609dd4cd5f3d2a1228b0e))
* **cli:** smind mcp serve task_wait + task_stop + stdio e2e (ADR-0017 steps 4, 5, 7) ([#228](https://github.com/spacingmind/smind/issues/228)) ([83c7eb1](https://github.com/spacingmind/smind/commit/83c7eb1165d61fb79521a8409916cf8b74a7afc3))
* **cli:** smind mcp serve with read-only MCP tools (ADR-0017 steps 1-2) ([#223](https://github.com/spacingmind/smind/issues/223)) ([63aafc3](https://github.com/spacingmind/smind/commit/63aafc3dc71f12a774c992570b1f058cb44770d6))
* mcp_servers wsapi + events + CLI with secret redaction (ADR-0018 step 2) ([#230](https://github.com/spacingmind/smind/issues/230)) ([b738680](https://github.com/spacingmind/smind/commit/b7386801906aaeb4f0beced973d728b93b8023d0))
* **server:** per-request proxy request_log with token usage (M1) ([#226](https://github.com/spacingmind/smind/issues/226)) ([114e348](https://github.com/spacingmind/smind/commit/114e3484fe5b1c76424b457a5e07da0e98f771ff))
* **store:** mcp_servers table, CRUD, and mcpservers.Registry (ADR-0018 step 1) ([#222](https://github.com/spacingmind/smind/issues/222)) ([d27b699](https://github.com/spacingmind/smind/commit/d27b699eea944aed38e059ff07ed05071820bd81))
* task parent/child hierarchy + MCP parent params + depth guard ([#229](https://github.com/spacingmind/smind/issues/229)) ([63a4f15](https://github.com/spacingmind/smind/commit/63a4f15337329152ab5673951f14f25d38a7881b))


### Bug Fixes

* **taskrunner:** stopped runs keep their agent session (O1) ([#232](https://github.com/spacingmind/smind/issues/232)) ([1454329](https://github.com/spacingmind/smind/commit/1454329a74c9bbd0022619c3a4f85db1a7c11f90))


### Code Refactoring

* replace ApprovalPolicy with provider-native permission modes (ADR-0019) ([#231](https://github.com/spacingmind/smind/issues/231)) ([16927b9](https://github.com/spacingmind/smind/commit/16927b93df7ebf0a66629fd9e9ec4d7770a8e054))

## [0.8.0](https://github.com/spacingmind/smind/compare/v0.7.0...v0.8.0) (2026-09-27)


### Features

* **acp:** GLM session config options end to end (thinking-level fix) ([#147](https://github.com/spacingmind/smind/issues/147)) ([f684425](https://github.com/spacingmind/smind/commit/f6844250ce0aa95cfb688941b4259df46e32cf6c))
* agent profiles stored in the daemon (ADR-0014) ([#200](https://github.com/spacingmind/smind/issues/200)) ([482d4c5](https://github.com/spacingmind/smind/commit/482d4c531886713a39bdd831c629303f70ff9bab))
* **cli:** render + answer pending permission requests; relay admission ADR ([#144](https://github.com/spacingmind/smind/issues/144)) ([165e8b7](https://github.com/spacingmind/smind/commit/165e8b783c42e2b5b529bb1d3d22231d0f4c9f2b))
* **cli:** task send --chat, task chat ls/new/rename/archive, task runs (ADR-0016 P4) ([#213](https://github.com/spacingmind/smind/issues/213)) ([0e2e0b7](https://github.com/spacingmind/smind/commit/0e2e0b7d1cd4197667f0dc94492312d70d950701))
* **daemon:** version signal — build stamp, smind --version, /healthz version ([#195](https://github.com/spacingmind/smind/issues/195)) ([17b9e94](https://github.com/spacingmind/smind/commit/17b9e9440f9982da48b233035237e8024abbee71))
* **desktop:** app-managed local daemon — install, update & restart (ADR-0013 part D2) ([#201](https://github.com/spacingmind/smind/issues/201)) ([9cd80a8](https://github.com/spacingmind/smind/commit/9cd80a875cf52be3e21568989b7f934ce06a4e66))
* **desktop:** bundled UI behind a loopback proxy + connection list (ADR-0013 part 1) ([#196](https://github.com/spacingmind/smind/issues/196)) ([2d398e9](https://github.com/spacingmind/smind/commit/2d398e91e472d4c1c38973f1f88230b7c529781a))
* **desktop:** relay transport in Rust over native gRPC (ADR-0013 part C) ([#199](https://github.com/spacingmind/smind/issues/199)) ([03132bc](https://github.com/spacingmind/smind/commit/03132bc092df75fed150886ce67d920a2be14c7a))
* **desktop:** Tauri 2 desktop shell + native quick wins; ADR-0012/0013 ([#194](https://github.com/spacingmind/smind/issues/194)) ([2c9a511](https://github.com/spacingmind/smind/commit/2c9a5115a624d74ed6b1a5e6feed89c389ccea60))
* mid-run approval-policy switching + retry with higher effort ([#172](https://github.com/spacingmind/smind/issues/172)) ([301f36c](https://github.com/spacingmind/smind/commit/301f36c404c39af6ecac27b5fc990d42a882d80d))
* mobile app milestone 1 — daemon↔relay bridge, grpc-web, Expo E2EE client ([#175](https://github.com/spacingmind/smind/issues/175)) ([906d2a9](https://github.com/spacingmind/smind/commit/906d2a9c3bf66c78d04ad87cdae37f2f651333bc))
* **mobile:** adopt @expo/ui for native controls ([#183](https://github.com/spacingmind/smind/issues/183)) ([b6e2c56](https://github.com/spacingmind/smind/commit/b6e2c569e1c1ba92445761eede6f9999220f77d4))
* **mobile:** design-token foundation + agent-run UI polish ([#181](https://github.com/spacingmind/smind/issues/181)) ([5a0cf3b](https://github.com/spacingmind/smind/commit/5a0cf3b1060a5ae93bafdac5b87a24ba71845030))
* **mobile:** persistent RPC client + workspace/task navigation + realtime timeline ([#176](https://github.com/spacingmind/smind/issues/176)) ([50e618d](https://github.com/spacingmind/smind/commit/50e618d158678e9acc5539bc1091d495e64fb49b))
* **mobile:** send a follow-up prompt + approve/deny live permission requests ([#178](https://github.com/spacingmind/smind/issues/178)) ([212fbfd](https://github.com/spacingmind/smind/commit/212fbfd0011f9ae75ec13fe71ec96faf6b3cc732))
* per-account base_url upstream routing ([#140](https://github.com/spacingmind/smind/issues/140)) ([5db2643](https://github.com/spacingmind/smind/commit/5db264303110311dac6ba6ff47245cf9fc0ad532))
* **relay:** Phase 3 crypto core — X25519 keypair, pairing offer + QR, E2EE handshake, replay, rotation ([#145](https://github.com/spacingmind/smind/issues/145)) ([333e043](https://github.com/spacingmind/smind/commit/333e0437e2e8dd4744b9ec7d3c7b3dfa36766a49))
* **relay:** self-hostable E2EE relay server + daemon-side client (Phase 3 relay half) ([#148](https://github.com/spacingmind/smind/issues/148)) ([76d9f2c](https://github.com/spacingmind/smind/commit/76d9f2c0a5e8d45355121dddc6829dd9ba11ae7f))
* Settings → Providers + account rename/updateCredential/remove (ADR-0015) ([#202](https://github.com/spacingmind/smind/issues/202)) ([2325b5b](https://github.com/spacingmind/smind/commit/2325b5be777b9b35d1a5e61feb801dfca26ee172))
* **tasks:** add task.move -- reassign a task to a different space ([#170](https://github.com/spacingmind/smind/issues/170)) ([fd0f6b5](https://github.com/spacingmind/smind/commit/fd0f6b5c1e82ecfbf2fefd6bf7d2c7953df3c21c))
* **web:** actionable notifications + sidebar triage (Paseo parity) ([#186](https://github.com/spacingmind/smind/issues/186)) ([fabf4b8](https://github.com/spacingmind/smind/commit/fabf4b8e95d95956414e868ebceaeb069b445fa1))
* **web:** calm technical console visual-identity pass + dropdown/resize/truncation fixes ([#152](https://github.com/spacingmind/smind/issues/152)) ([e903306](https://github.com/spacingmind/smind/commit/e9033062e234f9b824b0973a3db5ef5d683579eb))
* **web:** chord shortcuts, pane/tab keyboard actions, Settings → Shortcuts ([#190](https://github.com/spacingmind/smind/issues/190)) ([87c62dc](https://github.com/spacingmind/smind/commit/87c62dcbc444f5cf3de248ea08bc73ae2042935c))
* **web:** dogfood polish — flexible tabs, composer card, settings screen ([#158](https://github.com/spacingmind/smind/issues/158)) ([bf8a5b1](https://github.com/spacingmind/smind/commit/bf8a5b1f58128f075f91e700d0a1770413b5b520))
* **web:** Find in chat, file and terminal (Paseo 0.9 parity) ([#187](https://github.com/spacingmind/smind/issues/187)) ([d2f2e80](https://github.com/spacingmind/smind/commit/d2f2e80cf32ee3186cb36214a152a00b42960daa))
* **web:** first brand identity -- logo + cyan accent color ([#168](https://github.com/spacingmind/smind/issues/168)) ([808a168](https://github.com/spacingmind/smind/commit/808a1682d1e492a47d56772716e65647ccb0d4db))
* **web:** move task to space, per-provider full-access approval, thinking-level controls ([#171](https://github.com/spacingmind/smind/issues/171)) ([d652ccd](https://github.com/spacingmind/smind/commit/d652ccd90a012534adeea94643c60ea4fcfec934))
* **web:** multiple chat tabs per task — New chat, rename, archive, per-chat composer (ADR-0016 P3) ([#214](https://github.com/spacingmind/smind/issues/214)) ([41fb6e2](https://github.com/spacingmind/smind/commit/41fb6e23dfaced3c5b9e2c444a29a08d6cd1ac95))
* **web:** pane split-tree -- hook port, split affordance, recursive renderer (Items 2-4) ([#162](https://github.com/spacingmind/smind/issues/162)) ([923908a](https://github.com/spacingmind/smind/commit/923908abb4ad9ba351de0b00fd96f16056e04869))
* **web:** pane split-tree full parity -- 4-direction split, drag-to-split, exact-pane + (Items 7-9) ([#165](https://github.com/spacingmind/smind/issues/165)) ([96fad62](https://github.com/spacingmind/smind/commit/96fad62fe22aed2a3b8633a2b5af271194accb18))
* **web:** port paseo's split-tree module (Item 1) ([#161](https://github.com/spacingmind/smind/issues/161)) ([72d955d](https://github.com/spacingmind/smind/commit/72d955dfba974ce22acd407e8cb732183feb7941))
* **web:** recent-paths quick-jump in the new-workspace folder picker ([#167](https://github.com/spacingmind/smind/issues/167)) ([77b8053](https://github.com/spacingmind/smind/commit/77b8053446ec92ce7e2864e5122331e967c9f73f))
* **web:** run-config IA — agents toolbar, header pill, settings regroup, palette ([#203](https://github.com/spacingmind/smind/issues/203)) ([7061f9a](https://github.com/spacingmind/smind/commit/7061f9adbd9d1f180083ed6a6de68cd9c7ab2204))
* **web:** ZCode visual parity P1 — design system (tokens, text-ui-* scale, primitives) ([#197](https://github.com/spacingmind/smind/issues/197)) ([5e964d0](https://github.com/spacingmind/smind/commit/5e964d047783c07d9b22368702ae721061ca3e34))
* **web:** ZCode visual parity P2 — app shell frames + sidebar ([#207](https://github.com/spacingmind/smind/issues/207)) ([fa85410](https://github.com/spacingmind/smind/commit/fa854107a379c26619f1bbae8535c1c2426071ee))
* **web:** ZCode visual parity P3 — timeline, composer, tool & permission cards ([#208](https://github.com/spacingmind/smind/issues/208)) ([0534493](https://github.com/spacingmind/smind/commit/05344939b1199ddc342a5242867ef41ad748dfe1))
* **web:** ZCode visual parity P4 — diff/git, terminal, files, settings ([#209](https://github.com/spacingmind/smind/issues/209)) ([1e3029f](https://github.com/spacingmind/smind/commit/1e3029fc39f9c1103a1e1932cbcb5805ef82f08d))
* wire per-account base_url through CLI, wsapi, and UI ([#142](https://github.com/spacingmind/smind/issues/142)) ([4f86d06](https://github.com/spacingmind/smind/commit/4f86d0639ce267e500a43938a72058586ed6c89d))


### Bug Fixes

* **desktop:** stop flashing console windows and add Windows Desktop shortcut ([#216](https://github.com/spacingmind/smind/issues/216)) ([a42ab24](https://github.com/spacingmind/smind/commit/a42ab24384902ee55909c0518c7e260e1ded0b7c))
* **desktop:** use the smind logo for app/tray/shortcut icons (was Expo's placeholder) ([#217](https://github.com/spacingmind/smind/issues/217)) ([e84cb08](https://github.com/spacingmind/smind/commit/e84cb081e3c03da047d65e7e5dbbe7665d5c5f29))
* e2ee post-handshake cancel race; proxy aborts truncated upstream streams; CI flake ([#205](https://github.com/spacingmind/smind/issues/205)) ([0f1310e](https://github.com/spacingmind/smind/commit/0f1310e88fae41e8660f57fd1c378a2aa15a70c9))
* **mobile:** render TasksScreen empty state ([#193](https://github.com/spacingmind/smind/issues/193)) ([2da6d5f](https://github.com/spacingmind/smind/commit/2da6d5f5647009eb9fd762296570c3f3221bf033))
* root-cause two CI flakes (relay client reconnect, taskrunner fake Claude CLI) ([#204](https://github.com/spacingmind/smind/issues/204)) ([fd4444d](https://github.com/spacingmind/smind/commit/fd4444db231e3f6d21dfeffd6f183f5603968de1))
* **runs:** record claude-native permission cancellation + raise CLI dialog deadline ([#153](https://github.com/spacingmind/smind/issues/153)) ([f8586e4](https://github.com/spacingmind/smind/commit/f8586e4a8933ab8025b1a09b47ae6b4900cf198d))
* **taskrunner:** resume agent sessions so follow-up prompts keep context (ADR-0016 P2) ([#211](https://github.com/spacingmind/smind/issues/211)) ([b951d27](https://github.com/spacingmind/smind/commit/b951d27d762a88135673020623aeaafe0eb418f8))
* **web:** icon-collapsed sidebar no longer leaves dead space ([#166](https://github.com/spacingmind/smind/issues/166)) ([7c1bb27](https://github.com/spacingmind/smind/commit/7c1bb27ab16b111bca1cb9da8c2a2a8061cf36f8))
* **web:** make the resizable-panel drag actually work ([#146](https://github.com/spacingmind/smind/issues/146)) ([d1235c6](https://github.com/spacingmind/smind/commit/d1235c67f53a876aad4170e163e53817553ec713))
* **web:** rename input loses focus to Radix's own dismiss-focus restoration ([#215](https://github.com/spacingmind/smind/issues/215)) ([6fcf6ce](https://github.com/spacingmind/smind/commit/6fcf6cec7e997e5a4b25a504c8468a52562a4ad4))
* **web:** seed only the Chat tab on first task visit ([#159](https://github.com/spacingmind/smind/issues/159)) ([f60abbc](https://github.com/spacingmind/smind/commit/f60abbcba6ebf1a4524f935f69a30cf8c5d47f70))
* **web:** show a raw event's real ACP kind, not the generic "raw" label ([#173](https://github.com/spacingmind/smind/issues/173)) ([f48e704](https://github.com/spacingmind/smind/commit/f48e704144199ca3b1768f3597ea642598934f26))
* **web:** taller composer, no static border on the input card ([#169](https://github.com/spacingmind/smind/issues/169)) ([0af53b3](https://github.com/spacingmind/smind/commit/0af53b32e8d34a4cc268a2ef1abc57a851f64805))

## [0.7.0](https://github.com/spacingmind/smind/compare/v0.6.0...v0.7.0) (2026-09-15)


### Features

* add Codex (OpenAI) as a native third-party provider ([#40](https://github.com/spacingmind/smind/issues/40)) ([03fd0aa](https://github.com/spacingmind/smind/commit/03fd0aa0a96ae57193b7a3511166f523f915c604))
* add Kimi as a second ACP-speaking provider ([#39](https://github.com/spacingmind/smind/issues/39)) ([118419d](https://github.com/spacingmind/smind/commit/118419ddb2c75aa43fe98c7753fc88543680e5ff))
* merge account CLI and unified task dev hot reload ([#49](https://github.com/spacingmind/smind/issues/49)) ([0335bf4](https://github.com/spacingmind/smind/commit/0335bf41a08867bd1761f0584e7a532786796b08))
* persist run/conversation history so it survives a daemon restart ([#38](https://github.com/spacingmind/smind/issues/38)) ([e8ee22a](https://github.com/spacingmind/smind/commit/e8ee22a3cb86ccbdf90f9da397cab7f8c6e13f67))
* **store:** add terminal_sessions persistence ([0e3e59a](https://github.com/spacingmind/smind/commit/0e3e59acd2c9abe220fc908fed221b6ceddb11fc))
* **terminal:** survive a daemon restart, mirroring internal/runs ([640d80e](https://github.com/spacingmind/smind/commit/640d80e78e3d3b9a76dcefa648fac80d61e5ab06))
* **ui:** Item 21 — real responsive/compact shell layout ([#134](https://github.com/spacingmind/smind/issues/134)) ([4565837](https://github.com/spacingmind/smind/commit/45658376418eb39fa6a6b76a3d6f533143d3d6e2))
* **web:** add WsClient onClose hook and a browser reconnect wrapper ([7a638e4](https://github.com/spacingmind/smind/commit/7a638e4baf20114d2952d6abe9e32de02528ec2c))
* **web:** drive App.tsx's connection status from real socket events ([97fa3ab](https://github.com/spacingmind/smind/commit/97fa3abee4f3b9393963cc09840a045b43a72d7c))
* **web:** editor preview pane (markdown, svg, sandboxed html) ([#51](https://github.com/spacingmind/smind/issues/51)) ([b98d26f](https://github.com/spacingmind/smind/commit/b98d26ffcdc5edbbcfe9cd83b777d7253a654b6e))
* **web:** file conflict detection with reload/overwrite ([#62](https://github.com/spacingmind/smind/issues/62)) ([114800c](https://github.com/spacingmind/smind/commit/114800ca562bbb6fcbe143308588db4ef59963e4))
* **web:** live UI on event subscriptions ([#59](https://github.com/spacingmind/smind/issues/59)) ([7c4e881](https://github.com/spacingmind/smind/commit/7c4e8815fe021f5882e543a06a4d1072e808427e))
* **web:** per-file staging and commit flow ([#61](https://github.com/spacingmind/smind/issues/61)) ([43adcb3](https://github.com/spacingmind/smind/commit/43adcb33f9dad05b45763ed5f075dade0c65651f))
* **web:** provider dropdown from provider.list ([#60](https://github.com/spacingmind/smind/issues/60)) ([d5a2e2d](https://github.com/spacingmind/smind/commit/d5a2e2d2e2fd33fcea19313dfaf12785d2d41fb1))
* **web:** resync TaskDetailPane/TerminalPane after a reconnect ([cf8550c](https://github.com/spacingmind/smind/commit/cf8550c571d2547488a747445d6aaadf4325b5f0))
* **web:** tab registry with per-task scoping and attention badges ([#53](https://github.com/spacingmind/smind/issues/53)) ([e1141ad](https://github.com/spacingmind/smind/commit/e1141adfb7b632c5ce2939693e08810c953eb182))
* **wsapi:** event subscription RPC ([#54](https://github.com/spacingmind/smind/issues/54)) ([6947ba8](https://github.com/spacingmind/smind/commit/6947ba840b8e89dce62b9ec4a5ddf2f3a213086f))
* **wsapi:** thread store persistence into terminal.Registry construction ([bc0a893](https://github.com/spacingmind/smind/commit/bc0a89374c8be63686fe92cc1694d517710a8316))


### Bug Fixes

* kill in-flight run.start subprocesses on daemon shutdown ([#37](https://github.com/spacingmind/smind/issues/37)) ([93e8633](https://github.com/spacingmind/smind/commit/93e8633828c7fad0b803f56e6b6ae1cabafcbd14))
* persist a run's terminal status before it becomes visible in memory ([#42](https://github.com/spacingmind/smind/issues/42)) ([7933bd6](https://github.com/spacingmind/smind/commit/7933bd6275182df72fef98066e816e3ec3d89a7c))
* restore release metadata (manifest + changelog) accidentally reverted by the master rebase promotion ([8be6722](https://github.com/spacingmind/smind/commit/8be67227617e78649c608943d12205d505c558cc))
* **terminal:** close checkpoint/finish write race and reap zombies on Create failure ([6d9ff5b](https://github.com/spacingmind/smind/commit/6d9ff5b5539fa88da84d075920ac8af94ad87fd2))
* **ui:** wire the two missing Item 6 side-dock entry points, add the promised terminal detach-not-stop test ([#133](https://github.com/spacingmind/smind/issues/133)) ([cc79922](https://github.com/spacingmind/smind/commit/cc79922793ef3504173667c25f7203c8940f90c6))
* **web:** handleClose must reset lastTerminalIdRef, not just terminalId ([59079b9](https://github.com/spacingmind/smind/commit/59079b9496f922a4dbb8f5927ed689f5ec460cbf))
* **web:** isolate one throwing onClose callback from its siblings ([bc60060](https://github.com/spacingmind/smind/commit/bc60060de4b3805f0980626f47c2b4c189c67731))
* **workspace:** checkpoint task work before archiving ([#52](https://github.com/spacingmind/smind/issues/52)) ([f5233a1](https://github.com/spacingmind/smind/commit/f5233a180a29a526f91fabc5e0a697e4d3f84dba))

## [0.6.0](https://github.com/spacingmind/smind/compare/v0.5.0...v0.6.0) (2026-09-15)


### Features

* **accounts:** browser-based OAuth login for anthropic/openai accounts ([#70](https://github.com/spacingmind/smind/issues/70)) ([d213ac8](https://github.com/spacingmind/smind/commit/d213ac8253fed5c9948b0d382af0434ebc8cda18))
* add Codex (OpenAI) as a native third-party provider ([#40](https://github.com/spacingmind/smind/issues/40)) ([d6ac499](https://github.com/spacingmind/smind/commit/d6ac4990635fbb2e392929acc387577c09ea46f5))
* add Kimi as a second ACP-speaking provider ([#39](https://github.com/spacingmind/smind/issues/39)) ([f6c884c](https://github.com/spacingmind/smind/commit/f6c884c9a02f747e392aae8ec47c4e748448a899))
* **cli:** smind account test &lt;provider&gt; wraps provider.test ([#104](https://github.com/spacingmind/smind/issues/104)) ([f7c2766](https://github.com/spacingmind/smind/commit/f7c276648959164359ab44a34b3c213fd1c5423d))
* **cli:** task send --approval-policy flag + plan notes ([#97](https://github.com/spacingmind/smind/issues/97)) ([e69c507](https://github.com/spacingmind/smind/commit/e69c507cf6b8e4bf344f1a6e8e3da8f5f430e205))
* delete workspace/space (removes smind's tracking only, never on-disk) ([#73](https://github.com/spacingmind/smind/issues/73)) ([0c2b3f4](https://github.com/spacingmind/smind/commit/0c2b3f4e9560659d9c2b097be4ac46bef23ac9ed))
* merge account CLI and unified task dev hot reload ([#49](https://github.com/spacingmind/smind/issues/49)) ([50e1a6c](https://github.com/spacingmind/smind/commit/50e1a6c402382a9f80105cb1c9111e8fa480da02))
* persist run/conversation history so it survives a daemon restart ([#38](https://github.com/spacingmind/smind/issues/38)) ([60bbc28](https://github.com/spacingmind/smind/commit/60bbc282d00ef1048018eca0e093e42b36ef88b3))
* **store:** add terminal_sessions persistence ([6634490](https://github.com/spacingmind/smind/commit/663449017bea170986cc18b41a8b70f84ef8136c))
* **store:** PRAGMA-checked additive migration for pre-existing DBs ([#100](https://github.com/spacingmind/smind/issues/100)) ([d45a287](https://github.com/spacingmind/smind/commit/d45a287978a5f67809aef01b8a142f9ccc07a2e4))
* **taskrunner,wsapi,web:** derive accounts-dialog provider list from provider.list (Item 7d) ([#102](https://github.com/spacingmind/smind/issues/102)) ([c971d6b](https://github.com/spacingmind/smind/commit/c971d6b2dd75c7771da8003d85c479075b3da7ec))
* **taskrunner,wsapi,web:** surface CLI-kind providers (GLM) in accounts dialog (Item 7b) ([#99](https://github.com/spacingmind/smind/issues/99)) ([43a5196](https://github.com/spacingmind/smind/commit/43a5196e9c9873c2321b98837064ca49dd970986))
* **taskrunner:** add approval-policy + permission-request timeout (Items 1+2) ([#93](https://github.com/spacingmind/smind/issues/93)) ([0ca93fb](https://github.com/spacingmind/smind/commit/0ca93fb8ff2d86f8e7384e9d92c5eb8b31600165))
* **taskrunner:** allowlist local git add/commit under auto-safe ([#109](https://github.com/spacingmind/smind/issues/109)) ([3898fcc](https://github.com/spacingmind/smind/commit/3898fcc050fb49c4e5dc9effd4890f276c93d1ff))
* **taskrunner:** extract shell commands from ACP execute tool calls ([#119](https://github.com/spacingmind/smind/issues/119)) ([ffd07b1](https://github.com/spacingmind/smind/commit/ffd07b15fe23813e148651bd88206ff64befe65f))
* **taskrunner:** preserve unknown ACP session-update kinds as raw events ([#126](https://github.com/spacingmind/smind/issues/126)) ([d2e8d0a](https://github.com/spacingmind/smind/commit/d2e8d0a40558056cdfbfa2fe9c781562422e00f3))
* **terminal:** survive a daemon restart, mirroring internal/runs ([97b8601](https://github.com/spacingmind/smind/commit/97b86016eabb30e067eb929bbb41d89806347ca0))
* **ui:** design tokens, dark mode, primitives + structured/lifecycle events (Items 1,2,7,16) ([#120](https://github.com/spacingmind/smind/issues/120)) ([b421b09](https://github.com/spacingmind/smind/commit/b421b096525f3805c43adc83032d3da282201585))
* **ui:** surface permission_resolved's reason in the timeline ([#125](https://github.com/spacingmind/smind/issues/125)) ([bcf2962](https://github.com/spacingmind/smind/commit/bcf296219c9eb0e2f61e4f13d3a835c5fbf89bb0))
* **ui:** Track A — shell (keyboard, palette, routing, splits, responsive) ([#124](https://github.com/spacingmind/smind/issues/124)) ([728a5db](https://github.com/spacingmind/smind/commit/728a5db65fb1d718d24b61f171695af46786a5d3))
* **ui:** Track B — structured timeline, tool-call cards, composer v2, permissions v2 ([#122](https://github.com/spacingmind/smind/issues/122)) ([7b75b1e](https://github.com/spacingmind/smind/commit/7b75b1edbceb0bcd872c1ffb96ff847274b9a147))
* **ui:** Track C — panes (file explorer/editor, diff/review, terminal, quick-open) ([#123](https://github.com/spacingmind/smind/issues/123)) ([b07acce](https://github.com/spacingmind/smind/commit/b07accec0d1fd137d641fd85194ec03970c15d04))
* **ui:** Track D — settings screen, accounts v2, quota (Items 13-15) ([#127](https://github.com/spacingmind/smind/issues/127)) ([1507384](https://github.com/spacingmind/smind/commit/1507384832aecf1ebcb6e8047b3f7c75a8e4160a))
* **ui:** Track D — sidebar signal, settings, accounts v2, quota ([#121](https://github.com/spacingmind/smind/issues/121)) ([89e31bb](https://github.com/spacingmind/smind/commit/89e31bb3bb2313ad293c3a467c2e02d450e45c8a))
* **web:** add WsClient onClose hook and a browser reconnect wrapper ([7baea05](https://github.com/spacingmind/smind/commit/7baea0535b26a442980f302e045a2f14209b795f))
* **web:** approval-policy selector in run-start prompt form (Item 7a) ([#98](https://github.com/spacingmind/smind/issues/98)) ([15bc3d0](https://github.com/spacingmind/smind/commit/15bc3d052b21102121e96ed3e31ac6f44882a6ca))
* **web:** CRUD UI for workspace/space/task create, archive, accounts ([#69](https://github.com/spacingmind/smind/issues/69)) ([d3ee082](https://github.com/spacingmind/smind/commit/d3ee082f9745d6a6f0e8706bdc67bce53ac52dc0))
* **web:** drive App.tsx's connection status from real socket events ([fd97f12](https://github.com/spacingmind/smind/commit/fd97f12333fc77b8ce4e8a70d447b77595e019a0))
* **web:** editor preview pane (markdown, svg, sandboxed html) ([#51](https://github.com/spacingmind/smind/issues/51)) ([5a520ff](https://github.com/spacingmind/smind/commit/5a520ff0d20972c9e141a0a674065a393c4c2977))
* **web:** file conflict detection with reload/overwrite ([#62](https://github.com/spacingmind/smind/issues/62)) ([0839cf5](https://github.com/spacingmind/smind/commit/0839cf578625ba967169387199073d58d4eae22e))
* **web:** live UI on event subscriptions ([#59](https://github.com/spacingmind/smind/issues/59)) ([fda2c5b](https://github.com/spacingmind/smind/commit/fda2c5b9e78337a4a1145e23e77a6bf73d2796dd))
* **web:** per-file staging and commit flow ([#61](https://github.com/spacingmind/smind/issues/61)) ([7a2fb9b](https://github.com/spacingmind/smind/commit/7a2fb9bd851060a8b9066429caee9a343b59e0e6))
* **web:** pin permission card near composer, attention notifications, resizable layout (Items 3, 4, 6) ([#94](https://github.com/spacingmind/smind/issues/94)) ([9454f33](https://github.com/spacingmind/smind/commit/9454f33c254b97877d8808287a1a098105e7f1d9))
* **web:** provider dropdown from provider.list ([#60](https://github.com/spacingmind/smind/issues/60)) ([e53d6d8](https://github.com/spacingmind/smind/commit/e53d6d880b5ef2d47efe778dc1c326914b907524))
* **web:** resync TaskDetailPane/TerminalPane after a reconnect ([fe9b995](https://github.com/spacingmind/smind/commit/fe9b995bd5b54e128e784aabe31626a4b6237fd2))
* **web:** server-side folder picker for New workspace's Path field ([#72](https://github.com/spacingmind/smind/issues/72)) ([1caab67](https://github.com/spacingmind/smind/commit/1caab675fb4ebb2f5df9a0324407243a77dea9fe))
* **web:** tab registry with per-task scoping and attention badges ([#53](https://github.com/spacingmind/smind/issues/53)) ([7df8514](https://github.com/spacingmind/smind/commit/7df851426b3318028e8f4c2119ce4f868fe42215))
* **workspace,wsapi,web:** task.createPr RPC + Create PR button in diff view (Item 5) ([#96](https://github.com/spacingmind/smind/issues/96)) ([d3e9fed](https://github.com/spacingmind/smind/commit/d3e9fed8bca5aff0b3c7ee92b192fa7f34ca7bc6))
* **wsapi,web:** provider.test diagnostic RPC + health status and Test button in accounts dialog (Item 7c) ([#101](https://github.com/spacingmind/smind/issues/101)) ([c50b2a9](https://github.com/spacingmind/smind/commit/c50b2a91abe9fb1a8a30938d883d469b76d4e03c))
* **wsapi:** event subscription RPC ([#54](https://github.com/spacingmind/smind/issues/54)) ([3fda4ec](https://github.com/spacingmind/smind/commit/3fda4ecbc801e64b3ac2868f2186c64d1f1aec5d))
* **wsapi:** thread store persistence into terminal.Registry construction ([f1c6a6f](https://github.com/spacingmind/smind/commit/f1c6a6ff032215540b257c0972e6367c3decca76))


### Bug Fixes

* **accounts:** accept epoch-millis/seconds expires_at in OAuthCredential import ([#77](https://github.com/spacingmind/smind/issues/77)) ([b3230df](https://github.com/spacingmind/smind/commit/b3230dfc6fbe1456f881cc6205d55963c1d66aaa))
* kill in-flight run.start subprocesses on daemon shutdown ([#37](https://github.com/spacingmind/smind/issues/37)) ([210d14c](https://github.com/spacingmind/smind/commit/210d14c2cf7daccd5cc28702c5533cc623366696))
* persist a run's terminal status before it becomes visible in memory ([#42](https://github.com/spacingmind/smind/issues/42)) ([46d9ec5](https://github.com/spacingmind/smind/commit/46d9ec575582ee9250567464894f7c98282b9736))
* **store:** empty lists must marshal as [], never null (fresh-install crash) ([#68](https://github.com/spacingmind/smind/issues/68)) ([06bd802](https://github.com/spacingmind/smind/commit/06bd802dbe3eebcba84f1399c4fb4d6e6e79459c))
* **taskrunner:** allowlist task test/lint/build for auto-safe runs ([#105](https://github.com/spacingmind/smind/issues/105)) ([13245e1](https://github.com/spacingmind/smind/commit/13245e1172d2f319c3b633c37e0ec6051ff87d82))
* **taskrunner:** AllowlistedCommand handles cd-prefixed chained commands ([#103](https://github.com/spacingmind/smind/issues/103)) ([80c3b79](https://github.com/spacingmind/smind/commit/80c3b796e651066e41cb13c21ce969dd84000d08))
* **taskrunner:** auto-allow ACP file edits inside the worktree under auto-safe ([#111](https://github.com/spacingmind/smind/issues/111)) ([fb8a2a3](https://github.com/spacingmind/smind/commit/fb8a2a3e0b02604c1f988337bd6acb0a9978b934))
* **taskrunner:** claude runs with a human decider need acceptEdits mode ([#91](https://github.com/spacingmind/smind/issues/91)) ([f60a03d](https://github.com/spacingmind/smind/commit/f60a03d6fe32ad2cffb9a04263274f76529a0fa8))
* **taskrunner:** fall back to the ACP tool-call title for file-edit auto-allow ([#115](https://github.com/spacingmind/smind/issues/115)) ([d1da783](https://github.com/spacingmind/smind/commit/d1da783e1b12d0fa5b43528f72bd59dbb7df6fb0))
* **taskrunner:** pre-approve allowlisted Bash at the Claude CLI's own gate ([#107](https://github.com/spacingmind/smind/issues/107)) ([2bb1e57](https://github.com/spacingmind/smind/commit/2bb1e57e61436f4e6e3b3c600a875b3a97fe9800))
* **taskrunner:** resolve relative paths in ACP structured locations too ([#118](https://github.com/spacingmind/smind/issues/118)) ([5589b7a](https://github.com/spacingmind/smind/commit/5589b7a9b34c6d12d1985a3ca9dc6172cc68459a))
* **taskrunner:** resolve relative paths in ACP title-fallback file-edit gate ([#117](https://github.com/spacingmind/smind/issues/117)) ([24143e6](https://github.com/spacingmind/smind/commit/24143e6901a75ecf19c97bc1f7b935ed53c6512f))
* **terminal:** close checkpoint/finish write race and reap zombies on Create failure ([1a19ae7](https://github.com/spacingmind/smind/commit/1a19ae7856ae854c82ed2dc4e3d32fd486e3ecea))
* **ux:** stop leaking a raw stat error, tighten guide copy at 3 spots ([#74](https://github.com/spacingmind/smind/issues/74)) ([1a51f0a](https://github.com/spacingmind/smind/commit/1a51f0af1016e2072d4401270c026cabcf82ce0c))
* **web:** clip sidebar container overflow so long content can't bleed out ([#78](https://github.com/spacingmind/smind/issues/78)) ([2c65046](https://github.com/spacingmind/smind/commit/2c650467edc6dd5184b12e2e5c316a7e71820f49))
* **web:** close the remaining min-w-0 gap in the sidebar's own flex chain ([#76](https://github.com/spacingmind/smind/issues/76)) ([8f55cd1](https://github.com/spacingmind/smind/commit/8f55cd1a2bd8e56204f5774b61c45f8f67ab747e))
* **web:** handleClose must reset lastTerminalIdRef, not just terminalId ([0dc52fb](https://github.com/spacingmind/smind/commit/0dc52fb103d8a4cd93a836ea269a372f78d160ba))
* **web:** isolate one throwing onClose callback from its siblings ([8f21c1d](https://github.com/spacingmind/smind/commit/8f21c1d5b29f69927859af946154a5f6513545c7))
* **web:** long names no longer overflow/overlap in sidebar, tabs, and panes ([#75](https://github.com/spacingmind/smind/issues/75)) ([a98e1e2](https://github.com/spacingmind/smind/commit/a98e1e273fcfd9700587441239f57e4ebd996b31))
* **web:** reconnect must resolve previousId to its exact session ([b9f550e](https://github.com/spacingmind/smind/commit/b9f550e5111b67944f208614b01a77557d3b2c73))
* **workspace:** checkpoint task work before archiving ([#52](https://github.com/spacingmind/smind/issues/52)) ([06d6a87](https://github.com/spacingmind/smind/commit/06d6a87b98e94c8bc0f3698eed8ff5e31ac7958f))
* **workspace:** default empty workspace title to repo dir name; exclude archived tasks from task.list ([#84](https://github.com/spacingmind/smind/issues/84)) ([82b390e](https://github.com/spacingmind/smind/commit/82b390e34fa6dde8f1987f7e539fb483039cdf0f))
* **wsapi:** stop pumpEvents from spinning at 100% CPU after connection close ([#95](https://github.com/spacingmind/smind/issues/95)) ([f8d1bf7](https://github.com/spacingmind/smind/commit/f8d1bf7e16851e7f222a7ea72d6b12f0eb07feb7))

## [0.5.0](https://github.com/spacingmind/smind/compare/v0.4.0...v0.5.0) (2026-09-09)


### Features

* live UI on events, provider dropdown, per-file commit flow, conflict detection ([#65](https://github.com/spacingmind/smind/issues/65)) ([1154fcd](https://github.com/spacingmind/smind/commit/1154fcd656110f076b9625e493c8afb214b88569))

## [0.4.0](https://github.com/spacingmind/smind/compare/v0.3.0...v0.4.0) (2026-09-09)


### Features

* editor preview pane, tab registry with per-task tabs, wsapi event subscription, archive checkpoint ([#56](https://github.com/spacingmind/smind/issues/56)) ([f621492](https://github.com/spacingmind/smind/commit/f621492159de7956b247bb2b815e599180541707))

## [0.3.0](https://github.com/spacingmind/smind/compare/v0.2.0...v0.3.0) (2026-08-28)


### Features

* add Codex (OpenAI) as a native third-party provider ([#40](https://github.com/spacingmind/smind/issues/40)) ([9fcb01b](https://github.com/spacingmind/smind/commit/9fcb01b9bb9ec7ec9f890da02cbd451899869c59))
* add Kimi as a second ACP-speaking provider ([#39](https://github.com/spacingmind/smind/issues/39)) ([cc47ee7](https://github.com/spacingmind/smind/commit/cc47ee70feb77c14f46c543f9a4b0824c278fdf6))
* persist run/conversation history so it survives a daemon restart ([#38](https://github.com/spacingmind/smind/issues/38)) ([84f3ee5](https://github.com/spacingmind/smind/commit/84f3ee56f956412e7be1dc058a7d54645211912f))


### Bug Fixes

* close a race between Stop's cancellation and abandoning a pending permission request ([#41](https://github.com/spacingmind/smind/issues/41)) ([87f5acf](https://github.com/spacingmind/smind/commit/87f5acf3092ebe45932844801c64edbcd9053a93))
* kill in-flight run.start subprocesses on daemon shutdown ([#37](https://github.com/spacingmind/smind/issues/37)) ([f575359](https://github.com/spacingmind/smind/commit/f575359d26e6ddfb2af10c67d2209b975e4e7ed3))
* persist a run's terminal status before it becomes visible in memory ([#42](https://github.com/spacingmind/smind/issues/42)) ([5adb204](https://github.com/spacingmind/smind/commit/5adb204caf59191e3557d6f1b76fdb7b0af169c3))

## [0.2.0](https://github.com/spacingmind/smind/compare/v0.1.0...v0.2.0) (2026-08-28)


### Features

* add ACP client for spawning and driving GLM/other ACP agents ([b15b895](https://github.com/spacingmind/smind/commit/b15b895f5d74b4ce9eb32236daca7944dcb819ac))
* add Anthropic and OpenAI OAuth refresh implementations ([291b761](https://github.com/spacingmind/smind/commit/291b7619e79076078d1c49d7f58ca8ae8cc49fae))
* add CI workflow and npm name-reservation stub ([6e6ba3e](https://github.com/spacingmind/smind/commit/6e6ba3e10b035b5e02e1fef48fa5028eb6edf36a))
* add internal/accounts typed registry over internal/store ([a040854](https://github.com/spacingmind/smind/commit/a0408544b306f20d0cf17a9a83d64d50a8fbeb02))
* add internal/routing account-selection engine ([5b41c80](https://github.com/spacingmind/smind/commit/5b41c8072666cb11c6e365591b577c181442e970))
* add internal/taskrunner, unifying ACP/Claude Code task turns ([d720169](https://github.com/spacingmind/smind/commit/d7201699fd1d7f0be55685c70dd29123bd3a1d6e))
* add internal/workspace service layer with real git worktrees ([0ae79bc](https://github.com/spacingmind/smind/commit/0ae79bcac2246be9858a214dddafbd633fe5b9f1))
* add Kimi, xAI, and Antigravity OAuth refresh implementations ([5293b42](https://github.com/spacingmind/smind/commit/5293b429144f40e0af027664c57acf8759d229b3))
* add refs map, roadmap, and dev skills ([d38f12b](https://github.com/spacingmind/smind/commit/d38f12b8f9b8e4563fb50f2451111b4d1b6d38ac))
* add reverse proxy endpoints for Anthropic and OpenAI ([b99ba82](https://github.com/spacingmind/smind/commit/b99ba82eb18d4c2535a247eae6c69eb870e083a4))
* add simple bearer-token auth for the smind HTTP API ([1807b2a](https://github.com/spacingmind/smind/commit/1807b2ad29fa8367935302c3f779b21aa9ab1d61))
* add Space CRUD to workspace.Manager, expose RunPrompt's GLM command for testing ([b0e741c](https://github.com/spacingmind/smind/commit/b0e741c29eb7f32ac32e8592228a4c19d7e0b09d))
* add SQLite-backed store for accounts, routing decisions, quota snapshots ([7c71024](https://github.com/spacingmind/smind/commit/7c71024b3e47b3b476fe427e412270598853b139))
* add TTL-cached quota poller backed by store's quota_snapshots ([bc7b7ea](https://github.com/spacingmind/smind/commit/bc7b7ea8b2673588bc93bf2b992584ffecf4747a))
* add uTLS-based RoundTripper for outbound TLS fingerprinting ([5c6beae](https://github.com/spacingmind/smind/commit/5c6beaecfddc8c367779f7b196155c3f41916845))
* add WebSocket RPC API (internal/wsapi) for workspace/space/task CRUD and streaming task.prompt ([3ad2b35](https://github.com/spacingmind/smind/commit/3ad2b35e40f4e00cc8aeba05a84d3eb7e149170e))
* add Workspace/Space/Task data model to internal/store ([28a00c8](https://github.com/spacingmind/smind/commit/28a00c82e73024010779096d7add7e958354f532))
* run registry backend (internal/runs) + wsapi run.* methods ([#19](https://github.com/spacingmind/smind/issues/19)) ([6239064](https://github.com/spacingmind/smind/commit/6239064ff497613b1d9a3294c9227b15ffcebc02))


### Bug Fixes

* close/send race in acp.Client.Prompt ([e31b2ba](https://github.com/spacingmind/smind/commit/e31b2ba7a2ea72f82c7320edca4943d11a2e5003))
* close/send race in acp.Client.Prompt ([25450dd](https://github.com/spacingmind/smind/commit/25450dddc1647077f45eafd13e764baa0370aed2))
* guard final Done event send with ctx.Done() ([9d517a8](https://github.com/spacingmind/smind/commit/9d517a8de79c585e8c1deb40ece1018ce481bfd6))
* remove flaky timing assertion in streaming passthrough test ([bab67dc](https://github.com/spacingmind/smind/commit/bab67dcbf0d858a9d7d493f7046fe13e23004943))
* remove flaky timing assertion in TestProxy_StreamingPassthrough ([18d53c2](https://github.com/spacingmind/smind/commit/18d53c235b0452af5b810a9477c6b7cdb774bd41))
