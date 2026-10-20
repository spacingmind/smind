# Changelog

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
