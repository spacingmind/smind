---
title: Write-only secrets
description: How smind stores a provider key you type once, and what it never shows again.
summary: "This page explains write-only secrets in sMind by Spacing Mind: how a provider key is stored and referenced, what you can see afterwards (a set flag and a short fingerprint), where it is used and what it does not protect against. It is for people who add keys to provider instances."
order: 20
---

# Write-only secrets

A provider instance that talks to another vendor needs a key. smind stores that
key for you and lets you set, replace or clear it, but never shows it again:
the store is write-only.

## Set, replace and clear a key

In **Settings → Providers**, open the instance. The **Keys** card lists each
key as *Set · ••••abcd* or *Not set*. The four characters are the end of a
fingerprint (a hash of the key), not of the key: enough to see that a key
changed, not enough to use it.

- **Replace key** opens a password dialog. The field is emptied as soon as you
  submit.
- **Clear key** asks for confirmation, then removes the stored value.

Only a person can do this. Agents and plugins are refused, and no call returns
a stored value. When you add an instance with [Add from
recipe](provider-recipes.md), the key you paste is stored the same way.

## Reference a key from `config.yaml`

An instance's `env` can reference a stored key by name. The key itself never
goes in the file:

```yaml
providers:
  custom:
    - id: zai
      extends: claude-native
      env:
        ANTHROPIC_BASE_URL: https://api.z.ai/api/anthropic
        ANTHROPIC_AUTH_TOKEN: {secret: zai-key}
```

An `env` value is one of a plain literal, `{secret: <name>}` (a reference to a
stored key) or `{passthrough: true}` (copy the variable from the daemon's own
environment at launch, for keys you export outside smind). A variable whose name
marks it as a key (for example `ANTHROPIC_AUTH_TOKEN` or `OPENAI_API_KEY`) must
be a reference or a passthrough: a key written inline is rejected, naming the
field. The one exception is `ANTHROPIC_API_KEY`, which accepts an empty value
where a vendor needs the variable set but empty. Each launch type allows only
certain variable names; the list is in [Providers](providers.md).

## Where the value lives and when it is used

Values are kept in `smind.db`, with the same protection as account credentials.
A key is resolved only when a run launches, and goes into that child process's
environment. Deleting an instance from `config.yaml` and reloading providers
(or restarting smind) removes its stored keys.

## What is protected

A stored key never appears in `config.yaml`, in a command line, in logs, in
events, in the provider list, in error text or in anything sent over a relay.

## What is not protected

The agent's own shell can read its environment. By default Claude Code does not
hide the token from its Bash tool. For Claude Code instances that hold a key,
smind adds best-effort deny rules for the obvious ways to print it (`env`,
`printenv`, `set`, `export -p`, `declare -x`, `echo $NAME`, and reading
`/proc/*/environ` or the provider folder under `SMIND_HOME`). These are pattern
matches and are easily bypassed; they raise the cost of a careless prompt
injection and do not stop a determined one. Codex and other agents have no
equivalent, because their shell tools inherit the environment.

So: use a narrowly scoped key with a spend limit, and for Codex or other
vendors put a local gateway (for example LiteLLM) in front, so the agent holds
only a throwaway token.

smind does not turn on Claude Code's own environment scrubbing by default,
because with it on Claude Code forces the permission mode back to `default`,
which conflicts with smind's permission modes. You may opt in per instance with
`CLAUDE_CODE_SUBPROCESS_ENV_SCRUB: "1"` in the instance `env` if you accept
that.

## Related

- [Provider recipes](provider-recipes.md)
- [Providers](providers.md)
- [Configuration reference](configuration.md)
