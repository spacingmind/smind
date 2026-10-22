import { act } from "react";
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { Composer } from "@/components/composer/composer";
import type { AttachmentLimits, AttachmentRequest } from "@/lib/types";
import { RpcError } from "@/lib/ws-client";
import { useComposerAttachmentsStore } from "@/stores/composer-attachments-store";
import { FakeWsClient } from "@/test/fake-ws-client";
import { renderWithClientProp as render } from "@/test/render-with-daemon";

const LIMITS: AttachmentLimits = {
  maxCount: 10,
  maxImageBytes: 1 << 20,
  maxFileBytes: 1 << 20,
  maxTotalBytes: 1 << 20,
  maxMessageBytes: 16 << 20,
  images: ["image/png"],
  kinds: ["image", "file", "file_ref", "pr", "issue"],
};
const PR = { number: 123, title: "Fix the thing", state: "open", author: "ana", url: "https://github.com/o/r/pull/123", updatedAt: new Date().toISOString() };

async function flush() {
  await act(async () => {
    await new Promise((r) => setTimeout(r, 0));
  });
}

async function renderComposer(info: object = { attachments: LIMITS }, maxCount?: number) {
  const client = new FakeWsClient();
  const sent: { attachments?: AttachmentRequest[]; previews?: unknown }[] = [];
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
      onSubmit={async (_p, _t, _perm, _th, _w, _m, attachments, previews) => {
        sent.push({ attachments, previews });
        return { kind: "started", runId: "r" };
      }}
      onStop={async () => {}}
    />,
  );
  const withCap = maxCount ? { ...info, attachments: { ...LIMITS, maxCount } } : info;
  client.nth("daemon.info", 0).resolve(withCap);
  await flush();
  return { client, sent };
}

const openMenu = () => fireEvent.pointerDown(screen.getByRole("button", { name: "Attach" }), { button: 0 });
const searches = (c: FakeWsClient) => c.calls.filter((x) => x.method === "forge.search");

async function openPicker(client: FakeWsClient, name = "GitHub pull request…") {
  openMenu();
  fireEvent.click(await screen.findByRole("menuitem", { name: new RegExp(name) }));
  // The picker opens on the frame after the menu has closed (see composer.tsx pendingPicker).
  await screen.findByTestId("forge-picker");
  await flush();
  return client;
}

beforeEach(() => {
  useComposerAttachmentsStore.setState({ byKey: {}, mentions: {} });
  localStorage.clear();
});
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("GitHub picker", () => {
  it("hides the menu items when the daemon does not list pr/issue", async () => {
    await renderComposer({ attachments: { ...LIMITS, kinds: undefined } });
    openMenu();
    await screen.findByRole("menu");
    expect(screen.queryByRole("menuitem", { name: /GitHub/ })).toBeNull();
  });

  it("shows the items disabled with the reason when gh is missing", async () => {
    await renderComposer({ attachments: LIMITS, forge: { provider: "github", cli: false } });
    openMenu();
    const item = await screen.findByRole("menuitem", { name: /GitHub pull request/ });
    expect(item).toHaveAttribute("aria-disabled", "true");
    expect(item).toHaveTextContent("gh is not installed on the daemon host");
    expect(screen.getByRole("menuitem", { name: /GitHub issue/ })).toHaveAttribute("aria-disabled", "true");
  });

  it("searches on open and once per settled query with the right params", async () => {
    const { client } = await renderComposer();
    await openPicker(client);
    expect(searches(client)).toHaveLength(1);
    expect(searches(client)[0]!.params).toEqual({ forge: "github", taskId: 1, kind: "pr", query: "", limit: 30 });
    const field = screen.getByRole("combobox", { name: "Search pull requests" });
    fireEvent.change(field, { target: { value: "f" } });
    fireEvent.change(field, { target: { value: "fi" } });
    fireEvent.change(field, { target: { value: "fix" } });
    expect(searches(client)).toHaveLength(1);
    await waitFor(() => expect(searches(client)).toHaveLength(2), { timeout: 1500 });
    expect(searches(client)[1]!.params).toMatchObject({ query: "fix", kind: "pr" });
  });

  it("shows loading, rows, empty and error states", async () => {
    const { client } = await renderComposer();
    await openPicker(client);
    expect(screen.getByText("Searching…")).toBeInTheDocument();
    searches(client)[0]!.resolve({ forge: "github", repo: "o/r", items: [PR] });
    await flush();
    expect(screen.getByText("#123 Fix the thing")).toBeInTheDocument();
    expect(screen.getByText(/ana · open · just now/)).toBeInTheDocument();
    const field = screen.getByRole("combobox", { name: "Search pull requests" });
    fireEvent.change(field, { target: { value: "zz" } });
    await waitFor(() => expect(searches(client)).toHaveLength(2), { timeout: 1500 });
    searches(client)[1]!.resolve({ forge: "github", repo: "o/r", items: [] });
    await flush();
    expect(screen.getByText("No matching pull requests")).toBeInTheDocument();
  });

  it("says No open issues for an empty issue list", async () => {
    const { client } = await renderComposer();
    await openPicker(client, "GitHub issue");
    searches(client)[0]!.resolve({ forge: "github", repo: "o/r", items: [] });
    await flush();
    expect(screen.getByText("No open issues")).toBeInTheDocument();
  });

  it.each([
    ["forge_cli_missing", /gh is not installed/],
    ["forge_unauthenticated", /gh is not logged in on the daemon host/],
  ])("maps %s to a clear one-line callout", async (code, text) => {
    const { client } = await renderComposer();
    await openPicker(client);
    searches(client)[0]!.reject(new RpcError("raw", code));
    await flush();
    expect(await screen.findByRole("alert")).toHaveTextContent(text);
  });

  it("selecting adds a pill, closes the picker, and submit sends {kind:'pr', forge, number, repo}", async () => {
    const { client, sent } = await renderComposer();
    await openPicker(client);
    searches(client)[0]!.resolve({ forge: "github", repo: "o/r", items: [PR] });
    await flush();
    fireEvent.click(screen.getByRole("option", { name: /Fix the thing/ }));
    await flush();
    expect(screen.queryByTestId("forge-picker")).toBeNull();
    expect(screen.getByText("PR · o/r")).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Prompt"), { target: { value: "review" } });
    fireEvent.click(screen.getByTestId("chat-send-button"));
    await waitFor(() => expect(sent).toHaveLength(1));
    expect(sent[0]!.attachments).toEqual([{ kind: "pr", forge: "github", number: 123, repo: "o/r" }]);
    expect(sent[0]!.previews).toEqual([expect.objectContaining({ id: "1", kind: "pr", number: 123, repo: "o/r", title: "Fix the thing", url: PR.url })]);
  });

  it("Enter picks the highlighted row", async () => {
    const { client } = await renderComposer();
    await openPicker(client);
    searches(client)[0]!.resolve({ forge: "github", repo: "o/r", items: [PR, { ...PR, number: 124, title: "Second" }] });
    await flush();
    const field = screen.getByRole("combobox", { name: "Search pull requests" });
    fireEvent.keyDown(field, { key: "ArrowDown" });
    fireEvent.keyDown(field, { key: "Enter" });
    await flush();
    expect(screen.getByText("#124 Second")).toBeInTheDocument();
  });

  it("the pill opens its url and can be removed", async () => {
    const open = vi.spyOn(window, "open").mockReturnValue(null);
    const { client } = await renderComposer();
    await openPicker(client);
    searches(client)[0]!.resolve({ forge: "github", repo: "o/r", items: [PR] });
    await flush();
    fireEvent.click(screen.getByRole("option", { name: /Fix the thing/ }));
    await flush();
    fireEvent.click(screen.getByText("#123 Fix the thing"));
    expect(open).toHaveBeenCalledWith(PR.url, "_blank", "noopener,noreferrer");
    fireEvent.click(screen.getByRole("button", { name: "Remove #123 Fix the thing" }));
    expect(screen.queryByTestId("attachment-tray")).toBeNull();
  });

  it("a PR counts toward maxCount", async () => {
    const { client } = await renderComposer({ attachments: LIMITS }, 1);
    await openPicker(client);
    searches(client)[0]!.resolve({ forge: "github", repo: "o/r", items: [PR] });
    await flush();
    fireEvent.click(screen.getByRole("option", { name: /Fix the thing/ }));
    await flush();
    fireEvent.change(screen.getByTestId("composer-attach-input"), { target: { files: [new File(["x"], "a.txt", { type: "text/plain" })] } });
    expect(screen.getByText("Over the 1 attachment limit")).toBeInTheDocument();
  });
});
