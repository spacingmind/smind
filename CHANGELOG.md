# Changelog

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
