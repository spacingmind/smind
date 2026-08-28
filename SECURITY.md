# Security Policy

`smind` is a small, early-stage project without a dedicated security team.
We still take vulnerability reports seriously and will respond as
promptly as we can.

## Reporting a vulnerability

**Please do not open a public GitHub issue for security-sensitive
findings.** Instead, use GitHub's private vulnerability reporting:

<https://github.com/spacingmind/smind/security/advisories/new>

This opens a private advisory visible only to the maintainers, where you
can describe the issue, its impact, and any reproduction steps. We'll
follow up there.

## Scope

This applies to vulnerabilities in `smind` itself — the daemon (auth,
proxy request handling, the `/ws` API, path sandboxing in the file
explorer, PTY/terminal session handling), the CLI, and the embedded web
UI. Issues in a third-party agent CLI smind drives (Claude Code, a GLM
ACP agent, etc.), or in an upstream provider's own service, should be
reported to that project/provider directly.

smind routes requests across multiple accounts on your behalf and stores
OAuth credentials locally (`~/.spacingmind/`). A report involving
credential handling, token refresh, or account isolation is very much in
scope.

## Supported versions

This project is pre-`v1`; there is no long-term-support branch. Security
fixes land on the latest release.
