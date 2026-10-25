import { act } from "react";
import { cleanup, fireEvent, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { Composer } from "@/components/composer/composer";
import type { PluginCommand } from "@/lib/plugin-contrib";
import { useComposerAttachmentsStore } from "@/stores/composer-attachments-store";
import { useComposerInsertStore } from "@/stores/composer-insert-store";
import { FakeWsClient } from "@/test/fake-ws-client";
import { renderWithClientProp as render } from "@/test/render-with-daemon";

const cmd = (plugin: string, name: string, prompt: string, description = `${name} desc`): PluginCommand => ({
  plugin,
  name,
  qualified: `${plugin}:${name}`,
  description,
  prompt,
});
const COMMANDS = [cmd("acme", "review", "Review the staged diff.\n"), cmd("acme", "plan", "Plan {{first}}."), cmd("zeta", "review", "Zeta review")];

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
const options = () => screen.queryAllByRole("option").map((o) => o.textContent);

beforeEach(() => {
  useComposerAttachmentsStore.setState({ byKey: {}, mentions: {} });
  useComposerInsertStore.setState({ mounted: [], target: null, request: null });
  localStorage.clear();
});
afterEach(cleanup);

describe("slash commands in the composer", () => {
  it("TestSlashCommands_Autocomplete_InsertsNeverSends: Enter/Tab/click insert the prompt, caret at the end, send handler never called", async () => {
    const { sent } = await renderComposer();
    area().focus();
    type("/");
    await flush();
    expect(document.activeElement).toBe(area());
    expect(screen.getByRole("listbox", { name: "Commands" })).toBeInTheDocument();
    expect(options()).toEqual(["/reviewreview descfrom acme", "/planplan descfrom acme", "/zeta:reviewreview descfrom zeta"]);
    expect(area()).toHaveAttribute("role", "combobox");
    expect(area()).toHaveAttribute("aria-expanded", "true");
    expect(area()).toHaveAttribute("aria-controls", screen.getByRole("listbox").id);
    expect(area()).toHaveAttribute("aria-activedescendant", screen.getAllByRole("option")[0]!.id);

    key("ArrowDown");
    expect(area()).toHaveAttribute("aria-activedescendant", screen.getAllByRole("option")[1]!.id);
    key("ArrowUp");
    key("ArrowUp"); // wraps to the last row
    expect(area()).toHaveAttribute("aria-activedescendant", screen.getAllByRole("option")[2]!.id);
    key("ArrowDown"); // back to the first

    key("Enter");
    await flush();
    expect(area().value).toBe("Review the staged diff.\n");
    expect(area().selectionStart).toBe(area().value.length);
    expect(screen.queryByRole("listbox")).toBeNull();
    expect(sent).toEqual([]);
    expect(document.activeElement).toBe(area());
  });

  it("Tab and a click also insert and never send; Escape closes without inserting", async () => {
    const { sent } = await renderComposer();
    type("/pl");
    await flush();
    expect(options()).toHaveLength(1);
    key("Escape");
    expect(screen.queryByRole("listbox")).toBeNull();
    expect(area().value).toBe("/pl");
    // Leaving the token re-arms the list; a new `/` opens it again.
    type("");
    await flush();
    type("/p");
    await flush();
    key("Tab");
    await flush();
    expect(area().value).toBe("Plan {{first}}.");

    type("/zeta:rev");
    await flush();
    fireEvent.click(screen.getByRole("option"));
    await flush();
    expect(area().value).toBe("Zeta review");
    expect(sent).toEqual([]);
  });

  it("TestSlashCommands_FirstPluginByIdWinsBetweenPlugins: the bare /review is acme's; zeta's is /zeta:review", async () => {
    await renderComposer([cmd("zeta", "review", "Z"), cmd("acme", "review", "A")]);
    type("/review");
    await flush();
    expect(options().map((o) => o?.split("review")[0])).toEqual(["/", "/zeta:"]);
    key("Enter");
    await flush();
    expect(area().value).toBe("A");
  });

  it("TestSlashCommands_NoPluginCommands_ComposerUnchanged: no popover, Enter sends the text as before", async () => {
    const { sent } = await renderComposer([]);
    type("/");
    await flush();
    expect(screen.queryByRole("listbox")).toBeNull();
    expect(area()).not.toHaveAttribute("aria-controls");
    type("/review");
    await flush();
    expect(screen.queryByRole("listbox")).toBeNull();
    key("Enter");
    await flush();
    expect(sent).toEqual(["/review"]);
  });

  it("an old daemon without the RPC leaves the composer unchanged", async () => {
    const { client, sent } = await renderComposer(null);
    client.nth("plugin.commands", 0).reject(new Error("unknown method"));
    await flush();
    type("/x");
    await flush();
    expect(screen.queryByRole("listbox")).toBeNull();
    key("Enter");
    await flush();
    expect(sent).toEqual(["/x"]);
  });

  it("does not open for a slash that is not at the start, nor for no match", async () => {
    await renderComposer();
    type("look at /usr/bin");
    await flush();
    expect(screen.queryByRole("listbox")).toBeNull();
    type("/nomatch");
    await flush();
    expect(screen.queryByRole("listbox")).toBeNull();
  });

  it("TestSlashCommands_PaletteInsertsNeverSends: the store inserts into the focused composer after a blank line and never submits", async () => {
    const { sent } = await renderComposer();
    expect(useComposerInsertStore.getState().mounted).toEqual(["1:10"]);
    type("draft so far");
    fireEvent.focus(area());
    expect(useComposerInsertStore.getState().target).toBe("1:10");
    act(() => {
      expect(useComposerInsertStore.getState().insert("Review the diff.")).toBe(true);
    });
    await flush();
    expect(area().value).toBe("draft so far\n\nReview the diff.");
    expect(area().selectionStart).toBe(area().value.length);
    expect(sent).toEqual([]);
  });

  it("with no composer mounted the insert is refused", () => {
    useComposerInsertStore.setState({ mounted: [], target: null, request: null });
    expect(useComposerInsertStore.getState().insert("x")).toBe(false);
  });
});
