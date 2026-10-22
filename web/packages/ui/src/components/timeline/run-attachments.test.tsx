import { act } from "react";
import { cleanup, fireEvent, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { QueuedPrompts } from "@/components/composer/queued-prompts";
import { TaskDetailPane } from "@/components/task-detail";
import { RunAttachments } from "@/components/timeline/run-attachments";
import type { AttachmentMeta, Chat, ChatQueueItem, Task } from "@/lib/types";
import { FakeWsClient } from "@/test/fake-ws-client";
import { renderWithClientProp as render } from "@/test/render-with-daemon";

async function flush(): Promise<void> {
  await act(async () => {
    await new Promise((r) => setTimeout(r, 0));
  });
}

const image: AttachmentMeta = { id: "1", kind: "image", name: "shot.png", mimeType: "image/png", size: 2048 };
const file: AttachmentMeta = { id: "2", kind: "file", name: "report.pdf", mimeType: "application/pdf", size: 90311 };
const ref: AttachmentMeta = { id: "3", kind: "file_ref", path: "internal/runs/registry.go", isDir: false };
const dir: AttachmentMeta = { id: "4", kind: "file_ref", path: "internal/runs", isDir: true };
const pr: AttachmentMeta = { id: "5", kind: "github_pr", number: 123, title: "fix queue", url: "https://github.com/o/r/pull/123" };
const issue: AttachmentMeta = { id: "6", kind: "github_issue", number: 45, title: "bug" };

let revoked: string[];
beforeEach(() => {
  revoked = [];
  let n = 0;
  vi.stubGlobal("URL", Object.assign(URL, { createObjectURL: () => `blob:thumb-${++n}`, revokeObjectURL: (u: string) => revoked.push(u) }));
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

function Probe(props: { runId?: string; attachments: AttachmentMeta[]; client: FakeWsClient }) {
  return <RunAttachments runId={props.runId ?? "run-1"} attachments={props.attachments} />;
}

describe("RunAttachments", () => {
  it("renders nothing without attachments", () => {
    const client = new FakeWsClient();
    const { container } = render(<Probe attachments={[]} client={client} />);
    expect(container).toBeEmptyDOMElement();
  });

  it("fetches a thumbnail with run.attachment once per id and caches it", async () => {
    const client = new FakeWsClient();
    const view = render(<Probe attachments={[image]} client={client} />);
    // Same-size skeleton while loading.
    expect(screen.getByTestId("run-attachment-skeleton")).toBeInTheDocument();
    expect(client.calls.filter((c) => c.method === "run.attachment")).toHaveLength(1);
    expect(client.nth("run.attachment", 0).params).toEqual({ runId: "run-1", id: "1" });
    client.nth("run.attachment", 0).resolve({ name: "shot.png", mimeType: "image/png", size: 4, data: "AAAAAA==" });
    await flush();
    expect(screen.getByTestId("run-attachment-image")).toHaveAttribute("src", "blob:thumb-1");
    expect(screen.queryByTestId("run-attachment-skeleton")).toBeNull();
    // Unmount the thumbnail (revokes its URL) and mount it again: the bytes come from the cache, not the daemon.
    view.rerender(<Probe attachments={[]} client={client} />);
    expect(revoked).toContain("blob:thumb-1");
    view.rerender(<Probe attachments={[image]} client={client} />);
    await flush();
    expect(screen.getByTestId("run-attachment-image")).toBeInTheDocument();
    expect(client.calls.filter((c) => c.method === "run.attachment")).toHaveLength(1);
  });

  it("keeps the local preview and never fetches for an id that has one", () => {
    const client = new FakeWsClient();
    render(<Probe attachments={[{ ...image, previewUrl: "blob:local" }]} client={client} />);
    expect(screen.getByTestId("run-attachment-image")).toHaveAttribute("src", "blob:local");
    expect(client.calls.some((c) => c.method === "run.attachment")).toBe(false);
  });

  it("a failed fetch shows a same-size placeholder, not a broken image", async () => {
    const client = new FakeWsClient();
    render(<Probe attachments={[image]} client={client} />);
    client.nth("run.attachment", 0).reject(new Error("gone"));
    await flush();
    expect(screen.getByTestId("run-attachment-failed")).toHaveTextContent("shot.png");
  });

  it("fetches only when the thumbnail is on screen", async () => {
    let callback: IntersectionObserverCallback = () => {};
    vi.stubGlobal(
      "IntersectionObserver",
      class {
        constructor(cb: IntersectionObserverCallback) {
          callback = cb;
        }
        observe() {}
        disconnect() {}
      },
    );
    const client = new FakeWsClient();
    render(<Probe attachments={[image]} client={client} />);
    await flush();
    expect(client.calls.some((c) => c.method === "run.attachment")).toBe(false);
    await act(async () => callback([{ isIntersecting: true } as IntersectionObserverEntry], {} as IntersectionObserver));
    expect(client.calls.filter((c) => c.method === "run.attachment")).toHaveLength(1);
  });

  it("renders chips for file, file_ref, github items; only a github chip with a url is a link", () => {
    const client = new FakeWsClient();
    render(<Probe attachments={[file, ref, dir, pr, issue]} client={client} />);
    const chips = screen.getAllByTestId("attachment-chip");
    expect(chips.map((c) => c.textContent)).toEqual(["report.pdf88 KiB", "internal/runs/registry.go", "internal/runs", "#123 fix queue", "#45 bug"]);
    expect(chips[3].tagName).toBe("A");
    expect(chips[3]).toHaveAttribute("href", "https://github.com/o/r/pull/123");
    expect(chips[1].tagName).toBe("SPAN");
    expect(chips[4].tagName).toBe("SPAN");
  });

  it("opens the image in a lightbox and closes it with Escape", () => {
    const client = new FakeWsClient();
    render(<Probe attachments={[{ ...image, previewUrl: "blob:local" }]} client={client} />);
    fireEvent.click(screen.getByRole("button", { name: "Preview shot.png" }));
    const box = screen.getByTestId("attachment-lightbox");
    expect(box.querySelector("img")).toHaveAttribute("src", "blob:local");
    fireEvent.keyDown(box, { key: "Escape" });
    expect(screen.queryByTestId("attachment-lightbox")).toBeNull();
  });
});

describe("queued rows", () => {
  it("show name-only chips from the queue item's attachments", () => {
    const item: ChatQueueItem = {
      ID: 1, ChatID: 10, Prompt: "later", RunConfig: "{}", Source: "user", FromTaskID: null, FromChatID: null, Priority: 0, Status: "queued", RunID: null,
      CancelReason: "", CancelKind: "", CreatedAt: "", UpdatedAt: "", attachments: [image, file],
    };
    render(<QueuedPrompts items={[item]} onCancel={() => {}} />);
    const chips = screen.getAllByTestId("attachment-chip");
    expect(chips.map((c) => c.textContent)).toEqual(["shot.png", "report.pdf"]);
    expect(screen.queryByRole("img")).toBeNull();
  });
});

describe("reload", () => {
  const TASK: Task = { ID: 1, WorkspaceID: 1, SpaceID: null, Title: "T", Status: "active", WorktreePath: "/tmp/a", Branch: "t", CreatedAt: "2024-01-01T00:00:00Z", UpdatedAt: "2024-01-01T00:00:00Z", ArchivedAt: null };
  const CHAT: Chat = { ID: 10, TaskID: 1, Title: "Chat", Provider: null, AgentSession: null, CreatedAt: "2024-01-01T00:00:00Z", ArchivedAt: null };

  it("shows a run's attachments from run.list metadata and loads the thumbnail lazily", async () => {
    const client = new FakeWsClient();
    render(<TaskDetailPane client={client} task={TASK} chat={CHAT} isDefaultChat />);
    client.nth("run.list", 0).resolve([
      {
        ID: "run-1", TaskID: 1, ChatID: 10, Provider: "claude-native", Prompt: "see these", Status: "done", StartedAt: "2024-01-01T00:00:00Z",
        FinishedAt: "2024-01-01T00:00:05Z", StopReason: "end_turn", Err: "", PermissionMode: "", AutoAccept: false, ThinkingLevel: "",
        attachments: [image, file, pr],
      },
    ]);
    await flush();
    expect(screen.getByTestId("run-attachments")).toBeInTheDocument();
    expect(screen.getAllByTestId("attachment-chip")).toHaveLength(2);
    expect(client.nth("run.attachment", 0).params).toEqual({ runId: "run-1", id: "1" });
  });
});
