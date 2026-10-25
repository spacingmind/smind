# Homelab quickstart: reach a daemon from another device

> **Operator notes.** This page is for people who run a smind deployment: service
> units, the threat model of a remote daemon, hosting a relay, Docker details and
> backups. The user-facing how-tos are in [public-docs/](../public-docs/index.md):
> [Run the daemon](../public-docs/running-the-daemon.md) and
> [Mobile and relay pairing](../public-docs/mobile-and-relay.md).

The smind daemon (`smind serve`) listens on `127.0.0.1:4648` by default. To use it from a laptop, a phone or the desktop app
on another machine, you connect to it one of two ways:

| | Direct connection | Relay |
|---|---|---|
| What it is | The client talks to the daemon's port | The daemon dials **out** to a relay; clients dial the same relay; the relay forwards opaque ciphertext |
| Needs an open port on the daemon | Yes | No (only the relay needs reachable ports) |
| Works through NAT / CGNAT, phones off your LAN | No | Yes |
| Choose it when | The devices share a LAN or a VPN (Tailscale, WireGuard) | The daemon is behind NAT, or you want a phone to reach it from anywhere |

Terminology: a **relay host** is one daemon's mailbox on a relay (a host id plus an admission secret). Connecting a
daemon to a relay registers **this daemon once** with that relay; every paired client then sees **all** of the daemon's
workspaces, spaces and tasks. Nothing is created in smind. (Older docs and the wire format call this a "workspace";
smind's own workspaces are a different thing: registered git repositories.)

## Run the daemon as a service (Linux, macOS and Windows)

`smind serve` runs in the foreground and stops with your terminal. To keep the daemon running across logouts and
reboots, let your OS service manager supervise it. smind has no background `smind start` of its own: the service manager
is the supervisor.

```sh
smind service install        # systemd user unit (Linux) or LaunchAgent (macOS); enables it and starts the daemon
# or in one step with the installer:
curl -fsSL https://spacingmind.com/install.sh | sh -s -- --service       # Linux, macOS
$env:SMIND_SERVICE = 1; irm https://raw.githubusercontent.com/spacingmind/smind/master/scripts/install.ps1 | iex   # Windows (PowerShell)
```

| Command | What it does |
|---|---|
| `smind service install [--system] [--home <dir>] [--path <PATH>] [--no-start]` | Writes the unit, enables it, starts it. Safe to repeat: an unchanged unit is not rewritten and a running daemon is never restarted by it. |
| `smind service status` | State, whether the service is enabled, pid, port, and the daemon's version next to this binary's. Exit code 0 running, 3 stopped, 4 not installed. |
| `smind service start` / `stop` / `restart` | Drive the unit. `smind update` calls `restart` for you when the daemon is service-managed. |
| `smind service uninstall` | Stops and removes the unit. Your data in `$SMIND_HOME` is never touched. |
| `smind service logs [-f] [-n N]` | Tails the daemon's own log file (below). |

The port and every other setting come from `config.yaml` (there is no `--port`); the unit starts the same
`smind serve` you would run by hand. Once installed, `status`, `start`, `restart` and `logs` use the `SMIND_HOME` written
in the unit (not the one in your current shell), so they always look at the daemon's own data. `install` captures the `PATH` of the shell you run it from, so `git`, `node` and
your agent CLIs resolve inside the service (override with `--path`), and `SMIND_HOME` when you have it set (or `--home`).
A daemon password (`smind set-password`, stored as a hash in `config.yaml`) and `server.listen` work under every
service because the service starts the same `smind serve`: nothing secret is ever written into the unit, plist or task (the
Windows task's command line and XML are readable by every local user), and `SMIND_PASSWORD`, `SMIND_LISTEN` and
`SMIND_HOSTNAMES` from your shell are **not** carried into it, so set them in `config.yaml` with `smind config set`. The
service's own probes and shutdown request use the daemon's loopback address and its token file, as `smind stop` does.
If another daemon already answers on the configured port (a `smind serve` you started, or the desktop app's own),
`install` enables the unit but does not start it and says why; it never stops a process it did not start.

**Linux: user unit (default).** The unit lives in `~/.config/systemd/user/smind.service` and runs as you. A user unit
only runs while you are logged in unless *lingering* is on, so for a headless homelab box run
`sudo loginctl enable-linger $USER` once (`install` prints this when lingering is off). Without a systemd user session
(for example over `sudo su`, or in a container without systemd) `install` fails with an explanation.

**Linux: `--system`.** `sudo smind service install --system` writes `/etc/systemd/system/smind.service` with
`User=<you>`, taken from `SUDO_USER`; the daemon never runs as root, and the command refuses plain root without
`SUDO_USER`. sudo replaces your `PATH`, so pass it explicitly: `sudo smind service install --system --path "$PATH"`.
With the installer: `curl -fsSL https://spacingmind.com/install.sh | sudo sh -s -- --service --system` (this captures
sudo's `PATH`, so agent CLIs in your home do not resolve; install prints a warning, and the fix is the `--path "$PATH"`
form above run from your own shell).

**macOS.** A LaunchAgent in `~/Library/LaunchAgents/com.spacingmind.smind.plist`. It loads into your `gui/<uid>` domain,
which exists only while you are logged in to the Mac's graphical session, so it starts at login, not at boot, and cannot
be loaded over plain ssh on a Mac nobody is logged in to (the plist is still written and loads at the next login).
`smind stop` (or `smind service stop`) leaves it stopped; a crash restarts it.

**Windows.** A per-user **Scheduled Task** named `smind-<your SID>` (not a Windows Service, which would run as another identity
without your `PATH`, agent logins and profile). It has a logon trigger for your account only, runs as you in your
interactive session at the least privilege level, and registering it needs **no administrator rights** (a policy that
forbids scheduled tasks can still refuse it; the error then names the refused `schtasks /Create` step). **It starts at
logon, not at boot**: a headless Windows machine needs auto-logon, which smind does not set up. The daemon is launched
through `conhost.exe --headless`, so it has no console window. It runs only in your own interactive Windows session
(signed in at the console or over RDP): `smind service start` over plain ssh will time out. `status` exits 0 / 3 / 4 as on the other
systems. The name carries your SID, so each user of a machine has their own task. An older machine-wide task `\smind` owned by you is moved to the per-user name by the next `smind service install` (another user's `\smind` is left alone). If Task Scheduler cannot be asked (a transient error), `status` says `could not query Task Scheduler` instead of `not installed`; `smind service status` retries once.

- `smind service stop` asks the daemon to shut down (`POST /api/shutdown`) and only if it does not exit in time ends the
  task and kills what is left; it never stops a daemon this task did not start (the desktop app's, or one you started by
  hand), and `install`/`start` do not start next to one.
- A crashed daemon is restarted up to 3 times, 1 minute apart, by `smind service run` (what the task executes): the
  Task Scheduler's own restart-on-failure cannot see the daemon's exit code through the headless console. After that, or
  after a clean stop, it stays down until the next logon or `smind service start`.
- The task runs with your logon environment (`PATH` included), so there is no `--path`; `--home` becomes `service run
  --home`. Environment changes need a new logon to reach it.
- `install.ps1` takes `-Service` (`& ([scriptblock]::Create((irm <url>))) -Service`) or `SMIND_SERVICE=1` for the
  `irm | iex` form.

**Logs.** The daemon writes `$SMIND_HOME/daemon.log` itself (mode `0600`, rotated at 10 MiB, 5 files kept; change with
`log.maxSizeMB` and `log.maxFiles` in `config.yaml`) in addition to stderr, so `smind service logs` works the same on
every OS. On Linux `journalctl --user -u smind` (`journalctl -u smind` for a `--system` unit) shows the same lines plus
anything the daemon prints outside its logger. A Go panic or fatal error trace is also written to `daemon.log` itself, on every OS and supervisor, so `smind service logs` shows it.

**Desktop app.** The app never starts a second daemon on a port that is already served, and `smind service install`
does not start while the app's own daemon holds the port. The app sees a service-managed daemon as one it does not
manage (`/healthz` reports `"managedBy": "systemd"` or `"launchd"`) and leaves it alone: Settings → Daemon says "Managed
by systemd" and offers no **take over management** (the app also refuses it), because the service manager would just
restart the daemon. Stop the service first (`smind service uninstall`) if you want the app to manage the daemon. On Windows
the value is `"managedBy": "taskscheduler"` (Settings → Daemon says "Managed by Task Scheduler"), and `smind service install`
refuses to run from the app's own `%LOCALAPPDATA%\smind\bin` directory (the app replaces that daemon when it updates).

**Older than the app.** When the connected daemon is older than the desktop app, the app shows a dismissible notice with
the command that updates it and a **Copy** button: `smind update` (daemons from v0.12.0 on) or the installer one-liners
for an older one. A daemon reached through a relay is asked for its version over the connection itself (the `daemon.info`
method); a relay daemon too old to answer is shown as "version unknown" with the installer commands. The notice is
dismissed per connection and per daemon/app version pair. A daemon the app manages keeps its **Update & restart** button.

## Path A: direct connection

Let the daemon listen on your LAN or VPN address, protected by a password. Without a password it never listens beyond
loopback.

```sh
smind set-password                       # prompts twice; stores a bcrypt hash in ~/.smind/config.yaml (mode 0600)
smind config set server.listen 0.0.0.0:4648
smind config set server.hostnames nas.lan   # every name clients will type that is not an IP or localhost
smind stop && smind serve                # restart to apply
```

Then open `http://<address>:4648` in a browser on another machine and sign in with the password. A sign-in lasts 24 hours
(the session token is kept in the tab's `sessionStorage` and dropped when it expires; the password itself is never stored)
and ends when the daemon restarts.

Settings (`smind config get|set|unset`, comments in `config.yaml` are kept, a restart applies them):

| Key | Meaning |
|---|---|
| `server.port` | Port of the default `127.0.0.1` address (4648) |
| `server.listen` | `host:port` to bind. A non-loopback host needs a password; the daemon **refuses to start** otherwise |
| `server.hostnames` | Extra `Host` names to accept. A leading dot matches the domain and its subdomains (`.example.com`) |
| `server.passwordHash` | Written by `smind set-password` (shown as `<set>`, never printed); `smind config unset server.passwordHash` removes it |
| `providers.<id>.defaultModel` | The model a run of that provider uses when none is given (`<id>`: `claude-native`, `codex-native`, `glm`, `kimi`), e.g. `smind config set providers.claude-native.defaultModel sonnet`. Unset keeps the provider's own default, except claude-native: a run that resolves no model (none given, none on the profile, no default here) is refused with `no_model` instead of launching the Claude CLI unpinned (its own default may be Opus). A run's own model and a profile's model win over it; `--model default` on claude-native, from a human only, keeps the CLI's own |

For containers, `SMIND_LISTEN`, `SMIND_HOSTNAMES` (comma separated) and `SMIND_PASSWORD` (at least 12 characters, hashed in
memory, never written to disk) override the file. `smind serve` itself takes no flags.

### What is protected, and what is not

**The password is a remote-code-execution credential.** Whoever signs in gets a terminal and every agent, with your
user's rights. Choose a long, unique one (the minimum is 12 characters; there is no default).

- **Transport is plain HTTP.** On a LAN anyone on the network path can read the password and the session. Use it only on a
  network you trust. Otherwise put the daemon behind Tailscale or WireGuard, an SSH tunnel, or a reverse proxy that
  terminates TLS. smind has no built-in TLS.
- **Login:** `POST /api/login` returns a random 256-bit session token that expires after 24 hours. The standing daemon
  token is never sent to a remote client. Wrong passwords back off per client address (5 free attempts, then 2 s, 4 s,
  8 s... up to 15 minutes) and there is a global limit against address rotation.
- **`/api/token`** (the daemon's standing token) is **never served once a password is set**, not even to the machine's
  own browser: the local web UI signs in with the password like every other client. The CLI (`smind ...`) and the desktop
  app's notification client keep working because they read the token from `~/.smind/token` (readable only by your OS
  user) and send it in an `Authorization` header; the `?token=` URL form is refused when a password is set. (With no
  password the daemon is loopback-only and `/api/token` is served to a true loopback caller, as before.)
- **Host allowlist** (DNS-rebinding defence): every request must carry `localhost`, `*.localhost`, an IP address or a name
  from `server.hostnames`, or it gets `403 Host not allowed`. The WebSocket also requires a same-origin `Origin`.
  Session tokens travel in the `Sec-WebSocket-Protocol` header or `Authorization`, never in the URL.
- **Reverse proxy on the same host:** with a password set the daemon token cannot be fetched over HTTP at all, so a proxy
  in front of the daemon (even a stock nginx `proxy_pass http://127.0.0.1:4648;`, which rewrites `Host` and adds no
  header) exposes only the login. Put the name clients type in `server.hostnames`, pass `Host` through
  (`proxy_set_header Host $host;`) and proxy WebSockets (`Upgrade`/`Connection`). Check from another machine that
  `curl -i https://smind.example.com/api/token` answers `403`.
  **Without a password** a proxy on the daemon's own host is not safe (the daemon cannot tell a proxied request from a
  local one, and would serve the token): set a password first.
- **Listening on all interfaces:** `0.0.0.0:4648` is IPv4 only. For IPv6 as well use `[::]:4648` (or `:4648`); the daemon
  logs the addresses it actually bound. When `server.listen` is not `127.0.0.1` the daemon also binds `127.0.0.1` so the CLI
  and the desktop app keep working.
- **Lockout:** repeated wrong passwords throttle the client (IPv6 by /64). Behind a same-host proxy all clients share the
  proxy's address, so only a global limit (40 wrong passwords a minute, then one minute of no logins) applies; anyone who
  can reach the port can therefore hold logins locked. Restrict who can reach it (firewall, VPN).
- **Residual risk:** one shared password, no per-device revocation (restart the daemon to end every session), no MFA, no
  audit log. A local process of the same user can still read `~/.smind/token`.

### Alternatives with no open port

- An **SSH tunnel** keeps the daemon on loopback and needs no password:

  ```sh
  ssh -N -L 4648:127.0.0.1:4648 you@homelab    # then open http://localhost:4648 on this machine
  ```

- A **relay** (Path B) works through NAT.

> The desktop app's "Connect to a daemon" form does not take a password yet; use the browser, a tunnel or a relay from
> the app until first-run onboarding (a later change) adds it.

## Path B: relay

> **Hosted relay: pending.** A public relay operated for smind users is planned but does not exist yet, so for now you run
> your own relay. `smind relay connect` always takes an address today.

### 1. Run a relay

On a machine both your daemon and your clients can reach (a small VPS, or the homelab itself if it has a public or VPN
address):

```sh
smind relay            # listens on :7400 (native gRPC) and :7401 (grpc-web)
```

Open **`7400/tcp` and `7401/tcp`** on that machine's firewall. The two ports must stay **adjacent** (native, then
native + 1 for grpc-web): `smind relay offer` and the desktop app derive the grpc-web address from the native one.

The relay generates a self-signed TLS certificate on first start and prints its fingerprint:

```
smind relay listening on [::]:7400 (fingerprint 3f9a…), grpc-web on [::]:7401
```

Keep that fingerprint; you will check it in step 3. The relay data (certificate, relay host secret hashes) lives under
`$SMIND_HOME/relay` (default `~/.smind/relay`).

**A real certificate instead.** If the relay has a DNS name and you have a certificate for it (Let's Encrypt, or your own
CA), serve that instead:

```sh
smind relay --tls-cert /etc/letsencrypt/live/relay.example.com/fullchain.pem \
            --tls-key  /etc/letsencrypt/live/relay.example.com/privkey.pem
```

Both ports use it. Clients then verify it like a browser does (trusted CA, matching name, not expired), so there is
no fingerprint to compare or pin, and a phone can connect. The files are re-read when they change, so a renewal needs no
restart. Reach the relay by the name in the certificate (`relay.example.com:7400`), not by IP address.

### 2. Create a relay host

On the relay machine:

```sh
smind relay host new homelab
```

This prints a secret **once** (the relay stores only its hash). Copy it. Relay host changes (`new`, `rm`, `rotate`)
are picked up when the relay restarts. `smind relay host ls` lists the hosts.

### 3. Connect the daemon to the relay

On the machine that runs `smind serve`:

```sh
smind relay connect relay.example.com:7400 homelab <secret>
```

`connect` first checks the relay's certificate the way a browser would. If it verifies (a public CA, the right name, not
expired) it stores that decision and there is nothing to pin. Otherwise, which is the case for the default self-signed
certificate, it **shows the certificate's fingerprint and asks you to confirm it**:

```
The relay at relay.example.com:7400 does not present a certificate your system trusts:
  x509: certificate signed by unknown authority

Its certificate fingerprint is

  3f9a…

Compare it with the fingerprint "smind relay" printed when it started. …
Pin it and connect? [y/N]
```

**Compare it with the one the relay printed in step 1** — if they differ, someone is in the middle: answer no. It never
pins silently, and without a confirmation (for example with no terminal) it saves nothing. To skip the prompt, pass the
fingerprint you copied from the relay: `smind relay connect --fingerprint 3f9a… relay.example.com:7400 homelab <secret>`
pins it only if the relay presents exactly that certificate. `smind serve` then dials the relay automatically on every start
(restart it if it was already running).

### 4. Print a pairing link

```sh
smind relay offer
```

The link goes to stdout. On a terminal (or with `--qr`) a QR code is also drawn on stderr, so `$(smind relay offer)`
still captures only the link. `--png offer.png` writes the QR code to a file (mode `0600`; `--force` overwrites an
existing file). An offer carries the relay host secret and, for a self-signed relay, the pinned fingerprint (a relay with a
CA certificate has none), so it is about 430 bytes; the QR
falls back to error-correction level L to fit it, and an offer too long for any QR code is still printed as a link
with a note.

> **Treat the link like a password.** Anyone who has it can reach this daemon through the relay. Traffic is
> end-to-end encrypted; the relay cannot read your code or messages. To revoke every link at once, run
> `smind relay host rotate homelab` on the relay, then `smind relay connect` again on the daemon with the new secret.

### 5. Pair the desktop app

In the desktop app: Settings → Connections → **Add via relay link**, paste the link, check the host and fingerprint it
shows, and press Add. *(Add via relay link is being built; until it ships, the desktop app cannot add a relay
connection.)*

### 6. Pair a mobile device

Open the mobile app's pairing screen and paste the link (or scan the QR code in builds that support scanning), then
Connect. On first use the app creates its device key (kept in the iOS Keychain or the Android Keystore through the
platform secure store). If the pairing window is not open for it, the app shows **Waiting for approval** with the exact
`smind relay devices approve <id>` command to run on the daemon's computer (step 7) and connects by itself once you
approve. A device you revoke shows **This device was revoked**; *Reset device identity* gives it a new key, which the
daemon treats as a new device that needs approval again. After a dropped connection the app reconnects on its own and
resumes its session.

If the daemon has a password (`smind set-password`), the app asks for it ("Daemon password required") the first time, keeps it
only in memory until you close the app (never on disk, in the keychain or in logs), and asks again only if the daemon refuses
it ("Incorrect password"). After too many wrong attempts the daemon makes the app wait; the app then tries the password you
entered once more by itself.
The app and the daemon must speak the same device-identity version (hello v2): if the daemon sends no device challenge the
app says "Daemon outdated or on a different version" and does not connect — update the daemon with `smind update`.

A phone does not pin the relay's certificate (it has no way to confirm a fingerprint): the relay needs a certificate
that the phone already trusts, for example one from a public CA (`smind relay --tls-cert/--tls-key`). A self-signed relay
certificate is refused.

**Desktop app and CA relays.** Today the desktop app's "Add via relay link" accepts only an offer with a fingerprint, so
it cannot yet connect to a relay serving a CA certificate (an offer without one is rejected as "missing relay certificate
fingerprint"). Until that lands, use the daemon on the relay's own network, or pair desktop clients to a self-signed
relay.

### 7. Approve devices

Every client proves a **device identity** (a key it creates once) when it connects, and the daemon keeps a registry of
devices in `$SMIND_HOME/relay-devices.json`. Printing a link with `smind relay offer` opens a **pairing window**
(10 minutes or one new device; `--window <duration>` and `--max-devices <n>` change that): the first new device that
connects while it is open is approved automatically. Any other new device is recorded as *pending* and refused with
"awaiting approval" until you approve it:

```
smind relay devices ls [--pending]     # id, label, state, connected, first/last seen
smind relay devices approve <id>
smind relay devices revoke <id>        # ends its session at once and tells the relay to refuse it
smind relay devices rename <id> <label>
```

Ids accept a unique prefix. With the daemon running these talk to it; with no daemon they edit the registry file and
the change applies when the daemon next starts.

> **Requires updated clients.** Relay pairing needs apps that send a device identity. **The desktop app does** (a device key
> per relay connection, created when you add the link; Settings → Connections shows "Waiting for approval on the daemon" with
> the exact `smind relay devices approve <id>` to run, or "revoked"; it needs a daemon that sends the device challenge — one built
> after the hello-v2 fix, newer than v0.12.0 — and says "different versions" otherwise). **The mobile app does too** (a device key in the platform secure store; the pairing screen shows "Waiting for approval" with the same command, or "This device was revoked", or "Daemon outdated or on a different version" when the daemon sends no device challenge). A desktop or mobile build from before these clients **cannot connect** through the relay — the daemon refuses a client without a device identity ("device identity required"). As a stop-gap you can set `relay.legacyDevices: true` in
> `$SMIND_HOME/config.yaml` to let such a client in on one fixed route. **Do not leave it on:** a legacy client is served without
> approval and cannot be revoked, so anyone who has your pairing link (the relay host secret plus your daemon's public key) — or
> the relay operator — can connect, and `devices revoke`, the pending state and the pairing window do not apply to it. The daemon
> logs a warning at start and for every legacy connection.

### 8. Require the daemon password from relay devices

If the daemon has a password (`smind set-password`, see Path A), **every relay device must present it**, on every
connection, approved devices included. It travels inside the end-to-end encrypted channel (the relay never sees it), and it
is checked **before** anything about the device is recorded or revealed: a device without the right password is never listed
as pending, and learns nothing about whether it is approved or revoked. The desktop app asks for it in Settings → Connections
("This daemon needs its password"), keeps it in memory only (never on disk, never in a log, never handed back to the UI), and
asks again after a restart. Wrong guesses are rate limited per device (5 free tries, then waits that double up to 15 minutes),
and with more than 40 wrong attempts a minute across all devices the daemon refuses every password check for a minute.

Without a password a relay device is admitted by your approval alone, and the daemon logs one warning line when the relay
bridge starts. With a password, a client that predates device identity (`relay.legacyDevices: true`) is refused regardless: the
legacy route cannot carry a password. The mobile app takes the same password (its own prompt follows).

### 9. Push notifications (Android)

The phone gets a notification when a run needs a permission approval, reaches the turn cap and needs Continue, or finishes or
fails (finished and failed are sent for top-level tasks only), even with the app in the background or swiped away. Tapping it
opens that task. Android only for now: on iOS the app has no remote notifications yet (they need Apple's paid Developer
Program). The app must be a development build or an installed build; Expo Go cannot receive push.

Turn it on in the mobile app: Connection → Notifications → Turn on. Nothing is registered, and no permission is asked, until you
do. It works over the relay with a paired and approved device. `smind relay devices ls` has a PUSH column (`yes` or `no`; `-`
when the daemon is not running). To test it without running an agent:

```sh
smind push test                 # every phone registered for push
smind push test <device id prefix>   # one phone; the prefix is at least 4 characters
```

The exit code is 0 only if every phone was sent. "No phone is registered" means notifications are not turned on in the app yet.

To turn push off for the whole daemon, set it in `config.yaml` or the environment (the app's Notifications row then shows "Off
on this daemon" and nothing is sent). The default is on, which does nothing until a phone registers:

```yaml
push:
  enabled: false
```

```sh
SMIND_PUSH=off    # also 0, false, no, n, f, disabled
```

- **What leaves your daemon.** One HTTPS request to `https://exp.host` (Expo's push service) per notification. It carries an
  opaque, padded, encrypted blob and the phone's push token, and the daemon's IP address is the connection source. Task titles,
  prompts, file names and permission text are encrypted for the phone with a per-device key that only your daemon and that phone
  hold (exchanged over the end-to-end encrypted relay channel); Expo, Google and Apple cannot read them. If the phone cannot
  decrypt a push it shows a generic "Open smind to see what happened". The relay never sees the token or the key.
- **Credentials.** The daemon needs none by default. The Firebase (FCM) credential that makes delivery work belongs to whoever
  built the app (it lives in their Expo/EAS project), not to your daemon. Optional: if the app's Expo project turns on
  "enhanced push security", set `push.expoAccessToken` in `config.yaml` or the environment variable `SMIND_EXPO_ACCESS_TOKEN`
  (the variable wins). It is a secret and is never logged.
- **Revoking and re-pairing.** `smind relay devices revoke <id>` stops pushes to that device. Re-pairing a phone gives it a new
  device identity, so turn notifications on again afterwards.
- **Where it is stored.** Registrations live in `push-registrations.json` in the daemon's home directory (mode 0600). It holds
  each phone's token and key: treat it like the relay device registry. Losing it only means phones must turn notifications on
  again.
- **Limits.** Delivery is best effort and goes through Expo and Google, so a notification can be late or lost. A force-stopped
  app or an aggressive battery saver stops Android from receiving pushes until the app is opened. A notification already in
  flight is not retracted when a permission is answered elsewhere, so a stale one can arrive after you approved on another
  device; tapping it opens the task, which shows the true state. The daemon needs outbound HTTPS to `exp.host`.

### Limits today

- A relay host serves up to 16 devices at once (`smind relay --max-devices-per-host` on the relay,
  `relay.maxDevices` on the daemon); each device gets its own session and sees all of the daemon's workspaces, spaces
  and tasks.
- The pairing link has no expiry and is not single-use; `smind relay host rotate` is the way to invalidate every link at
  once. A leaked link alone no longer admits new devices once the pairing window has closed, because the daemon
  refuses devices it has not approved — but it still reaches the relay host.
- Requests in flight at the moment a connection drops hard can be lost; clients time out and open a fresh session.
- A self-signed relay is trusted by confirming its fingerprint in `relay connect` (step 3): compare it with the one the
  relay printed, and pass `--fingerprint` to make that check explicit. A CA certificate needs no such step.
- Push notifications are Android only; iOS needs the Apple Developer Program (step 9).

## Docker

`ghcr.io/spacingmind/smind` (`linux/amd64` and `linux/arm64`; tags `<version>`, `<major>.<minor>` and `latest`) runs the daemon
with the web UI. It is built from the release's own binaries (the ones `checksums.txt` lists), never compiled in Docker.

```sh
docker run -d --name smind --restart unless-stopped \
  -p 4648:4648 \
  -e SMIND_PASSWORD="$(openssl rand -base64 24)" \
  -v smind-data:/data -v smind-home:/home/smind -v "$PWD/workspaces:/workspaces" \
  ghcr.io/spacingmind/smind
```

Read the password back with `docker inspect smind` or put it in an env file first (`docker/docker-compose.example.yml`
does this: it reads `SMIND_PASSWORD` from `.env` and refuses to start without it). Then open `http://<host>:4648` and sign in.

- **A password is mandatory.** The image listens on `0.0.0.0:4648`, so it refuses to start without `SMIND_PASSWORD` (at least 12
  characters; the entrypoint says why, and the daemon enforces the same rule). A password already stored in `/data/config.yaml`
  by `docker exec --user smind smind smind set-password` also counts. To keep the daemon off the network set
  `-e SMIND_LISTEN=127.0.0.1:4648` (no port publishing) and reach it through a relay or a tunnel.
  Everything in "What is protected" above applies: plain HTTP, the password is a remote-code-execution credential, and
  `SMIND_HOSTNAMES` (comma separated) adds the names clients type when they are not IPs.
- **Volumes.** `/data` is `SMIND_HOME`: database, token, accounts, relay config, `config.yaml`. `/home/smind` holds agent
  CLI logins (`.claude`, `.codex`). `/workspaces` is where your repositories go: register them with their container path
  (`/workspaces/<repo>`). The entrypoint starts as root only to hand **root-owned** files in `/data` and `/home/smind` to uid 1000 (a fresh
  named volume, a bind mount created with `sudo`, or a file `docker exec` wrote as root), then runs the daemon as the `smind`
  user (uid/gid 1000, `tini` as PID 1). Files owned by any other uid are never touched (a bind mount of your own directory
  keeps its owner; the entrypoint warns, and you run the container with `--user <that uid>`), and `/workspaces` is only adopted
  at its top level when root owns it. **Use `docker exec --user smind`** for `smind ...` commands: as root they would write
  root-owned files. `--user <uid>:<gid>` skips the entrypoint's ownership step and needs mounts you already made writable
  (`/data`, and a writable `/home/smind` for npm and agent caches).
- **Port and listen address are set with the environment**, not `smind config`: the image sets `SMIND_LISTEN=0.0.0.0:4648`,
  and the environment beats `config.yaml`, so `smind config set server.port|listen` inside the container has no effect (or
  makes the daemon refuse to start because the two disagree). Change the published port with `-p`, or set `SMIND_LISTEN`.
  To restart the daemon use `docker restart smind` (`smind stop` ends the container).
- **Health and supervision.** The image has a healthcheck on `/healthz` (healthy within about 15 s) and sets
  `SMIND_SUPERVISOR=docker`, so `/healthz` reports `managedBy: docker`.
- **State survives** removing and recreating the container on the same volumes (database, token, accounts, relay config).
  Back up `/data` as described in "Backups and the database file" below: stop the container first, or use `VACUUM INTO`,
  and keep the volume on a local disk (the database uses WAL, which a network-backed volume driver does not support).
  `docker exec --user smind smind smind relay connect <relay:port> <host-id> <secret>` stores its config in `/data`, and the
  restarted container dials the relay again; see Path B.
- **Updating** means pulling a new tag. `smind update` inside the image refuses ("update by pulling a new image").
- **Agent CLIs are not in the image.** `claude` and `codex` are third-party programs with their own licences, versions and
  credentials. `docker/Dockerfile.agents.example` is a child image that adds both (`npm install --global
  @anthropic-ai/claude-code @openai/codex`); it was built and `claude --version` run in CI. Sign in once with
  `docker exec -it --user smind smind claude` (the login lands in the `/home/smind` volume). The GLM agent needs nothing
  extra: smind runs it with `npx` (Node 22 is in the image) and downloads it on first use.
- **Adding accounts.** `smind account add <provider> <label>` reads the credential from stdin and works in a container:
  `printf %s "$KEY" | docker exec -i --user smind smind smind account add anthropic homelab`. Browser login
  (`smind account login`) prints the authorize URL, but the image has no browser to open it and the OAuth callback is
  `http://localhost:<port>/callback` *inside* the container, so it only completes if that port is reachable from your
  browser (not tested); use `account add` or the agent CLI's own login.
- **Hardening.** The compose example drops every capability except `CHOWN`, `SETUID` and `SETGID` (what the entrypoint
  needs) and sets `no-new-privileges`: the daemon and its agents then run with no capabilities at all. For `docker run` add
  `--cap-drop ALL --cap-add CHOWN --cap-add SETUID --cap-add SETGID --security-opt no-new-privileges:true`. The password in
  `SMIND_PASSWORD` is visible in `docker inspect`, and (as in any process's initial environment) to other processes of the
  same user through `/proc/<pid>/environ` even though the daemon removes it from what its children inherit: agents run as that
  user and already hold the daemon token, so use a password you use nowhere else. To keep it out of the environment entirely, run
  `docker exec --user smind <c> smind set-password` once (it stores a bcrypt hash in the `/data` volume) and start without
  `SMIND_PASSWORD`.
- **Publishing (maintainers).** A package pushed to GHCR for the first time is private: after the first release, make
  `ghcr.io/spacingmind/smind` public in the repository's package settings, or the documented `docker pull` fails for everyone else.
- **The container is the isolation boundary:** agents read and write whatever you mount, including `/workspaces` and the
  home volume.

## Backups and the database file

The daemon keeps its state in `$SMIND_HOME/smind.db` (ADR-0034). It runs SQLite in WAL mode, so while the daemon is running
the database is three files: `smind.db`, `smind.db-wal` and `smind.db-shm`. Recent commits live in the `-wal` until a
checkpoint folds them into `smind.db`; a clean stop (`smind stop`, `docker stop`, a service stop) checkpoints and leaves
`smind.db` alone complete.

**Do not copy `smind.db` by itself while the daemon runs** (a file sync tool, `cp`, `rsync`, a volume snapshot of just that
file): the copy misses committed rows and can be torn. Use one of these:

- **A live backup** (daemon running, nothing to stop). Needs the `sqlite3` command-line tool, 3.27 or newer (the Docker
  image does not include it: use a cold backup there, or run `sqlite3` on the host against the volume's path):

  ```sh
  sqlite3 "$SMIND_HOME/smind.db" "VACUUM INTO '/backups/smind-$(date +%F).db'"
  # or:  sqlite3 "$SMIND_HOME/smind.db" ".backup '/backups/smind-$(date +%F).db'"
  ```

  The target file must not exist. The result is a single, consistent `.db` file with everything committed so far.
- **A cold backup.** Stop the daemon, then copy the whole `$SMIND_HOME` directory (or at least every `smind.db*` file).
  Docker: `docker stop smind`, then
  `docker run --rm -v smind-data:/data -v "$PWD":/backup alpine tar czf /backup/smind-data.tgz -C /data .`, then
  `docker start smind`. A volume snapshot taken while the container runs is also safe if it is atomic for the whole volume,
  because the `-wal` is in it; the daemon replays it on the next start, as after a crash.

**Restoring.** Stop the daemon, put the backup at `$SMIND_HOME/smind.db`, and **delete any `smind.db-wal` and `smind.db-shm`**
left from the old database: a stale `-wal` does not belong to the restored file. Never delete those two files from a
database you want to keep (for example after a crash): the `-wal` can hold committed rows the main file does not have yet.

**Local disks only.** WAL needs a local filesystem. If `$SMIND_HOME` is on a network mount (NFS, SMB/CIFS, a NAS volume
driver, WSL's `/mnt/c`), the daemon notices, logs `is on a network filesystem; using the rollback journal instead of WAL`
and runs the slower, older journal mode there. Keep `$SMIND_HOME` (or the Docker data volume) on the host's own disk. The
detector does not recognise FUSE-based mounts (sshfs, rclone, mergerfs, Unraid shares, Docker Desktop file sharing): if the
daemon fails to open its database there, start it with `SMIND_SQLITE_JOURNAL=delete`. Never bind-mount the single file
`smind.db` into a container: the `-wal` and `-shm` files would land outside the mount; mount the directory.

**After a power loss** the last few commits may be missing; the database itself stays consistent. To make every commit
survive a power loss at the cost of write speed, start the daemon with `SMIND_SQLITE_SYNCHRONOUS=full`.

Optionally also keep `push-registrations.json` from `$SMIND_HOME` (step 9 of Path B): it is a small file, and without it phones
only have to turn notifications on again.

## Which to use

- Same LAN or VPN, nothing to host: a direct connection (password), or an SSH tunnel.
- Phone, laptop on another network, or a daemon behind NAT: a relay.
- A hosted relay (no server to run) is planned; this page will say when it is available.
