# hello-process

A tiny process plugin for smind (ADR-0042). Python 3, standard library only, no network, no build step.

| Piece | What it shows |
|---|---|
| `process` | `python3 server.py`, spoken to over stdio (LSP-framed JSON-RPC 2.0) |
| `contributes.rpc` `echoPrompt` | a method a client may call with `plugin.call`; only declared names are ever forwarded |
| `contributes.commands` `hello` | an RPC-backed slash command: typing `/hello` calls `echoPrompt` and inserts the returned `prompt` into the composer (it never sends) |
| `contributes.hooks` `run.finished` | an observe hook; the plugin logs each finished run (`smind plugin logs hello-process`) |
| `settings` `greeting` | a typed setting, delivered in `plugin.initialize` |

Try it (plugins must be on: `plugins.enabled: true` in `config.yaml`):

```sh
smind plugin install plugin-examples/hello-process
smind plugin enable hello-process        # shows what runs, asks to confirm
smind plugin logs hello-process
```

Then type `/hello` (or `/hello-process:hello`) in a task's composer.

The full authoring guide is a separate item (DOC.1); the contract this example uses is in ADR-0042 and `public-docs/plugins.md`.
