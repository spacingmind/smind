#!/usr/bin/env python3
"""hello-process: the smallest useful smind process plugin (ADR-0042).

The host talks to this script over stdin/stdout with LSP framing
("Content-Length: N", a blank line, then N bytes of JSON-RPC 2.0). Anything
printed to stderr goes to `smind plugin logs hello-process`.
"""
import json
import sys

settings = {}


def read_message():
    length = None
    while True:
        line = sys.stdin.buffer.readline()
        if not line:
            return None  # the host closed stdin: exit
        line = line.strip()
        if not line:
            break
        key, _, value = line.partition(b":")
        if key.strip().lower() == b"content-length":
            length = int(value.strip())
    if length is None:
        return None
    return json.loads(sys.stdin.buffer.read(length))


def send(message):
    body = json.dumps(message).encode("utf-8")
    sys.stdout.buffer.write(b"Content-Length: %d\r\n\r\n" % len(body) + body)
    sys.stdout.buffer.flush()


def log(text):
    print(text, file=sys.stderr, flush=True)


def echo_prompt(params):
    # The slash command calls this with {"command": "<plugin>:<name>", "args": "<text after the name>"}.
    args = (params or {}).get("args", "").strip()
    greeting = settings.get("greeting") or "Hello"
    prompt = greeting + "! " + (args if args else "Please introduce yourself and summarize this task.")
    return {"prompt": prompt}


def hello_view(params):
    # The panel calls this with {"panelId": ..., "context": ...} and renders the returned
    # ui:1 document. It is data only: the host validates it and runs every action itself.
    panel_id = (params or {}).get("panelId") or ""
    greeting = settings.get("greeting") or "Hello"
    return {
        "ui": 1,
        "root": {
            "type": "stack",
            "children": [
                {"type": "heading", "text": "Hello"},
                {"type": "text", "text": "Greeting: " + greeting},
                {"type": "keyValue", "items": [
                    {"key": "Greeting", "value": greeting},
                    {"key": "Panel", "value": panel_id},
                ]},
                {"type": "button", "label": "Insert greeting", "action": {
                    "kind": "rpc", "rpc": "echoPrompt",
                    "params": {"command": "hello-process:hello", "args": ""},
                }},
                {"type": "button", "label": "Suggest a prompt", "action": {
                    "kind": "prompt", "text": "Hello! Please summarize this task.",
                }},
                {"type": "button", "label": "Docs", "action": {
                    "kind": "openUrl", "url": "https://example.com/docs",
                }},
            ],
        },
    }


def main():
    global settings
    while True:
        msg = read_message()
        if msg is None:
            return
        method = msg.get("method")
        msg_id = msg.get("id")
        if method == "plugin.initialize":
            settings = (msg.get("params") or {}).get("settings") or {}
            log("hello-process ready, greeting=%r" % settings.get("greeting"))
            send({"jsonrpc": "2.0", "id": msg_id, "result": {}})
        elif method == "plugin.shutdown":
            return
        elif method == "rpc.echoPrompt":
            send({"jsonrpc": "2.0", "id": msg_id, "result": echo_prompt(msg.get("params"))})
        elif method == "rpc.helloView":
            send({"jsonrpc": "2.0", "id": msg_id, "result": hello_view(msg.get("params"))})
        elif method == "hook.run.finished":
            p = msg.get("params") or {}
            log("run finished: run=%s task=%s status=%s" % (p.get("runId"), p.get("taskId"), p.get("status")))
        elif msg_id is not None:
            send({"jsonrpc": "2.0", "id": msg_id, "error": {"code": -32601, "message": "method not found: %s" % method}})


if __name__ == "__main__":
    main()
