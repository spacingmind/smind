import { act } from "react";
import { cleanup, fireEvent, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { Composer } from "@/components/composer/composer";
import type { AttachmentLimits } from "@/lib/types";
import { useComposerAttachmentsStore } from "@/stores/composer-attachments-store";
import { FakeWsClient } from "@/test/fake-ws-client";
import { renderWithClientProp as render } from "@/test/render-with-daemon";

const LIMITS: AttachmentLimits = { maxCount: 10, maxImageBytes: 1 << 20, maxFileBytes: 1 << 20, maxTotalBytes: 1 << 20, maxMessageBytes: 16 << 20, images: ["image/png"] };

async function flush() {
  await act(async () => {
    await new Promise((r) => setTimeout(r, 0));
  });
}

async function renderComposer(info: object = { attachments: LIMITS }) {
  const client = new FakeWsClient();
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
      onSubmit={async () => ({ kind: "started", runId: "r" })}
      onStop={async () => {}}
    />,
  );
  client.nth("daemon.info", 0).resolve(info);
  await flush();
  return client;
}

const openMenu = () => fireEvent.pointerDown(screen.getByRole("button", { name: "Attach" }), { button: 0 });

beforeEach(() => {
  useComposerAttachmentsStore.setState({ byKey: {}, mentions: {} });
  localStorage.clear();
});
afterEach(cleanup);

describe("composer attach menu", () => {
  it("Attach opens a menu instead of the picker", async () => {
    await renderComposer();
    const click = vi.spyOn(screen.getByTestId("composer-attach-input") as HTMLInputElement, "click");
    expect(screen.queryByRole("menu")).toBeNull();
    openMenu();
    expect(await screen.findByRole("menu")).toBeInTheDocument();
    expect(click).not.toHaveBeenCalled();
    expect(screen.getByRole("menuitem", { name: "Image or file…" })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: "Mention a file" })).toBeInTheDocument();
  });

  it("Image or file… opens the picker input", async () => {
    await renderComposer();
    const click = vi.spyOn(screen.getByTestId("composer-attach-input") as HTMLInputElement, "click");
    openMenu();
    fireEvent.click(await screen.findByRole("menuitem", { name: "Image or file…" }));
    expect(click).toHaveBeenCalledTimes(1);
  });

  it("Mention a file inserts @ at the caret, focuses the textarea and opens the popover", async () => {
    const client = await renderComposer();
    const area = screen.getByLabelText("Prompt") as HTMLTextAreaElement;
    fireEvent.change(area, { target: { value: "look at" } });
    openMenu();
    fireEvent.click(await screen.findByRole("menuitem", { name: "Mention a file" }));
    await flush();
    expect(area).toHaveValue("look at @");
    expect(area.selectionStart).toBe(9);
    expect(document.activeElement).toBe(area);
    client.nth("task.searchIndex", 0).resolve({ paths: ["a.go"] });
    await flush();
    expect(screen.getByRole("listbox")).toBeInTheDocument();
  });

  it("is keyboard accessible: Enter on the trigger opens it and ArrowDown + Enter picks", async () => {
    await renderComposer();
    const click = vi.spyOn(screen.getByTestId("composer-attach-input") as HTMLInputElement, "click");
    const trigger = screen.getByRole("button", { name: "Attach" });
    trigger.focus();
    fireEvent.keyDown(trigger, { key: "Enter" });
    const item = await screen.findByRole("menuitem", { name: "Image or file…" });
    expect(screen.getByRole("menu")).toBeInTheDocument();
    fireEvent.keyDown(item, { key: "Enter" });
    expect(click).toHaveBeenCalled();
  });

  it("omits Mention a file when the daemon does not list file_ref", async () => {
    await renderComposer({ attachments: { ...LIMITS, kinds: ["image", "file"] } });
    openMenu();
    await screen.findByRole("menu");
    expect(screen.queryByRole("menuitem", { name: "Mention a file" })).toBeNull();
  });
});
