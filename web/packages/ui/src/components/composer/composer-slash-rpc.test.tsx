import { act } from "react";
import { cleanup, fireEvent, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { RpcError } from "@/lib/ws-client";
import { buildSlashCommands, slashRpcPrompt } from "@/lib/slash-commands";

import { Composer } from "@/components/composer/composer";
import type { PluginCommand } from "@/lib/plugin-contrib";
import { useComposerAttachmentsStore } from "@/stores/composer-attachments-store";
import { useComposerInsertStore } from "@/stores/composer-insert-store";
import { FakeWsClient } from "@/test/fake-ws-client";
import { renderWithClientProp as render } from "@/test/render-with-daemon";

const prompt = (plugin: string, name: string, text: string): PluginCommand => ({ plugin, name, qualified: `${plugin}:${name}`, description: `${name} desc`, prompt: text });
const rpc = (plugin: string, name: string, rpcName = "gen"): PluginCommand => ({ plugin, name, qualified: `${plugin}:${name}`, description: `${name} desc`, rpc: rpcName });
const COMMANDS = [rpc("acme", "hello"), prompt("acme", "plan", "Plan it.")];

async function flush(): Promise<void> {
  await act(async () => {
    await new Promise((r) => setTimeout(r, 0));
  });
}

async function renderComposer(commands: PluginCommand[] | null = COMMANDS) {
  const client = new FakeWsClient();
  const sent: string[] = [];
  render(
    <Composer
      client={client}
      taskId={1}
      chatId={10}
      isDefaultChat
      boundProvider={null}
      connected
      runningRunId={null}
      queuedItems={[]}
      onCancelQueued={async () => {}}
      onSubmit={async (_p, prompt) => {
        sent.push(prompt);
        return { kind: "started", runId: "run-x" };
      }}
      onStop={async () => {}}
    />,
  );
  client.nth("daemon.info", 0).resolve({ attachments: null });
  if (commands) client.nth("plugin.commands", 0).resolve(commands);
  await flush();
  return { client, sent };
}

const area = () => screen.getByLabelText("Prompt") as HTMLTextAreaElement;
const type = (text: string) => fireEvent.change(area(), { target: { value: text } });
const key = (k: string, init: object = {}) => fireEvent.keyDown(area(), { key: k, ...init });

beforeEach(() => {
  useComposerAttachmentsStore.setState({ byKey: {}, mentions: {} });
  useComposerInsertStore.setState({ mounted: [], target: null, request: null });
  localStorage.clear();
});
afterEach(cleanup);

describe("RPC-backed slash commands in the composer", () => {
  it("TestSlashRpc_InsertsNeverSends: selecting calls plugin.call, shows Running, inserts the prompt as plain text, caret at the end, nothing sent", async () => {
    const { client, sent } = await renderComposer();
    type("/hel");
    await flush();
    expect(screen.getByRole("option")).toHaveTextContent("/hellohello descfrom acme");
    key("Enter");
    await flush();
    const call = client.nth("plugin.call", 0);
    expect(call.params).toEqual({ pluginId: "acme", rpc: "gen", params: { command: "acme:hello", args: "" } });
    expect(screen.getByRole("status")).toHaveTextContent("Running");
    expect(area().value).toBe("/hel");
    // a second Enter while it runs does not call again
    key("Enter");
    await flush();
    expect(client.calls.filter((c) => c.method === "plugin.call")).toHaveLength(1);

    const text = "Hello! <b>bold</b> **md** [x](javascript:alert(1)) <img src=x onerror=alert(1)>";
    await act(async () => call.resolve({ prompt: text }));
    await flush();
    expect(area().value).toBe(text); // the exact characters: not rendered, not stripped
    expect(area().selectionStart).toBe(text.length);
    expect(screen.queryByRole("listbox")).toBeNull();
    expect(document.querySelector("b, img, a")).toBeNull();
    expect(sent).toEqual([]);
  });

  it("an answer for a token the draft no longer holds is dropped, and Escape abandons a pending command", async () => {
    const { client } = await renderComposer();
    type("/hel");
    await flush();
    key("Enter");
    await flush();
    type("something else");
    await flush();
    await act(async () => client.nth("plugin.call", 0).resolve({ prompt: "late" }));
    await flush();
    expect(area().value).toBe("something else");

    type("/hel");
    await flush();
    key("Enter");
    await flush();
    key("Escape");
    await act(async () => client.nth("plugin.call", 1).resolve({ prompt: "after escape" }));
    await flush();
    expect(area().value).toBe("/hel");
  });

  it("TestSlashRpc_ErrorShownInline: a typed daemon error stays inline, the draft is untouched, nothing is sent; typing clears it", async () => {
    const { client, sent } = await renderComposer();
    type("/hello");
    await flush();
    key("Enter");
    await flush();
    await act(async () => client.nth("plugin.call", 0).reject(new RpcError("plugin.call: plugin acme: rpc gen did not answer within 5s", "call_timeout")));
    await flush();
    const alert = screen.getByRole("alert");
    expect(alert).toHaveTextContent("plugin acme: rpc gen did not answer within 5s");
    expect(alert).not.toHaveTextContent("plugin.call:");
    expect(alert).toHaveAttribute("data-code", "call_timeout");
    expect(area().value).toBe("/hello");
    expect(sent).toEqual([]);
    type("/hell");
    await flush();
    expect(screen.queryByRole("alert")).toBeNull();

    // a result that is not {prompt: non-empty text} is a failure too, never an insertion
    for (const bad of [null, {}, { prompt: "" }, { prompt: 7 }, { prompt: "a\u0000b" }, { prompt: "x".repeat(16 * 1024 + 1) }, "text"]) {
      expect(slashRpcPrompt(bad)).toBeNull();
    }
    expect(slashRpcPrompt({ prompt: "ok" })).toBe("ok");
    type("/hello");
    await flush();
    key("Enter");
    await flush();
    await act(async () => client.nth("plugin.call", 1).resolve({ prompt: "" }));
    await flush();
    expect(screen.getByRole("alert")).toHaveTextContent("did not return prompt text");
    expect(area().value).toBe("/hello");
  });
});

describe("TestSlashRpc_BuiltinAndCollisionRules", () => {
  const builtin = [{ name: "hello", description: "builtin", text: "built-in text" }];
  it("a built-in keeps the bare name against an rpc command, which stays reachable as /<plugin>:<name>", () => {
    const rows = buildSlashCommands(builtin, [rpc("acme", "hello")]);
    expect(rows.map((r) => [r.label, r.builtin, r.rpc?.rpc])).toEqual([
      ["hello", true, undefined],
      ["acme:hello", false, "gen"],
    ]);
    expect(rows[1]!.names).toEqual(["acme:hello"]);
    expect(rows[1]!.text).toBe("");
  });
  it("between an rpc and a prompt command the first plugin by id owns the bare name", () => {
    const rows = buildSlashCommands([], [prompt("zeta", "go", "Z"), rpc("alpha", "go")]);
    expect(rows.map((r) => [r.label, r.rpc ? "rpc" : "prompt"])).toEqual([
      ["go", "rpc"],
      ["zeta:go", "prompt"],
    ]);
  });
});
