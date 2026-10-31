---
title: Provider recipes
description: Add a Claude Code or Codex login for another vendor's compatible endpoint from a built-in template.
summary: "This page explains provider recipes in sMind by Spacing Mind: built-in templates that create a provider instance for a vendor such as Z.AI, DeepSeek, OpenRouter or a local model server, and how to use Add from recipe in Settings. It is for people who want to run an agent against an endpoint other than the first-party ones."
order: 19
---

# Provider recipes

A recipe is a built-in template for one [provider instance](providers.md): it
points Claude Code (or Codex) at another vendor's API, with the vendor's base
URL, model ids and caveats already filled in. You paste a key and, for a few
recipes, an endpoint or a model id. The catalog ships inside the daemon: there
is no remote catalog, and it changes only with a release.

## Add from recipe

1. Open **Settings → Providers** and choose **Add from recipe**.
2. The catalog opens as a list. Each row shows the recipe's name, a one-line
   description, the wire protocol it speaks and an **Unverified** badge when it
   was never run against a live endpoint. Pick a row.
3. Read the caveats; the form links the vendor's docs.
4. Set an id and label. Both are optional; the id defaults to the recipe id.
5. If the recipe needs one, type the endpoint URL.
6. Edit the model list. It starts with the recipe's models: remove one with
   its ✕, or type an id and press Enter. A recipe that needs a model id
   (Ollama, LM Studio, LiteLLM) will not submit with an empty list.
7. Paste the key into the password field and press **Add provider**.

The key leaves the field as soon as you submit, whether or not the daemon
accepts it. If the daemon refuses (for example the id belongs to another
entry), its message appears under the form and you enter the key again. The new
instance appears in the provider list with a **recipe** badge, plus
**unverified** when the row is unverified.

Adding is for people only: agents and plugins are refused. Adding the same
recipe with the same id again changes nothing; an id used by another entry is
refused.

## The recipes

| Recipe id | Runs on | You provide | Source | Verified |
| --- | --- | --- | --- | --- |
| `zai-glm` | Claude Code | key | vendor docs | no |
| `qwen-dashscope` | Claude Code | key | Paseo docs | no |
| `deepseek` | Claude Code | key | vendor docs | no |
| `kimi-moonshot` | Claude Code | key | inferred | no: the Claude Code base URL is a guess |
| `minimax` | Claude Code | key | vendor docs | no |
| `openrouter` | Claude Code | key | vendor docs | no |
| `ollama` | Claude Code | a model id (local, no key) | vendor docs | no |
| `lm-studio` | Claude Code | a model id (local, no key) | vendor docs | no |
| `litellm` | Claude Code | URL, key, model ids | vendor docs | no |
| `anthropic-compatible` | Claude Code | URL, key, model ids | Paseo docs | no |
| `openai-responses-compatible` | Codex | URL, key, model ids | Paseo docs | no |
| `chat-completions-via-opencode` | OpenCode | not addable yet | inferred | no |

"Verified: no" means no row has been run against a live endpoint yet. Treat
base URLs and model ids as what the vendor's documentation said on 2026-10-30,
and try the inferred rows with care. The `openai-responses-compatible` row
speaks only the OpenAI Responses API; Codex has no chat-completions mode.

## What a recipe instance does differently

- **Its own config directory.** A Claude Code recipe runs with its own
  `CLAUDE_CONFIG_DIR` under `<SMIND_HOME>/providers/<id>/claude`, so its files
  never mix with `~/.claude`. Set `inheritClaudeConfig: true` on the entry to
  use `~/.claude` instead.
- **No web search.** Every Claude Code recipe disallows `WebSearch`: third-party
  endpoints have no hosted search tool.
- **No thinking picker where it cannot work.** A recipe whose endpoint cannot
  turn thinking off is marked `thinking: hidden`: the run ignores a requested
  thinking level and the picker is hidden for that provider.
- **Vendor-shaped rate limits.** An instance has a vendor profile (`zai`,
  `anthropic`, `openai` or `generic`). A GLM-worded rate limit on a Z.AI
  instance puts that instance on cooldown until its reset (UTC+8) and leaves
  `claude-native` running. Other vendors' limits are recognised by generic
  patterns, without a reset time.
- **Keys you cannot read back.** The instance's detail page lists each key as
  *Set · ••••abcd* or *Not set*; **Replace key** and **Clear key** manage it.
  See [Write-only secrets](write-only-secrets.md).

On a phone, the new-task form shows a read-only note under the provider picker
(for example "Recipe zai-glm · Unverified · Key set (…ab12)"). Keys are never
entered or shown on mobile.

For GLM, the default way to add a new login is the `zai-glm` recipe. If you
add it you can also see the plan's usage windows: see
[Z.AI usage meter](zai-usage-meter.md).

## Related

- [Providers](providers.md): accounts, instances, and the `providers.custom`
  config for an endpoint no recipe covers.
- [Write-only secrets](write-only-secrets.md): where the key goes.
