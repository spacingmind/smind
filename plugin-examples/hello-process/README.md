# hello-process

A tiny process plugin for smind (ADR-0042). Python 3, standard library only, no network, no build step.

| Piece | What it shows |
|---|---|
| `process` | `python3 server.py`, spoken to over stdio (LSP-framed JSON-RPC 2.0) |
| `contributes.rpc` `echoPrompt` | a method a client may call with `plugin.call`; only declared names are ever forwarded |
| `contributes.commands` `hello` | an RPC-backed slash command: typing `/hello` calls `echoPrompt` and inserts the returned `prompt` into the composer (it never sends) |
| `contributes.hooks` `run.finished` | an observe hook; the plugin logs each finished run (`smind plugin logs hello-process`) |
| `settings` `greeting` | a typed setting, delivered in `plugin.initialize` |

## Panels

Two panels ship with the example: `hello` (a task panel whose view the plugin answers through the `helloView` rpc) and `about` (a sidebar panel read from `views/about.json`, reached from the `Hello` sidebar item). A view is data only, a `ui: 1` document: the host validates it and runs every action itself, so the plugin never ships UI code. See [public-docs/plugins-authoring.md](../../public-docs/plugins-authoring.md).

Try it (plugins must be on: `plugins.enabled: true` in `config.yaml`):

```sh
smind plugin install plugin-examples/hello-process
smind plugin enable hello-process        # shows what runs, asks to confirm
smind plugin logs hello-process
```

Then type `/hello` (or `/hello-process:hello`) in a task's composer.

The full authoring guide is a separate item (DOC.1); the contract this example uses is in ADR-0042 and `public-docs/plugins.md`.
