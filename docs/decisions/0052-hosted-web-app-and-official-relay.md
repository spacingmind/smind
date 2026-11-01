# 0052: Hosted web app + official relay (app.spacingmind.com / relay.spacingmind.com)

## Status

**Proposed — 2026-11-01. A DRAFT FOR THE HUMAN. No code, nothing built, nothing decided.** This document lays out the options, what each
costs and what each puts at risk, and ends with the decisions only the Human can make ([H1–H14](#human-decisions)). Nothing here is a recommendation
to merge anything; a "Lead's lean" is marked as one and is not a decision.

What *is* decided elsewhere and is not reopened here: the hosted relay is named `relay.spacingmind.com`, DNS on Cloudflare, with an accepted trust
model, deploys through a GitHub Environment (ADR-0031 D11; **hosting still pending**, decisions 12 and 27–32, 36 open, PRs P14–P16 blocked); the
relay's E2EE is v2 (ADR-0035); admission is SCRAM-style (ADR-0011); a hosted relay was kept **out of** the fleet work (ADR-0048 D2, "hosted relay
stays excluded"). [ADR-0051](0051-web-demo-mode-and-app-spacingmind-com.md) (Proposed) puts a **demo-only** UI at `app.spacingmind.com`; this ADR is about what happens
if that hostname is later asked to talk to *real* daemons.

Inputs: ADR-0007 (relay architecture), ADR-0011 (admission), ADR-0013 (bundled UI, same-origin `CheckOrigin`), ADR-0031 D8 (direct connections with a
password), D9 (multi-device), D11 (hosted relay), D35 (device identity), ADR-0035 (E2EE v2), ADR-0040 (mobile push), ADR-0045/0047 (inbox,
permission policy floor), ADR-0048 (fleet), `refs/paseo` (read for this ADR; files cited below).

## Context

Today a browser reaches a smind daemon in exactly one way: **it loads the UI from the daemon itself** (`internal/server`, ADR-0013) and the page talks to its
own origin — `GET /api/token` on loopback, or password login → a session token carried as a WebSocket subprotocol (`lib/auth.ts`, `lib/ws-client.ts`).
`CheckOrigin` is gorilla's default (an `Origin` must equal the request's `Host`), the `Host` header is validated against an allowlist (DNS-rebinding guard,
`internal/server/direct.go`), and there is no CORS. A page served from any *other* origin cannot connect, by design.

Phones and the desktop app reach a daemon behind NAT through the **relay**: the daemon dials out (gRPC), the device attaches (native gRPC on desktop; **grpc-web over WebSocket**
on mobile, `mobile/src/relay/grpcweb.ts`), and everything inside is **E2EE v2** (X25519 + ChaCha20-Poly1305, Noise-NK-shaped, pairing offer in a URL fragment,
device identity by Ed25519 `device_hello`). Only a self-hosted relay exists; `relay.spacingmind.com` is a name, not a service.

The Human is considering two new things: a hosted **web app** that connects to users' daemons, and an **official relay** to make that (and mobile) work with no
infrastructure on the user's side. They are separable, and the order matters (see [Staging](#staging-not-a-decision)).

### What Paseo does (read for this ADR)

Paseo is the closest shipped precedent: a daemon, a mobile app, a CLI and a **hosted web app at `app.paseo.sh`**, with a hosted relay at `relay.paseo.sh`.

- **One app, three shells.** The web app is the same Expo/React-Native-web code as mobile, deployed to Cloudflare Pages on release tags
  (`refs/paseo/.github/workflows/deploy-app.yml`). The daemon *can also serve that UI itself* (`features.webUi.enabled`, `public-docs/web-ui.md`: "the UI you serve always
  matches your daemon version"), which is Paseo's answer to "I don't want to trust the hosted copy".
- **Pairing = a URL whose fragment carries the offer.** `https://app.paseo.sh/#offer=<base64url>` (daemon id, daemon public key, relay endpoint;
  `packages/protocol/src/connection-offer.ts`, `packages/server/src/server/pairing-offer.ts`). The fragment is never sent to a server, so the hosting origin does not see the key
  (`SECURITY.md`). The same link is pasted into the web app (`components/pair-link-modal.tsx`) or scanned by the phone. smind's offer already has the same shape (ADR-0007 (g)).
- **Two ways in** (`SECURITY.md`): the relay (daemon dials out; clients meet it; E2EE) or a **direct** connection (`tcp://host:port?ssl=true&password=…`,
  `packages/protocol/src/daemon-endpoints.ts`).
- **Direct from the hosted origin is an allowlist, on by default for Paseo's own origin.** The daemon's default config is `cors.allowedOrigins: ["https://app.paseo.sh"]`
  (`packages/server/src/server/persisted-config.ts:348`); CORS headers are emitted for allowed origins (`bootstrap.ts:717-745`) and the WebSocket upgrade rejects an `Origin`
  that is neither allowed nor same-origin (`websocket-server.ts:880-908`, "Origin not allowed"). A password rides in `Sec-WebSocket-Protocol: paseo.bearer.<password>`. A `Host`
  allowlist blocks DNS rebinding (`SECURITY.md`). I found **no Private-Network-Access handling** in `packages/{app,server,client,relay}/src` (searched for `Private-Network`, `mixed content`).
- **Relay** (`packages/relay`): a Cloudflare Worker + Durable Object per `serverId` with a daemon **control socket** and a per-client **data socket** (`cloudflare-adapter.ts:125-127`),
  a 200-frame store-and-forward buffer, **no accounts** (the open, secret-less relay model; `SECURITY.md` "Relay threat model"). E2EE is NaCl `box` with an ephemeral client key and the daemon's
  static key, random 24-byte nonces; **no replay protection inside a session** (stated in `SECURITY.md`; ADR-0035 already does better). Its `wrangler.toml` currently forwards to a Fly
  upstream (`PASEO_RELAY_UPSTREAM`), which ADR-0031 D11 noted.
- **Trust statement.** Paseo's SECURITY.md says the *relay* is untrusted. It does **not** say anything about trusting `app.paseo.sh`'s JavaScript, which holds the same keys the relay
  is kept away from. That gap is the central problem of this ADR.

## The options

Three connection shapes for a hosted web app, plus the no-hosted-app baseline. They can be combined.

### Option A — browser → official relay, E2EE v2 in the browser

The hosted page is a relay **device** like the phone: it takes the offer from the fragment, opens grpc-web-over-WebSocket to `relay.spacingmind.com`, performs `Admit`, runs the E2EE v2
handshake and carries the existing wsapi JSON inside the encrypted frames.

What exists: the relay's grpc-web listener (`internal/relay/server/grpcweb.go`) already accepts any `Origin`, because admission, not origin, is the gate (comment at `grpcweb.go:39-49`);
the TypeScript client **exists** in `mobile/src/relay/` (`grpcweb.ts`, `e2ee.ts`, `admission.ts`, `deviceIdentity.ts`) and is pure JS (`@noble/curves`, `@noble/ciphers`, `@noble/hashes`), checked against
the Go-generated vectors (`internal/relay/e2ee/testdata/e2ee_v2_vectors.json`). It uses RN's `fetch`/`WebSocket`, which are the browser's. A browser client is mostly a port of that module
set into a shared package plus a `SocketLike` adapter so `WsClient` runs over the encrypted channel.

**WebCrypto.** Browsers can do part of E2EE v2 natively; not all:

| Primitive | WebCrypto | Notes |
|---|---|---|
| X25519 | yes in current evergreen browsers (Chrome 133+, Firefox 130+, Safari 17+; **verify the support floor at decision time**) | the *ephemeral private key can be non-extractable*; one DH output (`es`, `ee`) still has to be exported to run the schedule |
| HKDF-SHA-256, HMAC-SHA-256, SHA-256 | yes | |
| **ChaCha20-Poly1305** | **no** (WebCrypto offers AES-GCM, not ChaCha) | needs a JS/WASM implementation (`@noble/ciphers`, already in the repo) |
| Ed25519 (`device_hello`) | yes in the same floor | a **non-extractable device key in IndexedDB** is possible; it prevents *exfiltration* of the key, not *use* by hostile script on the same origin |

So "E2EE in WebCrypto" means a **hybrid**: WebCrypto for X25519/HKDF/HMAC/Ed25519 where it helps, noble for ChaCha20-Poly1305, and the **symmetric session keys live in JS memory** regardless.
Non-extractability protects the ephemeral and the device key at rest, not the session in use. Do not oversell it.

Cost and effort (not a decision): moving the relay client out of `mobile/` into a package web and mobile share is a cross-owner change (`mobile/` has its own Lead); the alternative is a
second copy. Roughly: one L-sized item for the web relay client (browser adapters, key storage, reconnect/resume, `WsClient` over the channel, pairing UI, tests against the existing vectors and
`relaytest`), one S item to wire it into the UI's connect flow.

Needs from the relay: reachable from a browser (CA-verified TLS — **a self-signed relay cannot work in a browser** — and WebSockets through Cloudflare), the single-port grpc-web mode that ADR-0031 D11/36 already plans, and the
`Origin` policy decision ([H10](#human-decisions)).

### Option B — browser → daemon directly

The hosted page connects straight to `wss://<daemon>` using the existing password login (`POST /api/login` → session token → subprotocol). No relay, no E2EE (TLS is the transport security).

Browser constraints a hosted `https://app.spacingmind.com` page runs into:

1. **Mixed content.** An `https` page may not open `ws://` to a LAN address in any browser; only `wss://`. For `localhost`/`127.0.0.1`, Chrome and Firefox treat the target as potentially trustworthy and allow `ws://`;
   **Safari has historically blocked it** (WebKit treats the page as mixed content), so *Safari users cannot reach a plain-HTTP daemon, even their own localhost, from a hosted page* (**verify on a current Safari before relying on this**). Reaching a LAN
   daemon from the hosted app therefore needs a **real certificate on the daemon** (Tailscale HTTPS certs, a reverse proxy with a public name, `smind` with built-in TLS, which ADR-0031 decision 10 left out of v1).
2. **Chrome Private Network Access / Local Network Access.** A public site reaching a loopback or private-IP address is gated by Chrome: first as a CORS-preflight rule (`Access-Control-Request-Private-Network` →
   the daemon must answer `Access-Control-Allow-Private-Network: true`), and, as I understand the 2025–26 direction, a **user permission prompt** ("access devices on your local network") that replaces/extends that rule. WebSocket
   handshakes are not CORS-preflighted but are in scope of the permission. **I have not verified the exact current behaviour per Chrome version, and Firefox/Safari differ; it must be tested on real browsers before this option is chosen.**
3. **Origin allowlist on the daemon.** Today's `CheckOrigin` accepts only same-origin, and there is no CORS. Option B needs a config like `server.allowedOrigins` (Paseo ships with its own origin allowed by default; smind
   would have to choose a default — [H8](#human-decisions)), CORS + preflight handling for `/api/login`, `/healthz`, and the PNA header. Every one of these widens the daemon's attack surface, which is a terminal (remote code execution)
   behind a password.
4. **DNS rebinding and `Host`** stay as they are; a hosted origin does not weaken them.

What it has going for it: no relay to operate, no relay cost, no metadata to a third party, and it works for the LAN/Tailscale homelab users that ADR-0031 D8 designed for. What it lacks: it does nothing for NAT (a laptop on a café network, a
daemon behind CGNAT), which is the relay's whole reason to exist, and it depends on browser policy that is in motion.

### Option C — both, one hosted bundle (the Paseo shape)

Relay for off-LAN and mobile, direct for LAN and localhost, in the same hosted app, chosen per host entry. Strictly more work than A or B alone; the least bad for users; the largest attack surface.

### Option D — no hosted app (the baseline)

Keep the demo ([ADR-0051](0051-web-demo-mode-and-app-spacingmind-com.md)); real daemons keep serving their own UI (`smind serve`, already same-origin, version-matched, no trust beyond the daemon); mobile and desktop use the relay. Optionally
ship the **same bundle** as a release asset / container so a user can host it on a name they control ("self-hosted app"). Costs nothing new, closes the trust problem completely, and gives the web user who is *away from the daemon* nothing — they use the mobile or desktop app.

## The trust problem: hosted JavaScript holding E2EE keys

E2EE v2 protects against **the relay operator and anyone on the path**. For a *native* app that claim stands, because the code is signed and fixed per release. For a **web app served from `app.spacingmind.com` it does not hold against the
party that serves the page**: whoever controls what `app.spacingmind.com` returns on the next load controls the code that holds the session keys, the device key and the plaintext. Concretely, for **browser users** the guarantee degrades to
"you trust the operator (smind, and Cloudflare, who terminates TLS and stores the bundle) not to ship malicious JS, **and** you trust that no one else can make them do so": a hijacked deploy token, a malicious dependency, or a compromised Cloudflare
account is a **remote-code-execution on every daemon whose owner has paired the hosted web app**, because the daemon's terminal and agents sit behind that session. Under Option B (no E2EE) the exposure is the same in kind. This must be stated to users in plain
words, in the app and in the docs, whichever option is chosen.

Mitigations, which stack and none of which makes the problem go away:

1. **Self-hosting the same bundle** (Option D's release asset, and the daemon-served UI, which already exists). The only complete mitigation; makes the hosted app a convenience, not the only way.
2. **Immutable, versioned builds.** Every release is served at `app.spacingmind.com/v/<semver>/` forever; `/` is a thin redirector. A user or an org can pin a version; a bad deploy cannot silently rewrite a pinned URL. Cheap with static assets.
3. **A published hash and a reproducible build.** The release workflow publishes the SHA-256 tree hash of the bundle with the GitHub release, signed (sigstore/cosign), built reproducibly so anyone can rebuild and compare. A small verifier
   (a CLI command `smind app verify`, or a browser extension in the spirit of Meta/Cloudflare's *Code Verify*; details unverified) compares what the browser received to the published hash. Protects against a *targeted or silent* swap, only for people who verify.
4. **SRI** on every subresource. Honest scope: it stops a **CDN/subresource** swap but not a malicious `index.html`, which carries the hashes; it is a layer, not a guarantee.
5. **A strict CSP and no third parties** (no analytics, no CDN fonts/scripts, `connect-src` limited to `relay.spacingmind.com` plus origins the user adds), Trusted Types for DOM sinks, `frame-ancestors 'none'`. Reduces XSS and supply-chain script injection, not a malicious deploy.
6. **Service-worker pinning with explicit upgrade**: the app installs a version and only moves to a new one when the user accepts. TOFU on first load; helps against silent mid-session swaps.
7. **Device capability tiers.** The daemon already authenticates devices by Ed25519 identity (D35). Add a **device class** to the pairing grant: a hosted-web device gets a *narrower default scope* than a phone or the desktop app (for example: read, send prompts and answer permissions, **no terminal, no file write**,
   unless the daemon owner enables them), so the *worst case* of a compromised bundle is bounded by what the owner granted rather than "everything". This does not exist today; it is the mitigation that changes the blast radius, and the one most worth the Human's attention ([H5](#human-decisions)).
8. **Operational controls on the deploy path**: protected GitHub Environment with required reviewers (as for `relay-production`), a token scoped to one Worker, deploy only from tagged releases, a kill switch that serves a "temporarily disabled" page, an audit trail.
9. **Key hygiene in the browser**: non-extractable device key in IndexedDB; the session ephemeral is per tab and discarded; nothing in `localStorage`; the password never stored (as `lib/auth.ts` does today). Safari's storage eviction for sites a user has not interacted with for a while
   (ITP, about seven days for script-writable storage unless installed as a PWA; **verify**) will drop a stored device key and force a re-pair, which is a usability cost of the hosted app, not a security failure.

## Relay: cost, limits, abuse, operations

The relay is a dumb pipe of ciphertext; cost scales with **connected devices × time** and **bytes**, not CPU. ADR-0031 D11 already records the options (Cloudflare gRPC proxy + Go relay on a VPS/Cloud Run; Cloudflare Containers via inbound gRPC, private beta; Workers + Durable Objects
WebSocket rewrite, withdrawn) and the **enrolment and abuse defaults** (≤ 5 enrolments/IP/hour and ≤ 20/day, ≤ 50,000 hosts, never-admitted hosts deleted after 24 h and idle ones after 90 days, ≤ 16 devices per host, ≤ 1 MiB per message, ≈ 1 MiB/s sustained and a soft
≈ 20 GB/month per host, ≤ 8 concurrent attempts per IP, an operator ban list). A hosted web app adds to those:

- **Browser tabs are devices.** Each open tab holds one long-lived data stream; a user with five tabs consumes five of the host's 16 device slots and five idle connections. The app should hold **one** relay connection per browser (a SharedWorker / BroadcastChannel leader) or the limit
  must be revisited. Idle-connection cost is the dominant cost line on a connection-billed host (Durable Objects, Containers) and nearly free on a VPS.
- **A browser-reachable relay is reachable from any website's script.** Admission (host id + proof of the host secret, ADR-0011) is the gate, not origin: an `Origin` allowlist on the grpc-web listener stops *other sites from embedding the relay as a service*, does nothing against non-browser abusers, and would have to allow
  self-hosted app origins ([H10](#human-decisions)). Rate limits by IP and by host id stay the real abuse control.
- **Content is opaque, so abuse handling is by volume and metadata** (D11): a host that moves terabytes is rate-limited or banned by host id; there is no content moderation and the privacy page must say so. The relay will be used to carry traffic the operator would not choose (an agent that downloads a lot); the quota is the lever.
- **ToS and privacy before launch.** IP addresses are personal data in most jurisdictions; a privacy notice and a retention rule (D32) are required before the hosted default is switched on, as D11 already says. A hosted *app* additionally needs terms (acceptable use, no warranty, no SLA, the explicit statement of the trust problem above) and a named legal entity/contact that **only the Human can supply** ([H11](#human-decisions)).
- **Uptime and ops.** A single VPS relay is a single point of failure; clients already reconnect with backoff and resume, so an outage is an inconvenience rather than data loss (the daemon keeps running; sessions re-establish). A status page, an external synthetic check, billing alerts on Human-owned accounts (D11) and an explicit **"best effort, no SLA"** statement are the minimum. Who is on call is a Human question; today the answer is "nobody off-hours".
- **A kill switch** for the hosted app (serve a static notice) and for the relay (refuse admission) belongs in the plan, with the deploy path in (8) above.

## How mobile benefits

- **The official relay is the mobile feature.** The phone app today needs a user-run relay with a self-signed certificate; D11 notes mobile does not pin and probably does not work with a self-signed relay on a real device. A CA-verified `relay.spacingmind.com` is what makes "install the app, scan the QR, it works" real, with or without any hosted web app. P14–P16 are blocked on D11 decision 12, not on this ADR.
- **A hosted web app on a phone** is a zero-install fallback (for a platform without a store build, or a quick look from someone else's device). It has no native push (ADR-0040 is native), loses its device key to storage eviction on iOS Safari unless installed as a PWA, and runs the trust problem above on a device where the user can least verify anything. It is a convenience, not the mobile strategy.
- **The web relay client and the mobile relay client are the same code** if it is factored into a shared package: one E2EE implementation under test against the Go vectors instead of two. That is a benefit to mobile's correctness, and a coordination cost with the mobile owner.

## Staging (not a decision)

The pieces are separable and have a natural order, offered so the Human can accept some and defer others:

0. **Demo only** — [ADR-0051](0051-web-demo-mode-and-app-spacingmind-com.md), no daemon, no relay. Independent of everything below.
1. **Hosted relay** — D11 decision 12 (hosting), P14–P16. Benefits mobile and desktop immediately; needs no web client.
2. **Web relay client** — Option A, against *any* relay (self-hosted first), served from the daemon's own UI at first (no new trust), then optionally from a hosted origin.
3. **Hosted origin for real daemons** — only after the trust mitigations the Human requires exist (immutable builds, published hash, device capability tiers).
4. **Direct from a hosted origin** (Option B) — last, behind browser testing, because it widens the daemon rather than adding a service.

Whatever happens, `app.spacingmind.com` currently belongs to the demo (ADR-0051): a real app either replaces the demo, serves it from a path (`/demo`), or gets another hostname ([H7](#human-decisions)).

## Alternatives considered (to the whole idea)

- **Do nothing; keep self-serve UI + relay for native apps (Option D).** Zero new risk; leaves "I want it in any browser, anywhere" unmet.
- **A hosted app that only ever talks to the user's own self-hosted relay** (the user provides the relay URL in the pairing offer). Removes `relay.spacingmind.com` from the web story and the operator's cost; keeps the hosted-JS trust problem.
- **Cloudflare Tunnel / Tailscale Funnel for the user's daemon, daemon-served UI.** No smind infrastructure at all; works today with `server.hostnames` and a password (D8); the user brings the name and the certificate.
- **A browser extension or desktop-app "bridge" holding the keys** with the hosted page as a dumb renderer. Solves the key-holding problem properly (keys never in page JS) at the cost of an install, which defeats "zero install" for the one audience that wanted it.

## Human decisions

None of these is decided by this ADR. Where a "Lead's lean" is given it is input, not a decision.

- **H1. Do we want a hosted web app that connects to real daemons at all**, or stop at the demo (ADR-0051) plus daemon-served UI and the native apps (Option D)? *Lead's lean: decide after H3 and H4; the hosted relay is worth doing on its own.*
- **H2. Which transports**: A (relay only), B (direct only), C (both), D (none)? If B, which browsers must be supported (Safari included)? *Lead's lean: if any, A first; B only after real-browser testing of mixed content and Local Network Access.*
- **H3. Sequencing against the hosted-relay hosting decision** (ADR-0031 decision 12): must ADR-0052 wait for it, or may a web relay client be built against a self-hosted relay first?
- **H4. Trust posture for hosted JS**: which of mitigations 1–9 are **required before** a hosted origin may reach real daemons (immutable versioned builds? a signed, reproducible published hash? self-hosting as a first-class documented path? a kill switch?), and what user-facing disclosure wording is acceptable.
- **H5. Capability tiers**: should a hosted-web device get a narrower default scope than a phone/desktop (read + prompt + permissions, no terminal/file-write unless the owner enables it)? This needs a device-class field in the pairing grant (a protocol and policy change, ADR-0047 interplay).
- **H6. Browser key storage**: non-extractable IndexedDB device key (re-pair after eviction), per-session ephemeral only (re-pair every visit), or an optional passphrase-wrapped key?
- **H7. What `app.spacingmind.com` is**: the demo only, the real app only (demo moves to `/demo` or `demo.spacingmind.com`), or both from one bundle (`?demo`)?
- **H8. Daemon-side origin policy for Option B**: `server.allowedOrigins` empty by default and opt-in per origin (smind's own hosted origin included), or allowed by default as Paseo does for `app.paseo.sh`? Also whether to add built-in TLS (ADR-0031 decision 10) to make `wss://` practical.
- **H9. Self-hosters**: do we publish the web bundle as a release asset and/or container so anyone can serve it from a name they control, and may that bundle connect to other daemons (as the daemon-served UI could)?
- **H10. Relay `Origin` policy** for browser clients: keep permissive (admission is the gate), or allowlist `app.spacingmind.com` plus operator-configured origins?
- **H11. Operator and legal**: who operates `relay.`/`app.` (the entity named in the terms), who writes and owns the ToS and privacy notice, the retention period for relay logs (D32), and the jurisdiction question for IP addresses.
- **H12. Uptime and support posture**: confirm "best effort, no SLA, no accounts", a public status page, who is on call, and the monthly spend ceiling at which the relay is throttled or paused (D11 decision 27).
- **H13. Abuse limits**: confirm the D11 defaults (enrolment, hosts, devices per host, bandwidth, per-IP) and whether a browser tab/leader policy (one relay connection per browser) is required in v1.
- **H14. Analytics on the hosted app**: none (default of ADR-0051), or privacy-preserving aggregate only (a CSP and privacy-notice change).

## Consequences (if the Human proceeds)

Each option, once chosen, becomes its own ADR/plan with acceptance criteria; this document is the shared background. Choosing nothing costs nothing: ADR-0051's demo ships without it, and the relay and mobile paths (D11) are unaffected.
