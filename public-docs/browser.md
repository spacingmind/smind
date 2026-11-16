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
- **Opening a page** asks every time and shows the address without its query:
  **Allow once**, or **Allow `browser_open` for this run** (which also covers
  closing a tab).
- Nothing is saved across runs and there is no "always". Turning the workspace
  switch off ends the grants.
- Saying no three times in a minute pauses further cards for five minutes, so an
  agent cannot nag you.
- Permission modes of the agent's own tool never answer these cards, including
  auto-accept. A rule in your permission policy for `mcp:smind-browser.<tool>`
  can allow or refuse a tool; with no rule smind asks.

## Which agents can use it

Agents that smind runs through the Agent Client Protocol (for example GLM and
the other providers run that way) and Claude Code runs can use it. Codex-native
runs and plugin providers never get it. On **Windows the browser is not
offered**. An agent that starts its tools from a process outside its own process
tree (a tool server that was already running, a sandbox with its own process
namespace) cannot reach the browser: the call fails with "the browser is not
available for this agent".

## What it does not protect against

- A program running as you on the same machine can do what you can: it can read
  the daemon token. The browser is a convenience for a project you trust, not a
  sandbox for hostile code.
- The agent's own shell, running as you, can reach the same places a person's
  shell can.
- The kill switch: set `browser.disabled: true` in `config.yaml` (or
  `SMIND_BROWSER_DISABLED=1`) and the browser is off for every workspace and any
  open session ends at the next call.
