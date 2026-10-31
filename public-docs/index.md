---
title: sMind by Spacing Mind documentation
description: What smind is and where to read next.
summary: "sMind by Spacing Mind is a self-hosted platform for running coding agents, delivered as one Go binary with a web app, desktop app, command line and mobile access. This page is for developers who run coding agents every day and want them on their own machine under their own accounts."
order: 1
---

# smind

## What smind is

smind is a self-hosted platform for running coding agents. It is one Go binary
that serves both the API and a web interface, and it spreads agent traffic
across your own provider accounts instead of a single shared pool. Each task
runs in its own isolated git worktree, so several agents can work on one
repository without stepping on each other. You use it from the web app, the
desktop app, the command line or a phone.

It is for developers who run coding agents every day and want them on their own
machine or server, under their own accounts, with a clear view of what each
agent is doing and what it costs.

## Where to read next

- [Getting started](getting-started.md): install smind, start the daemon, create your first workspace and task.
- [Providers](providers.md): connect Claude, Codex, GLM and Kimi accounts.
- [Workspaces and tasks](workspaces-and-tasks.md): the daily workflow, chats, attachments and the queue.
- [Git and pull requests](git-and-prs.md): branches, publishing, updating from base and opening pull requests.
- [Mobile and relay pairing](mobile-and-relay.md): pair a phone, what end-to-end encryption protects, push notifications.
- [Orchestration](orchestration.md): run a Lead that launches Peers, with budgets, alerts and permission rules.
- [Inbox](inbox.md): one list of permissions, validator verdicts and stalled work that need you, on the web and on a phone.
- [Scripting and permissions](scripting-and-permissions.md): drive agents from scripts, permission modes, the MCP server.
- [Run the daemon](running-the-daemon.md): run it as a service, reach it from other devices, Docker, updates, backups.
- [Usage and limits](usage-and-limits.md): what your accounts have used and what happens at a rate limit.
- [CLI reference](cli-reference.md): every `smind` command.
- [Troubleshooting](troubleshooting.md): fix a daemon that will not start, login problems and reconnects.
- [Configuration reference](configuration.md): the keys in `config.yaml` and their defaults.
- [Plugins](plugins.md): install MCP servers, skills, profiles, commands, themes and plugin processes, and see what they do first.
- [Writing a plugin](plugins-authoring.md): the manifest reference, capabilities, hooks, RPCs and a worked example.
- [Automations](automations.md): start a task on a schedule or when a workflow fails.
- [Connected hosts](hosts.md): pair two daemons and choose which direction works.

## How the docs are organised

The pages are listed in reading order: first orientation and install, then
daily work, then mobile and orchestration, then reference pages you look things
up in. Each page stands on its own and links to the pages it relies on. Commands
are shown in copy-pasteable blocks; run `smind --help` for the usage text of
the version you have installed.
