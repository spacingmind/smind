import { act } from "react";
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { Composer } from "@/components/composer/composer";
import type { AttachmentLimits, AttachmentMeta, AttachmentRequest } from "@/lib/types";
import { useComposerAttachmentsStore } from "@/stores/composer-attachments-store";
import { FakeWsClient } from "@/test/fake-ws-client";
import { renderWithClientProp as render } from "@/test/render-with-daemon";

const LIMITS: AttachmentLimits = {
  maxCount: 10,
  maxImageBytes: 5 << 20,
  maxFileBytes: 10 << 20,
  maxTotalBytes: 10 << 20,
  maxMessageBytes: 16 << 20,
  images: ["image/png"],
};
const PATHS = ["README.md", "src/main.go", "src/util.go", "docs/my notes.md"];

async function flush(): Promise<void> {
  await act(async () => {
    await new Promise((r) => setTimeout(r, 0));
  });
}

interface Sent {
  prompt: string;
  attachments?: AttachmentRequest[];
  previews?: AttachmentMeta[];
}

async function renderComposer(limits: AttachmentLimits = LIMITS) {
  const client = new FakeWsClient();
  const sent: Sent[] = [];
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
      onSubmit={async (_p, prompt, _perm, _t, _w, _m, attachments, previews) => {
        sent.push({ prompt, attachments, previews });
        return { kind: "started", runId: "run-x" };
      }}
      onStop={async () => {}}
    />,
  );
  client.nth("daemon.info", 0).resolve({ attachments: limits });
  await flush();
  return { client, sent };
}

const area = () => screen.getByLabelText("Prompt") as HTMLTextAreaElement;
function type(text: string) {
  fireEvent.change(area(), { target: { value: text } });
}
async function open(client: FakeWsClient, text = "@") {
  type(text);
  await flush();
  client.nth("task.searchIndex", 0).resolve({ paths: PATHS });
  await flush();
}
const key = (k: string, init: object = {}) => fireEvent.keyDown(area(), { key: k, ...init });
const options = () => screen.queryAllByRole("option").map((o) => o.textContent);

beforeEach(() => {
  useComposerAttachmentsStore.setState({ byKey: {}, mentions: {} });
  localStorage.clear();
});
afterEach(cleanup);

describe("@ file mention", () => {
  it("opens on @, filters as you type and wires the combobox aria", async () => {
    const { client } = await renderComposer();
    expect(screen.queryByRole("listbox")).toBeNull();
    await open(client);
    expect(screen.getByRole("listbox")).toBeInTheDocument();
    expect(options()).toHaveLength(4);
    expect(area()).toHaveAttribute("role", "combobox");
    expect(area()).toHaveAttribute("aria-expanded", "true");
    expect(area()).toHaveAttribute("aria-controls", screen.getByRole("listbox").id);
    expect(area()).toHaveAttribute("aria-activedescendant", screen.getAllByRole("option")[0]!.id);
    type("@main");
    expect(options()).toHaveLength(1);
    expect(options()[0]).toContain("main.go");
    type("@zzz");
    expect(screen.getByText("No matching files")).toBeInTheDocument();
  });

  it("does not open inside an email or a word", async () => {
    const { client } = await renderComposer();
    type("mail me a@b");
    await flush();
    expect(screen.queryByRole("listbox")).toBeNull();
    expect(client.calls.filter((c) => c.method === "task.searchIndex")).toHaveLength(0);
  });

  it("shows Searching… while the index loads, and fetches it once per task and only when a mention is active", async () => {
    const { client } = await renderComposer();
    type("plain text");
    await flush();
    expect(client.calls.filter((c) => c.method === "task.searchIndex")).toHaveLength(0);
    type("@");
    await flush();
    expect(screen.getByText("Searching…")).toBeInTheDocument();
    client.nth("task.searchIndex", 0).resolve({ paths: PATHS });
    await flush();
    type("@s");
    type("@sr");
    await flush();
    expect(client.calls.filter((c) => c.method === "task.searchIndex")).toHaveLength(1);
  });

  it("ArrowDown/ArrowUp move the highlight and wrap", async () => {
    const { client } = await renderComposer();
    await open(client);
    const selected = () => screen.getAllByRole("option").findIndex((o) => o.getAttribute("aria-selected") === "true");
    expect(selected()).toBe(0);
    key("ArrowUp");
    expect(selected()).toBe(3);
    key("ArrowDown");
    expect(selected()).toBe(0);
    key("ArrowDown");
    expect(selected()).toBe(1);
  });

  it("Enter accepts without submitting, inserts @path plus a space and puts the caret after it", async () => {
    const { client, sent } = await renderComposer();
    await open(client, "see @mai");
    key("Enter");
    await flush();
    expect(sent).toHaveLength(0);
    expect(area()).toHaveValue("see @src/main.go ");
    expect(area().selectionStart).toBe("see @src/main.go ".length);
    expect(screen.queryByRole("listbox")).toBeNull();
  });

  it("Tab accepts too, and a path with whitespace is quoted", async () => {
    const { client, sent } = await renderComposer();
    await open(client, "@notes");
    key("Tab");
    await flush();
    expect(area()).toHaveValue('@"docs/my notes.md" ');
    expect(sent).toHaveLength(0);
  });

  it("Esc closes the popover and keeps the text", async () => {
    const { client } = await renderComposer();
    await open(client, "x @sr");
    key("Escape");
    expect(screen.queryByRole("listbox")).toBeNull();
    expect(area()).toHaveValue("x @sr");
  });

  it("clicking a row picks it", async () => {
    const { client } = await renderComposer();
    await open(client);
    fireEvent.click(screen.getAllByRole("option")[1]!);
    await flush();
    expect(area()).toHaveValue("@src/main.go ");
  });

  it("sends file_refs only for picked mentions still in the text, then forgets them", async () => {
    const { client, sent } = await renderComposer();
    await open(client, "@mai");
    key("Enter");
    await flush();
    type("@src/main.go and @README.md typed by hand");
    await flush();
    fireEvent.click(screen.getByTestId("chat-send-button"));
    await waitFor(() => expect(sent).toHaveLength(1));
    expect(sent[0]!.attachments).toEqual([{ kind: "file_ref", path: "src/main.go" }]);
    expect(sent[0]!.previews).toEqual([{ id: "1", kind: "file_ref", path: "src/main.go" }]);
    expect(useComposerAttachmentsStore.getState().mentions).toEqual({});
    // The next prompt mentions it by hand: nothing remembered, nothing sent.
    type("again @src/main.go");
    await flush();
    fireEvent.click(screen.getByTestId("chat-send-button"));
    await waitFor(() => expect(sent).toHaveLength(2));
    expect(sent[1]!.attachments).toBeUndefined();
  });

  it("a mention deleted from the text is not sent", async () => {
    const { client, sent } = await renderComposer();
    await open(client, "@mai");
    key("Enter");
    await flush();
    type("never mind");
    await flush();
    fireEvent.click(screen.getByTestId("chat-send-button"));
    await waitFor(() => expect(sent).toHaveLength(1));
    expect(sent[0]!.attachments).toBeUndefined();
  });

  it("a daemon without file_ref in its kinds never opens the popover", async () => {
    const { client } = await renderComposer({ ...LIMITS, kinds: ["image", "file"] });
    type("@");
    await flush();
    expect(screen.queryByRole("listbox")).toBeNull();
    expect(client.calls.filter((c) => c.method === "task.searchIndex")).toHaveLength(0);
  });
});
