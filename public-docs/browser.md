---
title: The agent browser
description: Let an agent open and read pages on your own machine, with a per-workspace switch, an allowlist and a permission card for every step.
summary: "This page explains the agent browser in sMind by Spacing Mind: what an agent can do with it, how you turn it on for one workspace, which addresses it can and cannot reach, the permission cards you answer, which agents can use it, and what it does not protect against. It is for people who want an agent to look at a page their project serves."
order: 26
---

# The agent browser

An agent can open a page on your machine (your dev server, say), read what it
shows and look at a screenshot. It is **off for every workspace until you turn
it on**, and only a person can: the switch is not available to agents.

## Turn it on for a workspace

```sh
smind browser enable --workspace 7
smind browser status --workspace 7
```

`smind browser disable --workspace 7` turns it off again and ends the workspace's
browser at once. `smind browser clear --workspace 7` ends its session and removes
its data. These commands are for people only; an agent running a task cannot use
them. A local program that talks to the daemon without saying who it is counts as
you, so keep the daemon's token private.

A system Chrome, Chromium or Edge must be installed (smind never downloads one).
The browser runs headless with a fresh profile that is removed when the session
ends.

## Where it can go

- Your own machine (`localhost`) on any port, except the addresses smind itself
  listens on: the daemon, its relay and web listeners, a login callback while a
  login is open, and the address of any daemon you linked.
- Hosts you add by name or address: `smind browser allow add intranet.example
  --workspace 7` (one exact host, no wildcards, no ports; `smind browser allow rm`
  removes it). A host name you add must resolve to a public address; to allow a
  machine on your own network, add its IP address instead.
- Never: link-local and cloud-metadata addresses, other schemes than `http` and
  `https`, and anything that carries a username or password in the address.

Everything a page shows is treated as untrusted text and given to the agent
inside a labelled envelope: an instruction written on a page is not followed.

## The permission cards

smind asks you, in the app and on your phone like any other permission request,
before the agent uses the browser. The card says it is smind's own check, not
the agent tool's.

- **Reading** (tabs, page text, screenshot, console, wait): the first read raises
  a card. **Allow reads for this run** lets that run read for the rest of the run;
  **Allow once** covers one call.
- **Opening a page** asks every time and shows the address without its query or
  fragment, and always says when they are there: `(+query, N characters)` and
  `(+fragment)`, and `(path cut)` when a long path is shortened. The options are
  **Allow once**, or **Allow opening any page the browser policy allows (localhost
  and your added hosts) and closing tabs, for this run**: that second option lets
  the agent open any such page, with any query, for the rest of the run.
  Closing a tab has its own card ("Close a browser tab?").
- Nothing is saved across runs and there is no "always". Turning the workspace
  switch off ends the grants.
- Saying no three times in a minute pauses further cards for five minutes, so an
  agent cannot nag you.
- Permission modes of the agent's own tool never answer these cards, including
  auto-accept. A rule in your permission policy for `mcp:smind-browser.<tool>`
  can allow or refuse a tool; with no rule smind asks.

## Which agents can use it

| Agent | Browser | Screenshots |
| --- | --- | --- |
| Claude Code | yes | yes |
| Codex | no | no |
| GLM | offered, text only | no |
| Kimi | offered if the agent advertises MCP support | unknown |
| OpenCode | offered if the agent advertises MCP support | yes |
| GitHub Copilot | offered if the agent advertises MCP support | unknown |
| Cline | offered if the agent advertises MCP support | unknown |
| Goose | offered if the agent advertises MCP support | unknown |
| Cursor | offered if the agent advertises MCP support | unknown |
| providers you add yourself under `providers.custom` | offered, not guaranteed | unknown |
| compatible-endpoint providers made from a recipe | offered, not guaranteed: the model behind the endpoint decides whether it calls the tools | unknown |
| plugin providers | never | none |

A screenshot reaches the agent as an image only when smind knows the agent
reads images; when it is unknown or no, the agent gets a text note and works
from the page text.

An agent without the browser is told
"the browser is not available for this agent" with the reason, and gets no
browser tools. On **Windows the browser is not offered**. An agent that starts
its tools from a process outside its own process tree (a tool server that was
already running, a sandbox with its own process namespace) cannot reach the
browser: the call fails with "the browser is not available for this agent".

## What it does not protect against

- **The cards protect against the model, not against a shell-capable agent that
  attacks the daemon.** Any process in the run's own process tree (the agent's
  shell, anything it starts) can talk to the daemon without saying who it is,
  and smind treats such a connection as you, exactly as for every other
  permission card: it could answer a card through the daemon's websocket. Keep
  the daemon token private, and do not let an agent you do not trust run shell
  commands.
- **The list of places the browser must never reach is best effort against the
  agent's own shell, for listeners of other smind processes.** The daemon and a
  login callback in the daemon's own process are remembered in memory and cannot
  be unreserved by touching files. A relay or web listener started as another
  smind process leaves a small record under your smind home (`listeners/`) that
  its process rewrites every few seconds and also repairs if something is put in
  its place; a shell running as you can still delete or change such a record, so
  that port can be unprotected for a few seconds until it heals. An unreadable
  or flooded `listeners/` directory makes the browser refuse.
- The kill switch: set `browser.disabled: true` in `config.yaml` (or
  `SMIND_BROWSER_DISABLED=1`) and the browser is off for every workspace: open
  sessions end and every grant is dropped within about two seconds, and any call
  made meanwhile is refused.
