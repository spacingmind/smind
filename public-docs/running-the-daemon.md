---
title: Run the daemon
description: Keep the daemon running as a service, reach it from other devices, run it in Docker, and update it.
summary: "This page explains how to keep the sMind by Spacing Mind daemon running as an operating-system service, reach it from other devices, run it in Docker and update it. It is for people who host smind on a machine or server."
order: 14
---

# Run the daemon

`smind serve` runs the daemon in the foreground and stops with your terminal.
This page covers keeping it running, reaching it from another device,
Docker, and updating.

## Run it as a service

smind has no command that starts the daemon in the background. Your operating system's service manager
supervises the daemon instead.

```sh
smind service install
smind service status
```

`install` writes the service, enables it and starts the daemon. Running it
again is safe.

| System | What `install` creates |
| --- | --- |
| Linux | a systemd user unit (`--system` for a system unit that still runs as you) |
| macOS | a LaunchAgent, loaded when you log in |
| Windows | a per-user scheduled task that starts at logon, not at boot |

You can also install it with the installer script:

```sh
curl -fsSL https://spacingmind.com/install.sh | sh -s -- --service
```

On a headless Linux machine, a user unit stops when you log out unless lingering
is on. Run `sudo loginctl enable-linger $USER` once.

The port and other settings come from `config.yaml`, not from flags. See
[Configuration](configuration.md). Other commands:

```sh
smind service start
smind service stop
smind service restart
smind service logs -f
smind service uninstall
```

`smind service status` exits 0 when the daemon runs, 3 when it is stopped and 4
when no service is installed. `uninstall` never touches your data.

## Reach the daemon from another device

By default the daemon listens on `127.0.0.1:4648` only. There are two ways in
from another machine.

**Direct connection.** Use this when the devices share a LAN or a VPN such as
Tailscale or WireGuard. Set a password, choose the address to listen on, and
restart:

```sh
smind set-password
smind config set server.listen 0.0.0.0:4648
smind config set server.hostnames nas.lan
smind stop && smind serve
```

Then open `http://<address>:4648` in a browser and sign in. The daemon refuses
to start on a non-loopback address without a password. Add every name clients
type (other than an IP address or `localhost`) to `server.hostnames`.

The password gives whoever has it a terminal and every agent, with your
user's rights. Pick a long, unique one. The connection is plain HTTP, so use it
only on a network you trust, or put the daemon behind a VPN, an SSH tunnel or a
reverse proxy that adds TLS.

An SSH tunnel needs no password and no open port:

```sh
ssh -N -L 4648:127.0.0.1:4648 you@homelab
```

Then open `http://localhost:4648` on your own machine.

**Relay.** Use this for a phone, or when the daemon is behind NAT. The daemon
dials out to a relay, and the relay only forwards encrypted data. See
[Mobile and relay pairing](mobile-and-relay.md).

## Run it in Docker

The image `ghcr.io/spacingmind/smind` (amd64 and arm64) runs the daemon with
the web UI. It listens on all interfaces, so it needs a password of at least 12
characters and refuses to start without one:

```sh
docker run -d --name smind --restart unless-stopped \
  -p 4648:4648 \
  -e SMIND_PASSWORD="$(openssl rand -base64 24)" \
  -v smind-data:/data -v smind-home:/home/smind -v "$PWD/workspaces:/workspaces" \
  ghcr.io/spacingmind/smind
```

- `/data` is the smind home (database, token, accounts, `config.yaml`).
- `/home/smind` holds the agent CLIs' logins.
- `/workspaces` is where your repositories go. Register them with their
  container path, for example `/workspaces/my-repo`.
- Run `smind` commands with `docker exec --user smind smind smind ...`.
- Restart with `docker restart smind`. `smind stop` ends the container.
- Update by pulling a new image tag. `smind update` refuses inside a container.
- The image does not include `claude` or `codex`. The repository has a sample
  child image, `docker/Dockerfile.agents.example`, that adds them.

Set the port and listen address with `-p` or `SMIND_LISTEN`, not with
`smind config`. The environment wins over the file.

## Update

Rerun the install script to update the CLI, or run:

```sh
smind update --check
smind update
```

`--check` only reports. `--version <v>` picks a release, and `--dry-run` shows
what would change. `smind update` verifies the download against a signed
checksum list, and it refuses while runs are active unless you pass `--force`.

The desktop apps check for updates on their own and ask before downloading.

## Install a release by hand

Each GitHub release has `smind` binaries for Linux and macOS (amd64, arm64) as
`.tar.gz` files, `smind_<version>_windows_<amd64|arm64>.zip` for Windows
(with `smind.exe` at the top), and a `checksums.txt`:

```sh
version=0.8.0   # match the release you want
os=linux        # or darwin
arch=amd64      # or arm64

curl -fsSLO "https://github.com/spacingmind/smind/releases/download/v${version}/smind_${version}_${os}_${arch}.tar.gz"
curl -fsSLO "https://github.com/spacingmind/smind/releases/download/v${version}/checksums.txt"
sha256sum --ignore-missing -c checksums.txt
tar -xzf "smind_${version}_${os}_${arch}.tar.gz"
./smind --version
```

On Windows, unzip anywhere and run `smind.exe serve`.

## Back up

The daemon keeps its state in `smind.db` in the smind home. While it runs, the
database also has `smind.db-wal` and `smind.db-shm` files, so do not copy
`smind.db` alone. For a live backup, use the `sqlite3` tool:

```sh
sqlite3 "$SMIND_HOME/smind.db" "VACUUM INTO '/backups/smind.db'"
```

The target file must not exist. Or stop the daemon and copy the whole home
directory. Keep the home directory on a local disk.

## Further reading

- [Operator notes: threat model, relay hosting, Docker details](../docs/homelab.md)
