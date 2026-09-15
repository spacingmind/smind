# Changelog

## [0.6.0](https://github.com/spacingmind/smind/compare/v0.5.0...v0.6.0) (2026-09-15)


### Features

* **accounts:** browser-based OAuth login for anthropic/openai accounts ([#70](https://github.com/spacingmind/smind/issues/70)) ([d213ac8](https://github.com/spacingmind/smind/commit/d213ac8253fed5c9948b0d382af0434ebc8cda18))
* add Codex (OpenAI) as a native third-party provider ([#40](https://github.com/spacingmind/smind/issues/40)) ([03fd0aa](https://github.com/spacingmind/smind/commit/03fd0aa0a96ae57193b7a3511166f523f915c604))
* add Codex (OpenAI) as a native third-party provider ([#40](https://github.com/spacingmind/smind/issues/40)) ([d6ac499](https://github.com/spacingmind/smind/commit/d6ac4990635fbb2e392929acc387577c09ea46f5))
* add Kimi as a second ACP-speaking provider ([#39](https://github.com/spacingmind/smind/issues/39)) ([118419d](https://github.com/spacingmind/smind/commit/118419ddb2c75aa43fe98c7753fc88543680e5ff))
* add Kimi as a second ACP-speaking provider ([#39](https://github.com/spacingmind/smind/issues/39)) ([f6c884c](https://github.com/spacingmind/smind/commit/f6c884c9a02f747e392aae8ec47c4e748448a899))
* **cli:** smind account test &lt;provider&gt; wraps provider.test ([#104](https://github.com/spacingmind/smind/issues/104)) ([f7c2766](https://github.com/spacingmind/smind/commit/f7c276648959164359ab44a34b3c213fd1c5423d))
* **cli:** task send --approval-policy flag + plan notes ([#97](https://github.com/spacingmind/smind/issues/97)) ([e69c507](https://github.com/spacingmind/smind/commit/e69c507cf6b8e4bf344f1a6e8e3da8f5f430e205))
* delete workspace/space (removes smind's tracking only, never on-disk) ([#73](https://github.com/spacingmind/smind/issues/73)) ([0c2b3f4](https://github.com/spacingmind/smind/commit/0c2b3f4e9560659d9c2b097be4ac46bef23ac9ed))
* merge account CLI and unified task dev hot reload ([#49](https://github.com/spacingmind/smind/issues/49)) ([0335bf4](https://github.com/spacingmind/smind/commit/0335bf41a08867bd1761f0584e7a532786796b08))
* merge account CLI and unified task dev hot reload ([#49](https://github.com/spacingmind/smind/issues/49)) ([50e1a6c](https://github.com/spacingmind/smind/commit/50e1a6c402382a9f80105cb1c9111e8fa480da02))
* persist run/conversation history so it survives a daemon restart ([#38](https://github.com/spacingmind/smind/issues/38)) ([e8ee22a](https://github.com/spacingmind/smind/commit/e8ee22a3cb86ccbdf90f9da397cab7f8c6e13f67))
* persist run/conversation history so it survives a daemon restart ([#38](https://github.com/spacingmind/smind/issues/38)) ([60bbc28](https://github.com/spacingmind/smind/commit/60bbc282d00ef1048018eca0e093e42b36ef88b3))
* **store:** add terminal_sessions persistence ([0e3e59a](https://github.com/spacingmind/smind/commit/0e3e59acd2c9abe220fc908fed221b6ceddb11fc))
* **store:** add terminal_sessions persistence ([6634490](https://github.com/spacingmind/smind/commit/663449017bea170986cc18b41a8b70f84ef8136c))
* **store:** PRAGMA-checked additive migration for pre-existing DBs ([#100](https://github.com/spacingmind/smind/issues/100)) ([d45a287](https://github.com/spacingmind/smind/commit/d45a287978a5f67809aef01b8a142f9ccc07a2e4))
* **taskrunner,wsapi,web:** derive accounts-dialog provider list from provider.list (Item 7d) ([#102](https://github.com/spacingmind/smind/issues/102)) ([c971d6b](https://github.com/spacingmind/smind/commit/c971d6b2dd75c7771da8003d85c479075b3da7ec))
* **taskrunner,wsapi,web:** surface CLI-kind providers (GLM) in accounts dialog (Item 7b) ([#99](https://github.com/spacingmind/smind/issues/99)) ([43a5196](https://github.com/spacingmind/smind/commit/43a5196e9c9873c2321b98837064ca49dd970986))
* **taskrunner:** add approval-policy + permission-request timeout (Items 1+2) ([#93](https://github.com/spacingmind/smind/issues/93)) ([0ca93fb](https://github.com/spacingmind/smind/commit/0ca93fb8ff2d86f8e7384e9d92c5eb8b31600165))
* **taskrunner:** allowlist local git add/commit under auto-safe ([#109](https://github.com/spacingmind/smind/issues/109)) ([3898fcc](https://github.com/spacingmind/smind/commit/3898fcc050fb49c4e5dc9effd4890f276c93d1ff))
* **taskrunner:** extract shell commands from ACP execute tool calls ([#119](https://github.com/spacingmind/smind/issues/119)) ([ffd07b1](https://github.com/spacingmind/smind/commit/ffd07b15fe23813e148651bd88206ff64befe65f))
* **taskrunner:** preserve unknown ACP session-update kinds as raw events ([#126](https://github.com/spacingmind/smind/issues/126)) ([d2e8d0a](https://github.com/spacingmind/smind/commit/d2e8d0a40558056cdfbfa2fe9c781562422e00f3))
* **terminal:** survive a daemon restart, mirroring internal/runs ([640d80e](https://github.com/spacingmind/smind/commit/640d80e78e3d3b9a76dcefa648fac80d61e5ab06))
* **terminal:** survive a daemon restart, mirroring internal/runs ([97b8601](https://github.com/spacingmind/smind/commit/97b86016eabb30e067eb929bbb41d89806347ca0))
* **ui:** design tokens, dark mode, primitives + structured/lifecycle events (Items 1,2,7,16) ([#120](https://github.com/spacingmind/smind/issues/120)) ([b421b09](https://github.com/spacingmind/smind/commit/b421b096525f3805c43adc83032d3da282201585))
* **ui:** Item 21 — real responsive/compact shell layout ([#134](https://github.com/spacingmind/smind/issues/134)) ([4565837](https://github.com/spacingmind/smind/commit/45658376418eb39fa6a6b76a3d6f533143d3d6e2))
* **ui:** surface permission_resolved's reason in the timeline ([#125](https://github.com/spacingmind/smind/issues/125)) ([bcf2962](https://github.com/spacingmind/smind/commit/bcf296219c9eb0e2f61e4f13d3a835c5fbf89bb0))
* **ui:** Track A — shell (keyboard, palette, routing, splits, responsive) ([#124](https://github.com/spacingmind/smind/issues/124)) ([728a5db](https://github.com/spacingmind/smind/commit/728a5db65fb1d718d24b61f171695af46786a5d3))
* **ui:** Track B — structured timeline, tool-call cards, composer v2, permissions v2 ([#122](https://github.com/spacingmind/smind/issues/122)) ([7b75b1e](https://github.com/spacingmind/smind/commit/7b75b1edbceb0bcd872c1ffb96ff847274b9a147))
* **ui:** Track C — panes (file explorer/editor, diff/review, terminal, quick-open) ([#123](https://github.com/spacingmind/smind/issues/123)) ([b07acce](https://github.com/spacingmind/smind/commit/b07accec0d1fd137d641fd85194ec03970c15d04))
* **ui:** Track D — settings screen, accounts v2, quota (Items 13-15) ([#127](https://github.com/spacingmind/smind/issues/127)) ([1507384](https://github.com/spacingmind/smind/commit/1507384832aecf1ebcb6e8047b3f7c75a8e4160a))
* **ui:** Track D — sidebar signal, settings, accounts v2, quota ([#121](https://github.com/spacingmind/smind/issues/121)) ([89e31bb](https://github.com/spacingmind/smind/commit/89e31bb3bb2313ad293c3a467c2e02d450e45c8a))
* **web:** add WsClient onClose hook and a browser reconnect wrapper ([7a638e4](https://github.com/spacingmind/smind/commit/7a638e4baf20114d2952d6abe9e32de02528ec2c))
* **web:** add WsClient onClose hook and a browser reconnect wrapper ([7baea05](https://github.com/spacingmind/smind/commit/7baea0535b26a442980f302e045a2f14209b795f))
* **web:** approval-policy selector in run-start prompt form (Item 7a) ([#98](https://github.com/spacingmind/smind/issues/98)) ([15bc3d0](https://github.com/spacingmind/smind/commit/15bc3d052b21102121e96ed3e31ac6f44882a6ca))
* **web:** CRUD UI for workspace/space/task create, archive, accounts ([#69](https://github.com/spacingmind/smind/issues/69)) ([d3ee082](https://github.com/spacingmind/smind/commit/d3ee082f9745d6a6f0e8706bdc67bce53ac52dc0))
* **web:** drive App.tsx's connection status from real socket events ([97fa3ab](https://github.com/spacingmind/smind/commit/97fa3abee4f3b9393963cc09840a045b43a72d7c))
* **web:** drive App.tsx's connection status from real socket events ([fd97f12](https://github.com/spacingmind/smind/commit/fd97f12333fc77b8ce4e8a70d447b77595e019a0))
* **web:** editor preview pane (markdown, svg, sandboxed html) ([#51](https://github.com/spacingmind/smind/issues/51)) ([b98d26f](https://github.com/spacingmind/smind/commit/b98d26ffcdc5edbbcfe9cd83b777d7253a654b6e))
* **web:** editor preview pane (markdown, svg, sandboxed html) ([#51](https://github.com/spacingmind/smind/issues/51)) ([5a520ff](https://github.com/spacingmind/smind/commit/5a520ff0d20972c9e141a0a674065a393c4c2977))
* **web:** file conflict detection with reload/overwrite ([#62](https://github.com/spacingmind/smind/issues/62)) ([114800c](https://github.com/spacingmind/smind/commit/114800ca562bbb6fcbe143308588db4ef59963e4))
* **web:** file conflict detection with reload/overwrite ([#62](https://github.com/spacingmind/smind/issues/62)) ([0839cf5](https://github.com/spacingmind/smind/commit/0839cf578625ba967169387199073d58d4eae22e))
* **web:** live UI on event subscriptions ([#59](https://github.com/spacingmind/smind/issues/59)) ([7c4e881](https://github.com/spacingmind/smind/commit/7c4e8815fe021f5882e543a06a4d1072e808427e))
* **web:** live UI on event subscriptions ([#59](https://github.com/spacingmind/smind/issues/59)) ([fda2c5b](https://github.com/spacingmind/smind/commit/fda2c5b9e78337a4a1145e23e77a6bf73d2796dd))
* **web:** per-file staging and commit flow ([#61](https://github.com/spacingmind/smind/issues/61)) ([43adcb3](https://github.com/spacingmind/smind/commit/43adcb33f9dad05b45763ed5f075dade0c65651f))
* **web:** per-file staging and commit flow ([#61](https://github.com/spacingmind/smind/issues/61)) ([7a2fb9b](https://github.com/spacingmind/smind/commit/7a2fb9bd851060a8b9066429caee9a343b59e0e6))
* **web:** pin permission card near composer, attention notifications, resizable layout (Items 3, 4, 6) ([#94](https://github.com/spacingmind/smind/issues/94)) ([9454f33](https://github.com/spacingmind/smind/commit/9454f33c254b97877d8808287a1a098105e7f1d9))
* **web:** provider dropdown from provider.list ([#60](https://github.com/spacingmind/smind/issues/60)) ([d5a2e2d](https://github.com/spacingmind/smind/commit/d5a2e2d2e2fd33fcea19313dfaf12785d2d41fb1))
* **web:** provider dropdown from provider.list ([#60](https://github.com/spacingmind/smind/issues/60)) ([e53d6d8](https://github.com/spacingmind/smind/commit/e53d6d880b5ef2d47efe778dc1c326914b907524))
* **web:** resync TaskDetailPane/TerminalPane after a reconnect ([cf8550c](https://github.com/spacingmind/smind/commit/cf8550c571d2547488a747445d6aaadf4325b5f0))
* **web:** resync TaskDetailPane/TerminalPane after a reconnect ([fe9b995](https://github.com/spacingmind/smind/commit/fe9b995bd5b54e128e784aabe31626a4b6237fd2))
* **web:** server-side folder picker for New workspace's Path field ([#72](https://github.com/spacingmind/smind/issues/72)) ([1caab67](https://github.com/spacingmind/smind/commit/1caab675fb4ebb2f5df9a0324407243a77dea9fe))
* **web:** tab registry with per-task scoping and attention badges ([#53](https://github.com/spacingmind/smind/issues/53)) ([e1141ad](https://github.com/spacingmind/smind/commit/e1141adfb7b632c5ce2939693e08810c953eb182))
* **web:** tab registry with per-task scoping and attention badges ([#53](https://github.com/spacingmind/smind/issues/53)) ([7df8514](https://github.com/spacingmind/smind/commit/7df851426b3318028e8f4c2119ce4f868fe42215))
* **workspace,wsapi,web:** task.createPr RPC + Create PR button in diff view (Item 5) ([#96](https://github.com/spacingmind/smind/issues/96)) ([d3e9fed](https://github.com/spacingmind/smind/commit/d3e9fed8bca5aff0b3c7ee92b192fa7f34ca7bc6))
* **wsapi,web:** provider.test diagnostic RPC + health status and Test button in accounts dialog (Item 7c) ([#101](https://github.com/spacingmind/smind/issues/101)) ([c50b2a9](https://github.com/spacingmind/smind/commit/c50b2a91abe9fb1a8a30938d883d469b76d4e03c))
* **wsapi:** event subscription RPC ([#54](https://github.com/spacingmind/smind/issues/54)) ([6947ba8](https://github.com/spacingmind/smind/commit/6947ba840b8e89dce62b9ec4a5ddf2f3a213086f))
* **wsapi:** event subscription RPC ([#54](https://github.com/spacingmind/smind/issues/54)) ([3fda4ec](https://github.com/spacingmind/smind/commit/3fda4ecbc801e64b3ac2868f2186c64d1f1aec5d))
* **wsapi:** thread store persistence into terminal.Registry construction ([bc0a893](https://github.com/spacingmind/smind/commit/bc0a89374c8be63686fe92cc1694d517710a8316))
* **wsapi:** thread store persistence into terminal.Registry construction ([f1c6a6f](https://github.com/spacingmind/smind/commit/f1c6a6ff032215540b257c0972e6367c3decca76))


### Bug Fixes

* **accounts:** accept epoch-millis/seconds expires_at in OAuthCredential import ([#77](https://github.com/spacingmind/smind/issues/77)) ([b3230df](https://github.com/spacingmind/smind/commit/b3230dfc6fbe1456f881cc6205d55963c1d66aaa))
* kill in-flight run.start subprocesses on daemon shutdown ([#37](https://github.com/spacingmind/smind/issues/37)) ([93e8633](https://github.com/spacingmind/smind/commit/93e8633828c7fad0b803f56e6b6ae1cabafcbd14))
* kill in-flight run.start subprocesses on daemon shutdown ([#37](https://github.com/spacingmind/smind/issues/37)) ([210d14c](https://github.com/spacingmind/smind/commit/210d14c2cf7daccd5cc28702c5533cc623366696))
* persist a run's terminal status before it becomes visible in memory ([#42](https://github.com/spacingmind/smind/issues/42)) ([7933bd6](https://github.com/spacingmind/smind/commit/7933bd6275182df72fef98066e816e3ec3d89a7c))
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
* **terminal:** close checkpoint/finish write race and reap zombies on Create failure ([6d9ff5b](https://github.com/spacingmind/smind/commit/6d9ff5b5539fa88da84d075920ac8af94ad87fd2))
* **terminal:** close checkpoint/finish write race and reap zombies on Create failure ([1a19ae7](https://github.com/spacingmind/smind/commit/1a19ae7856ae854c82ed2dc4e3d32fd486e3ecea))
* **ui:** wire the two missing Item 6 side-dock entry points, add the promised terminal detach-not-stop test ([#133](https://github.com/spacingmind/smind/issues/133)) ([cc79922](https://github.com/spacingmind/smind/commit/cc79922793ef3504173667c25f7203c8940f90c6))
* **ux:** stop leaking a raw stat error, tighten guide copy at 3 spots ([#74](https://github.com/spacingmind/smind/issues/74)) ([1a51f0a](https://github.com/spacingmind/smind/commit/1a51f0af1016e2072d4401270c026cabcf82ce0c))
* **web:** clip sidebar container overflow so long content can't bleed out ([#78](https://github.com/spacingmind/smind/issues/78)) ([2c65046](https://github.com/spacingmind/smind/commit/2c650467edc6dd5184b12e2e5c316a7e71820f49))
* **web:** close the remaining min-w-0 gap in the sidebar's own flex chain ([#76](https://github.com/spacingmind/smind/issues/76)) ([8f55cd1](https://github.com/spacingmind/smind/commit/8f55cd1a2bd8e56204f5774b61c45f8f67ab747e))
* **web:** handleClose must reset lastTerminalIdRef, not just terminalId ([59079b9](https://github.com/spacingmind/smind/commit/59079b9496f922a4dbb8f5927ed689f5ec460cbf))
* **web:** handleClose must reset lastTerminalIdRef, not just terminalId ([0dc52fb](https://github.com/spacingmind/smind/commit/0dc52fb103d8a4cd93a836ea269a372f78d160ba))
* **web:** isolate one throwing onClose callback from its siblings ([bc60060](https://github.com/spacingmind/smind/commit/bc60060de4b3805f0980626f47c2b4c189c67731))
* **web:** isolate one throwing onClose callback from its siblings ([8f21c1d](https://github.com/spacingmind/smind/commit/8f21c1d5b29f69927859af946154a5f6513545c7))
* **web:** long names no longer overflow/overlap in sidebar, tabs, and panes ([#75](https://github.com/spacingmind/smind/issues/75)) ([a98e1e2](https://github.com/spacingmind/smind/commit/a98e1e273fcfd9700587441239f57e4ebd996b31))
* **web:** reconnect must resolve previousId to its exact session ([b9f550e](https://github.com/spacingmind/smind/commit/b9f550e5111b67944f208614b01a77557d3b2c73))
* **workspace:** checkpoint task work before archiving ([#52](https://github.com/spacingmind/smind/issues/52)) ([f5233a1](https://github.com/spacingmind/smind/commit/f5233a180a29a526f91fabc5e0a697e4d3f84dba))
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
