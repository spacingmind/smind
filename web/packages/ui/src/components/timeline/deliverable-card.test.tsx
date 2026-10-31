import { act } from "react";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const h = vi.hoisted(() => ({ desktop: false, openPath: vi.fn(() => Promise.resolve()) }));
vi.mock("@/lib/platform", async (orig) => ({
  ...(await orig<typeof import("@/lib/platform")>()),
  get isDesktop() {
    return h.desktop;
  },
  desktop: { openPathDefault: h.openPath },
}));

import { DeliverableCards, resetDeliverableCache, type DeliverableContext } from "@/components/timeline/deliverable-card";
import { RunTimeline } from "@/components/timeline/run-timeline";
import type { LocalPathActions } from "@/hooks/use-local-path-actions";
import type { RunEntry } from "@/hooks/use-run-timeline";
import type { FileStatResult } from "@/lib/types";
import { RpcError } from "@/lib/ws-client";
import { FakeWsClient, type RecordedCall } from "@/test/fake-ws-client";

const WT = "/home/u/wt";
const STARTED = "2026-10-31T10:00:00Z";

function run(items: RunEntry["items"], over: Partial<RunEntry> = {}): RunEntry {
  return { id: "run-1", provider: "claude-native", prompt: "make files", status: "done", startedAt: STARTED, items, permissionMode: "", autoAccept: false, ...over } as RunEntry;
}
const write = (path: string, n: number) => ({ kind: "tool_call" as const, id: `t${n}`, toolCallId: `c${n}`, toolName: "Write", status: "success" as const, input: { file_path: path } });

function stat(over: Partial<FileStatResult> = {}): FileStatResult {
  return { size: 240 * 1024, mtime: "2026-10-31T10:05:00Z", kind: "pdf", mime: "application/pdf", ext: ".pdf", mismatch: false, previewable: true, ...over };
}

/** Answers every file.stat the cards ask, by path. */
async function answer(client: FakeWsClient, byPath: Record<string, FileStatResult | RpcError>, expected: number): Promise<RecordedCall[]> {
  await waitFor(() => expect(client.calls.filter((c) => c.method === "file.stat")).toHaveLength(expected));
  const calls = client.calls.filter((c) => c.method === "file.stat");
  await act(async () => {
    for (const c of calls) {
      const r = byPath[(c.params as { path: string }).path];
      if (r instanceof RpcError) c.reject(r);
      else c.resolve(r);
    }
  });
  return calls;
}

const localActions: LocalPathActions = { revealLabel: "Reveal in File Explorer", editors: [], reveal: vi.fn(), open: vi.fn() };
function ctx(client: FakeWsClient, over: Partial<DeliverableContext> = {}): DeliverableContext {
  return { client, taskId: 7, pathActions: null, ...over };
}

beforeEach(() => {
  resetDeliverableCache();
  h.desktop = false;
  h.openPath.mockClear();
  (localActions.reveal as ReturnType<typeof vi.fn>).mockClear();
  localStorage.clear();
});

describe("deliverable cards", () => {
  it("verification_drops_old_missing_mismatch_dir", async () => {
    const client = new FakeWsClient();
    const items = [
      write("out/fresh.pdf", 1),
      write("out/old.pdf", 2),
      write("out/missing.pdf", 3),
      write("out/fake.pdf", 4),
      write("out/dir.pdf", 5),
      write("out/boundary.pdf", 6),
      // never even asked about: outside the worktree or not a worktree path at all
      write("../x.pdf", 7),
      write("~/x.pdf", 8),
      write("https://example.com/x.pdf", 9),
      write("/etc/outside.pdf", 10),
      write("out/run.exe", 11), // extension not on the list
    ];
    const r = run(items);
    const { rerender } = render(<DeliverableCards run={r} ctx={ctx(client)} worktreePath={WT} />);
    const calls = await answer(
      client,
      {
        "out/fresh.pdf": stat(),
        "out/old.pdf": stat({ mtime: "2026-10-31T09:00:00Z" }),
        "out/missing.pdf": new RpcError("not found", "not_found"),
        "out/fake.pdf": stat({ mismatch: true, previewable: false, kind: "text" }),
        "out/dir.pdf": new RpcError("not a regular file", "invalid_params"),
        "out/boundary.pdf": stat({ mtime: STARTED }),
      },
      6,
    );
    expect(calls.map((c) => (c.params as { path: string }).path)).toEqual(["out/fresh.pdf", "out/old.pdf", "out/missing.pdf", "out/fake.pdf", "out/dir.pdf", "out/boundary.pdf"]);
    expect(calls.every((c) => (c.params as { taskId: number }).taskId === 7)).toBe(true);
    await waitFor(() => expect(screen.getAllByTestId("deliverable-card")).toHaveLength(2));
    expect(screen.getAllByTestId("deliverable-card").map((c) => c.getAttribute("data-path"))).toEqual(["out/fresh.pdf", "out/boundary.pdf"]);

    // one file.stat per candidate per run: a re-render with the same transcript asks nothing more
    rerender(<DeliverableCards run={{ ...r }} ctx={ctx(client)} worktreePath={WT} />);
    await act(async () => {
      await Promise.resolve();
    });
    expect(client.calls.filter((c) => c.method === "file.stat")).toHaveLength(6);
  });

  it("caps_cards_and_summarises_the_rest", async () => {
    const client = new FakeWsClient();
    const items = Array.from({ length: 9 }, (_, i) => write(`out/f${i}.pdf`, i));
    const onShow = vi.fn();
    render(<DeliverableCards run={run(items)} ctx={ctx(client, { onShowFiles: onShow })} worktreePath={WT} />);
    await answer(client, Object.fromEntries(items.map((_, i) => [`out/f${i}.pdf`, stat()])), 9);
    await waitFor(() => expect(screen.getAllByTestId("deliverable-card")).toHaveLength(6));
    const more = screen.getByTestId("deliverable-more");
    expect(more).toHaveTextContent("+ 3 more files");
    fireEvent.click(more);
    expect(onShow).toHaveBeenCalledTimes(1);

    // 25 candidates: only 20 are checked
    resetDeliverableCache();
    const c2 = new FakeWsClient();
    const many = Array.from({ length: 25 }, (_, i) => write(`m/f${i}.pdf`, i));
    render(<DeliverableCards run={run(many, { id: "run-2" })} ctx={ctx(c2)} worktreePath={WT} />);
    await waitFor(() => expect(c2.calls.filter((c) => c.method === "file.stat")).toHaveLength(20));
  });

  it("a_card_shows_icon_name_type_size_and_the_path_as_tooltip", async () => {
    const client = new FakeWsClient();
    render(<DeliverableCards run={run([write("reports/Q3 summary.pdf", 1)])} ctx={ctx(client)} worktreePath={WT} />);
    await answer(client, { "reports/Q3 summary.pdf": stat() }, 1);
    const card = await screen.findByTestId("deliverable-card");
    expect(within(card).getByTestId("deliverable-name")).toHaveTextContent("Q3 summary.pdf");
    expect(within(card).getByTestId("deliverable-name")).toHaveAttribute("title", "reports/Q3 summary.pdf");
    expect(within(card).getByTestId("deliverable-meta")).toHaveTextContent("PDF · 240 KB");
  });

  it("no_auto_open", async () => {
    const client = new FakeWsClient();
    const onOpenFile = vi.fn();
    render(<DeliverableCards run={run([write("out/a.pdf", 1)])} ctx={ctx(client)} worktreePath={WT} onOpenFile={onOpenFile} />);
    await answer(client, { "out/a.pdf": stat() }, 1);
    const card = await screen.findByTestId("deliverable-card");
    await act(async () => {
      await new Promise((r) => setTimeout(r, 20));
    });
    expect(onOpenFile).not.toHaveBeenCalled(); // showing a card never opens a tab
    expect(h.openPath).not.toHaveBeenCalled();
    fireEvent.click(within(card).getByRole("button", { name: "Open" }));
    expect(onOpenFile).toHaveBeenCalledTimes(1);
    expect(onOpenFile).toHaveBeenCalledWith("out/a.pdf");
  });

  it("actions_per_platform_and_type", async () => {
    const names = (card: HTMLElement) => within(card).getAllByRole("button").map((b) => b.textContent);

    // web: PDF -> Open, Download, Copy path; docx has no in-app view -> Download, Copy path (no Open)
    let client = new FakeWsClient();
    const items = [write("out/a.pdf", 1), write("out/b.docx", 2), write("notes/c.md", 3), write("pic/d.png", 4)];
    const { unmount } = render(<DeliverableCards run={run(items)} ctx={ctx(client)} worktreePath={WT} onOpenFile={() => undefined} />);
    await answer(client, { "out/a.pdf": stat(), "out/b.docx": stat({ kind: "office", ext: ".docx", previewable: false }), "notes/c.md": stat({ kind: "text", ext: ".md" }), "pic/d.png": stat({ kind: "image", ext: ".png" }) }, 4);
    const web = await screen.findAllByTestId("deliverable-card");
    expect(web).toHaveLength(4);
    expect(names(web[0])).toEqual(["Open", "Download", "Copy path"]);
    expect(names(web[1])).toEqual(["Download", "Copy path"]);
    expect(names(web[2])).toEqual(["Open", "Download", "Copy path"]);
    expect(names(web[3])).toEqual(["Open", "Download", "Copy path"]);
    unmount();

    // desktop, local daemon: Open, Open in app, Reveal, Copy path (no Download); docx: Open in app, Reveal, Copy path
    resetDeliverableCache();
    h.desktop = true;
    client = new FakeWsClient();
    render(<DeliverableCards run={run(items, { id: "run-d" })} ctx={ctx(client, { pathActions: localActions })} worktreePath={WT} onOpenFile={() => undefined} />);
    await answer(client, { "out/a.pdf": stat(), "out/b.docx": stat({ kind: "office", ext: ".docx", previewable: false }), "notes/c.md": stat({ kind: "text", ext: ".md" }), "pic/d.png": stat({ kind: "image", ext: ".png" }) }, 4);
    const local = await screen.findAllByTestId("deliverable-card");
    expect(names(local[0])).toEqual(["Open", "Open in app", "Reveal in File Explorer", "Copy path"]);
    expect(names(local[1])).toEqual(["Open in app", "Reveal in File Explorer", "Copy path"]);
    fireEvent.click(within(local[1]).getByRole("button", { name: "Open in app" }));
    expect(h.openPath).toHaveBeenCalledWith(WT, "out/b.docx");
    fireEvent.click(within(local[0]).getByRole("button", { name: "Reveal in File Explorer" }));
    expect(localActions.reveal).toHaveBeenCalledWith(`${WT}/out/a.pdf`);
  });

  it("desktop_attached_to_a_remote_daemon_offers_neither_open_in_app_nor_reveal", async () => {
    h.desktop = true;
    const client = new FakeWsClient();
    render(<DeliverableCards run={run([write("out/b.docx", 1), write("out/a.pdf", 2)])} ctx={ctx(client, { pathActions: null })} worktreePath={WT} onOpenFile={() => undefined} />);
    await answer(client, { "out/b.docx": stat({ kind: "office", ext: ".docx", previewable: false }), "out/a.pdf": stat() }, 2);
    const cards = await screen.findAllByTestId("deliverable-card");
    expect(within(cards[0]).getAllByRole("button").map((b) => b.textContent)).toEqual(["Copy path"]);
    expect(within(cards[1]).getAllByRole("button").map((b) => b.textContent)).toEqual(["Open", "Copy path"]);
  });

  it("both_modes", async () => {
    // cards are not part of the interface mode: the same transcript renders the same cards whatever mode is stored
    const html: string[] = [];
    for (const mode of ["coding", "office", "garbage"]) {
      resetDeliverableCache();
      localStorage.setItem("smind:interface-mode", mode);
      const client = new FakeWsClient();
      const { container, unmount } = render(<DeliverableCards run={run([write("out/a.pdf", 1)])} ctx={ctx(client)} worktreePath={WT} onOpenFile={() => undefined} />);
      await answer(client, { "out/a.pdf": stat() }, 1);
      await screen.findByTestId("deliverable-card");
      html.push(container.innerHTML);
      unmount();
    }
    expect(html[0]).toBe(html[1]);
    expect(html[1]).toBe(html[2]);
    expect(html[0]).toContain("deliverable-card");
  });

  it("renders_nothing_without_a_context_a_client_or_a_qualifying_file", async () => {
    const client = new FakeWsClient();
    const a = render(<DeliverableCards run={run([write("out/a.pdf", 1)])} worktreePath={WT} />);
    expect(a.container.innerHTML).toBe("");
    expect(client.calls).toHaveLength(0);
    a.unmount();
    const b = render(<DeliverableCards run={run([write("out/a.pdf", 1)])} ctx={{ client: null, taskId: 1, pathActions: null }} worktreePath={WT} />);
    expect(b.container.innerHTML).toBe("");
    b.unmount();
    const c = render(<DeliverableCards run={run([{ kind: "assistant", id: "a", text: "nothing produced" }])} ctx={ctx(client)} worktreePath={WT} />);
    expect(c.container.innerHTML).toBe("");
    expect(client.calls).toHaveLength(0);
  });

  it("a_run_still_running_checks_tool_paths_and_waits_for_the_final_message", async () => {
    const client = new FakeWsClient();
    const items = [write("out/a.pdf", 1), { kind: "assistant" as const, id: "m", text: "Also see `out/b.pdf`" }];
    const { rerender } = render(<DeliverableCards run={run(items, { status: "running" })} ctx={ctx(client)} worktreePath={WT} />);
    await answer(client, { "out/a.pdf": stat() }, 1);
    await screen.findByTestId("deliverable-card");
    expect(client.calls.filter((c) => c.method === "file.stat")).toHaveLength(1);
    // the run ends: the message's path is now a candidate, the first one is not asked again
    rerender(<DeliverableCards run={run(items, { status: "done" })} ctx={ctx(client)} worktreePath={WT} />);
    await waitFor(() => expect(client.calls.filter((c) => c.method === "file.stat")).toHaveLength(2));
    expect((client.calls.filter((c) => c.method === "file.stat")[1].params as { path: string }).path).toBe("out/b.pdf");
  });

  it("a_transport_failure_is_retried_but_a_daemon_no_is_final", async () => {
    const client = new FakeWsClient();
    const r = run([write("out/a.pdf", 1)]);
    const { rerender } = render(<DeliverableCards run={r} ctx={ctx(client)} worktreePath={WT} />);
    await waitFor(() => expect(client.calls).toHaveLength(1));
    await act(async () => client.calls[0].reject(new Error("socket closed")));
    // a new transcript object with a new item identity asks again (the failed answer was not cached)
    rerender(<DeliverableCards run={run([write("out/a.pdf", 1)])} ctx={ctx(client)} worktreePath={WT} />);
    await waitFor(() => expect(client.calls).toHaveLength(2));
    await act(async () => client.calls[1].reject(new RpcError("not found", "not_found")));
    rerender(<DeliverableCards run={run([write("out/a.pdf", 1)])} ctx={ctx(client)} worktreePath={WT} />);
    await act(async () => {
      await Promise.resolve();
    });
    expect(client.calls).toHaveLength(2); // the daemon said no: that stands for this run
  });

  it("cards_render_under_the_answer_in_the_run_timeline", async () => {
    const client = new FakeWsClient();
    const items = [write("out/a.pdf", 1), { kind: "assistant" as const, id: "m", text: "Here is the report." }];
    render(
      <ul>
        <RunTimeline run={run(items)} worktreePath={WT} onOpenFile={() => undefined} deliverables={ctx(client)} />
      </ul>,
    );
    await answer(client, { "out/a.pdf": stat() }, 1);
    const cards = await screen.findByTestId("deliverable-cards");
    const log = screen.getByTestId("run-timeline");
    // after the transcript, before the footer
    expect(log.compareDocumentPosition(cards) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });
});
