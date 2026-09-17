import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { FakeWsClient } from "@/test/fake-ws-client";
import { requestDiffReveal, resetDiffReveal } from "@/lib/diff-reveal";
import { DIFF_PREFS_STORAGE_KEY } from "@/lib/diff-prefs";
import { getReviewDrafts, resetReviewDrafts } from "@/lib/review-drafts";
import { DiffViewerPane } from "@/components/diff-viewer-pane";
import type { Task, TaskCreatePrResult, TaskFileDiffResult, TaskFilesResult } from "@/lib/types";

const TASK: Task = {
  ID: 1,
  WorkspaceID: 1,
  SpaceID: null,
  Title: "Task A",
  Status: "active",
  WorktreePath: "/tmp/a",
  Branch: "task-a",
  CreatedAt: "2024-01-01T00:00:00Z",
  UpdatedAt: "2024-01-01T00:00:00Z",
  ArchivedAt: null,
};

const SAMPLE_DIFF = `diff --git a/file.txt b/file.txt
index a29bdeb..0226208 100644
--- a/file.txt
+++ b/file.txt
@@ -1 +1,2 @@
 line1
+line2 added
`;

const FILES: TaskFilesResult = {
  files: [
    { path: "file.txt", status: "modified", staged: false },
    { path: "new.txt", status: "added", staged: false },
  ],
};

/** Flushes pending microtasks, wrapped in `act` so React commits any resulting state updates before the caller asserts. */
async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

/** Resolves the initial task.files call and every pending task.fileDiff it triggers. */
async function resolveFilesAndDiffs(
  client: FakeWsClient,
  files: TaskFilesResult = FILES,
  diff: string = SAMPLE_DIFF,
): Promise<void> {
  client.nth("task.files", 0).resolve(files);
  await flush();
  for (let i = 0; ; i++) {
    const matches = client.calls.filter((c) => c.method === "task.fileDiff");
    if (i >= matches.length) break;
    client.nth("task.fileDiff", i).resolve({ diff } satisfies TaskFileDiffResult);
  }
  await flush();
}

describe("DiffViewerPane", () => {
  it("fetches task.files on mount and renders one collapsible entry per file with its own task.fileDiff", async () => {
    const client = new FakeWsClient();
    render(<DiffViewerPane client={client} task={TASK} />);

    expect(client.nth("task.files", 0).params).toEqual({ taskId: TASK.ID });
    await resolveFilesAndDiffs(client);

    expect(screen.getByTestId("diff-file-file.txt")).toBeInTheDocument();
    expect(screen.getByTestId("diff-file-new.txt")).toBeInTheDocument();

    const container = screen.getByTestId("diff-container-file.txt");
    expect(container.textContent).toContain("line2 added");
    expect(screen.queryByTestId("diff-empty")).not.toBeInTheDocument();
    expect(screen.queryByTestId("diff-error")).not.toBeInTheDocument();
  });

  it("collapses and re-expands a file, hiding then re-showing its diff", async () => {
    const client = new FakeWsClient();
    render(<DiffViewerPane client={client} task={TASK} />);
    await resolveFilesAndDiffs(client);

    fireEvent.click(screen.getByTestId("diff-file-header-file.txt"));
    expect(screen.queryByTestId("diff-container-file.txt")).not.toBeInTheDocument();

    fireEvent.click(screen.getByTestId("diff-file-header-file.txt"));
    expect(screen.getByTestId("diff-container-file.txt")).toBeInTheDocument();
  });

  it("shows a clear empty state for a task with no changes", async () => {
    const client = new FakeWsClient();
    render(<DiffViewerPane client={client} task={TASK} />);

    client.nth("task.files", 0).resolve({ files: [] } satisfies TaskFilesResult);
    await flush();

    expect(screen.getByTestId("diff-empty")).toHaveTextContent(/no changes/i);
  });

  it("surfaces a task.files failure as a visible error", async () => {
    const client = new FakeWsClient();
    render(<DiffViewerPane client={client} task={TASK} />);

    client.nth("task.files", 0).reject(new Error("boom"));
    await flush();

    expect(screen.getByTestId("diff-error")).toHaveTextContent("boom");
  });

  it("the Refresh control re-issues task.files", async () => {
    const client = new FakeWsClient();
    render(<DiffViewerPane client={client} task={TASK} />);
    await resolveFilesAndDiffs(client);

    fireEvent.click(screen.getByRole("button", { name: "Refresh" }));
    await flush();
    expect(client.calls.filter((c) => c.method === "task.files")).toHaveLength(2);
  });

  it("disables the Refresh control and issues no request when there is no client", () => {
    render(<DiffViewerPane client={null} task={TASK} />);
    expect(screen.getByRole("button", { name: "Refresh" })).toBeDisabled();
  });
});

describe("DiffViewerPane staging", () => {
  it("stage checkbox calls task.stage and updates the file's staged state", async () => {
    const client = new FakeWsClient();
    render(<DiffViewerPane client={client} task={TASK} />);
    await resolveFilesAndDiffs(client);

    const box = screen.getByTestId("stage-file.txt");
    expect(box).not.toBeChecked();
    // Commit stays disabled: nothing staged yet.
    expect(screen.getByTestId("commit-button")).toBeDisabled();

    fireEvent.click(box);
    const call = client.nth("task.stage", 0);
    expect(call.params).toEqual({ taskId: TASK.ID, path: "file.txt", staged: true });
    call.resolve({});
    await flush();

    expect(screen.getByTestId("stage-file.txt")).toBeChecked();
    expect(screen.getByTestId("commit-button")).toHaveTextContent(/1 staged/);
  });

  it("unchecking a staged file sends staged: false", async () => {
    const client = new FakeWsClient();
    render(<DiffViewerPane client={client} task={TASK} />);
    client.nth("task.files", 0).resolve({
      files: [{ path: "file.txt", status: "modified", staged: true }],
    } satisfies TaskFilesResult);
    await flush();

    fireEvent.click(screen.getByTestId("stage-file.txt"));
    expect(client.nth("task.stage", 0).params).toEqual({
      taskId: TASK.ID,
      path: "file.txt",
      staged: false,
    });
  });

  it("a stage failure renders inline", async () => {
    const client = new FakeWsClient();
    render(<DiffViewerPane client={client} task={TASK} />);
    await resolveFilesAndDiffs(client);

    fireEvent.click(screen.getByTestId("stage-file.txt"));
    client.nth("task.stage", 0).reject(new Error("stage blew up"));
    await flush();

    expect(screen.getByTestId("diff-error")).toHaveTextContent("stage blew up");
    expect(screen.getByTestId("stage-file.txt")).not.toBeChecked();
  });
});

describe("DiffViewerPane commit bar", () => {
  it("Commit stays disabled until at least one file is staged and the message is non-empty", async () => {
    const client = new FakeWsClient();
    render(<DiffViewerPane client={client} task={TASK} />);
    await resolveFilesAndDiffs(client);

    const button = screen.getByTestId("commit-button");
    fireEvent.change(screen.getByTestId("commit-message"), { target: { value: "a message" } });
    expect(button).toBeDisabled();

    fireEvent.click(screen.getByTestId("stage-file.txt"));
    client.nth("task.stage", 0).resolve({});
    await flush();
    expect(button).toBeEnabled();
  });

  it("offers a one-click suggested message once a file is staged, filling the composer without typing", async () => {
    const client = new FakeWsClient();
    render(<DiffViewerPane client={client} task={TASK} />);
    await resolveFilesAndDiffs(client);

    // Nothing staged yet: no suggestion to offer.
    expect(screen.queryByTestId("commit-suggestion")).not.toBeInTheDocument();

    fireEvent.click(screen.getByTestId("stage-file.txt"));
    client.nth("task.stage", 0).resolve({});
    await flush();

    const suggestion = screen.getByTestId("commit-suggestion");
    expect(suggestion).toHaveTextContent("Update file.txt");
    // Filling is a click, and the click both fills and retires the
    // offer -- otherwise it would sit beside an already-filled box
    // offering to duplicate what's there.
    fireEvent.click(suggestion);
    expect(screen.getByTestId("commit-message")).toHaveValue("Update file.txt");
    expect(screen.queryByTestId("commit-suggestion")).not.toBeInTheDocument();
    expect(screen.getByTestId("commit-button")).toBeEnabled();
  });

  it("keeps the suggestion out of the way once the user has typed a message", async () => {
    const client = new FakeWsClient();
    render(<DiffViewerPane client={client} task={TASK} />);
    await resolveFilesAndDiffs(client);

    fireEvent.click(screen.getByTestId("stage-file.txt"));
    client.nth("task.stage", 0).resolve({});
    await flush();

    fireEvent.change(screen.getByTestId("commit-message"), { target: { value: "my own words" } });
    expect(screen.queryByTestId("commit-suggestion")).not.toBeInTheDocument();
  });

  it("a successful commit shows sha+subject and refreshes the files list", async () => {
    const client = new FakeWsClient();
    render(<DiffViewerPane client={client} task={TASK} />);
    await resolveFilesAndDiffs(client);

    fireEvent.click(screen.getByTestId("stage-file.txt"));
    client.nth("task.stage", 0).resolve({});
    await flush();
    fireEvent.change(screen.getByTestId("commit-message"), { target: { value: "my commit" } });
    fireEvent.click(screen.getByTestId("commit-button"));

    const call = client.nth("task.commit", 0);
    expect(call.params).toEqual({ taskId: TASK.ID, message: "my commit", author: "human" });
    call.resolve({ commit: "abcdef1234567890", subject: "my commit", files: 1 });
    await flush();

    expect(screen.getByTestId("commit-success")).toHaveTextContent("my commit");
    expect(screen.getByTestId("commit-success")).toHaveTextContent("abcdef12");
    // Post-commit refresh of the files list.
    expect(client.calls.filter((c) => c.method === "task.files")).toHaveLength(2);
  });

  it("a commit failure renders inline and does not refresh", async () => {
    const client = new FakeWsClient();
    render(<DiffViewerPane client={client} task={TASK} />);
    await resolveFilesAndDiffs(client);

    fireEvent.click(screen.getByTestId("stage-file.txt"));
    client.nth("task.stage", 0).resolve({});
    await flush();
    fireEvent.change(screen.getByTestId("commit-message"), { target: { value: "my commit" } });
    fireEvent.click(screen.getByTestId("commit-button"));
    client.nth("task.commit", 0).reject(new Error("nothing staged to commit"));
    await flush();

    expect(screen.getByTestId("commit-error")).toHaveTextContent("nothing staged to commit");
    expect(client.calls.filter((c) => c.method === "task.files")).toHaveLength(1);
  });
});

describe("DiffViewerPane Create PR", () => {
  it("calls task.createPr and renders the returned URL as a link", async () => {
    const client = new FakeWsClient();
    render(<DiffViewerPane client={client} task={TASK} />);
    await resolveFilesAndDiffs(client);

    fireEvent.click(screen.getByTestId("create-pr-button"));
    const call = client.nth("task.createPr", 0);
    expect(call.params).toEqual({ taskId: TASK.ID });
    call.resolve({ url: "https://github.com/example/repo/pull/42" } satisfies TaskCreatePrResult);
    await flush();

    const link = screen.getByTestId("pr-url-link");
    expect(link).toHaveTextContent("https://github.com/example/repo/pull/42");
    expect(link).toHaveAttribute("href", "https://github.com/example/repo/pull/42");
    expect(screen.queryByTestId("pr-error")).not.toBeInTheDocument();
  });

  it("surfaces a task.createPr failure inline instead of failing silently", async () => {
    const client = new FakeWsClient();
    render(<DiffViewerPane client={client} task={TASK} />);
    await resolveFilesAndDiffs(client);

    fireEvent.click(screen.getByTestId("create-pr-button"));
    client.nth("task.createPr", 0).reject(new Error("gh pr create: HTTP 429: rate limited"));
    await flush();

    expect(screen.getByTestId("pr-error")).toHaveTextContent("429");
    expect(screen.queryByTestId("pr-url")).not.toBeInTheDocument();
  });

  it("disables the Create PR control when there is no client", () => {
    render(<DiffViewerPane client={null} task={TASK} />);
    expect(screen.getByTestId("create-pr-button")).toBeDisabled();
  });
});

/** Minimal DaemonEvents stub: records listeners per topic, lets the test fire them. */
function makeEventsStub() {
  const listeners = new Map<string, Set<(payload: unknown) => void>>();
  return {
    events: {
      subscribe(topic: string, listener: (payload: unknown) => void): () => void {
        let set = listeners.get(topic);
        if (!set) {
          set = new Set();
          listeners.set(topic, set);
        }
        set.add(listener);
        return () => set!.delete(listener);
      },
    },
    fire(topic: string, payload: unknown): void {
      for (const l of listeners.get(topic) ?? []) l(payload);
    },
  };
}

describe("DiffViewerPane live refresh", () => {
  it("refetches task.files on a terminal run.status for the viewed task", async () => {
    const client = new FakeWsClient();
    const stub = makeEventsStub();
    render(<DiffViewerPane client={client} task={TASK} events={stub.events} />);
    client.nth("task.files", 0).resolve({ files: [] } satisfies TaskFilesResult);
    await flush();
    expect(screen.getByTestId("diff-empty")).toBeInTheDocument();

    // Non-terminal status: no refetch.
    act(() => stub.fire("run.status", { runId: "r1", taskId: TASK.ID, status: "running" }));
    await flush();
    expect(client.calls.filter((c) => c.method === "task.files")).toHaveLength(1);

    // Terminal status for the same task: refetch.
    act(() => stub.fire("run.status", { runId: "r1", taskId: TASK.ID, status: "done" }));
    await flush();
    expect(client.calls.filter((c) => c.method === "task.files")).toHaveLength(2);
  });

  it("ignores terminal run.status events for a different task", async () => {
    const client = new FakeWsClient();
    const stub = makeEventsStub();
    render(<DiffViewerPane client={client} task={TASK} events={stub.events} />);
    client.nth("task.files", 0).resolve({ files: [] } satisfies TaskFilesResult);
    await flush();

    act(() => stub.fire("run.status", { runId: "r9", taskId: TASK.ID + 1, status: "done" }));
    await flush();

    expect(client.calls.filter((c) => c.method === "task.files")).toHaveLength(1);
  });
});

describe("DiffViewerPane reveal-in-diff (Item 17)", () => {
  afterEach(() => {
    resetDiffReveal();
  });

  it("consumes a request latched before it mounted, expanding and scrolling to the file", async () => {
    const scrollIntoView = vi.fn();
    // jsdom implements no layout, so Element.scrollIntoView doesn't exist
    // at all -- stub it on the prototype to observe the call.
    Object.defineProperty(Element.prototype, "scrollIntoView", {
      value: scrollIntoView,
      configurable: true,
      writable: true,
    });

    requestDiffReveal(TASK.ID, "new.txt");

    const client = new FakeWsClient();
    render(<DiffViewerPane client={client} task={TASK} />);
    await resolveFilesAndDiffs(client);

    expect(scrollIntoView).toHaveBeenCalled();
    // Consumed, not merely read: re-mounting must not re-scroll.
    expect(screen.getByTestId("diff-file-new.txt")).toBeInTheDocument();
  });

  it("leaves a request aimed at a different task alone", async () => {
    requestDiffReveal(TASK.ID + 1, "new.txt");

    const client = new FakeWsClient();
    render(<DiffViewerPane client={client} task={TASK} />);
    await resolveFilesAndDiffs(client);

    // Still latched for the task it was meant for.
    const { takeDiffReveal } = await import("@/lib/diff-reveal");
    expect(takeDiffReveal(TASK.ID + 1)).toEqual({ taskId: TASK.ID + 1, path: "new.txt" });
  });

  it("re-expands a collapsed file when revealed while already mounted", async () => {
    const client = new FakeWsClient();
    render(<DiffViewerPane client={client} task={TASK} />);
    await resolveFilesAndDiffs(client);

    fireEvent.click(screen.getByTestId("diff-file-header-file.txt"));
    await flush();
    expect(screen.getByTestId("diff-file-header-file.txt")).toHaveTextContent("▸");

    act(() => {
      requestDiffReveal(TASK.ID, "file.txt");
    });
    await flush();

    expect(screen.getByTestId("diff-file-header-file.txt")).toHaveTextContent("▾");
  });
});

const WHOLE_DIFF = `diff --git a/file.txt b/file.txt
index a29bdeb..0226208 100644
--- a/file.txt
+++ b/file.txt
@@ -1,2 +1,2 @@
 line1
-old line
+line2 added
diff --git a/new.txt b/new.txt
new file mode 100644
index 0000000..f2ba8f8
--- /dev/null
+++ b/new.txt
@@ -0,0 +1,1 @@
+brand new
`;

describe("DiffViewerPane review v2 (Item 19)", () => {
  afterEach(() => {
    resetReviewDrafts();
    resetDiffReveal();
    window.localStorage.clear();
  });

  /** Clicks the rendered diff line whose text contains `text`, inside `containerTestId`. */
  function clickDiffLine(containerTestId: string, text: string): void {
    const container = screen.getByTestId(containerTestId);
    const line = [...container.querySelectorAll(".d2h-code-line-ctn")].find((el) => el.textContent?.includes(text));
    if (!line) throw new Error(`no rendered diff line containing ${JSON.stringify(text)}`);
    fireEvent.click(line);
  }

  async function writeComment(body: string): Promise<void> {
    fireEvent.change(screen.getByTestId("review-composer-body"), { target: { value: body } });
    fireEvent.click(screen.getByTestId("review-composer-add"));
    await flush();
  }

  it("exposes a diff stat matching the file list", async () => {
    const client = new FakeWsClient();
    render(<DiffViewerPane client={client} task={TASK} />);
    await resolveFilesAndDiffs(client);
    client.nth("task.diff", 0).resolve({ diff: WHOLE_DIFF });
    await flush();

    const stat = screen.getByTestId("diff-stat");
    // Two files, matching the two-entry task.files list above.
    expect(stat).toHaveAttribute("data-files", String(FILES.files.length));
    expect(stat).toHaveAttribute("data-additions", "2");
    expect(stat).toHaveAttribute("data-deletions", "1");
    expect(stat).toHaveTextContent("2 files +2 −1");
  });

  it("renders every changed file in the whole-diff view", async () => {
    const client = new FakeWsClient();
    render(<DiffViewerPane client={client} task={TASK} />);
    await resolveFilesAndDiffs(client);
    client.nth("task.diff", 0).resolve({ diff: WHOLE_DIFF });
    await flush();

    fireEvent.click(screen.getByTestId("diff-view-toggle-whole"));
    await flush();

    const whole = screen.getByTestId("whole-diff-container");
    expect(whole.textContent).toContain("line2 added");
    expect(whole.textContent).toContain("brand new");
    // The per-file list is replaced, not shown alongside.
    expect(screen.queryByTestId("diff-container-file.txt")).not.toBeInTheDocument();
  });

  it("switches rendering mode with the side-by-side / unified toggle, and persists the choice", async () => {
    const client = new FakeWsClient();
    const { unmount } = render(<DiffViewerPane client={client} task={TASK} />);
    await resolveFilesAndDiffs(client);

    expect(screen.getByTestId("diff-container-file.txt")).toHaveAttribute("data-output-format", "side-by-side");

    fireEvent.click(screen.getByTestId("diff-format-toggle-line-by-line"));
    await flush();

    expect(screen.getByTestId("diff-container-file.txt")).toHaveAttribute("data-output-format", "line-by-line");
    expect(JSON.parse(window.localStorage.getItem(DIFF_PREFS_STORAGE_KEY)!).format).toBe("line-by-line");

    // A tab switch unmounts this pane -- the toggle must not reset.
    unmount();
    const next = new FakeWsClient();
    render(<DiffViewerPane client={next} task={TASK} />);
    await resolveFilesAndDiffs(next);
    expect(screen.getByTestId("diff-container-file.txt")).toHaveAttribute("data-output-format", "line-by-line");
  });

  it("keeps a draft comment across collapsing the file and switching tabs", async () => {
    const client = new FakeWsClient();
    const { unmount } = render(<DiffViewerPane client={client} task={TASK} />);
    await resolveFilesAndDiffs(client);

    clickDiffLine("diff-container-file.txt", "line2 added");
    await flush();
    expect(screen.getByTestId("review-composer")).toHaveAttribute("data-path", "file.txt");
    await writeComment("this leaks the connection");

    const draft = screen.getByTestId("review-draft");
    expect(draft).toHaveAttribute("data-line", "2");
    expect(draft).toHaveTextContent("this leaks the connection");

    // Collapsing the file keeps the draft visible (hiding it would look
    // like it was lost).
    fireEvent.click(screen.getByTestId("diff-file-header-file.txt"));
    await flush();
    expect(screen.getByTestId("review-draft")).toHaveTextContent("this leaks the connection");

    // Switching tabs unmounts the pane entirely.
    unmount();
    const next = new FakeWsClient();
    render(<DiffViewerPane client={next} task={TASK} />);
    await resolveFilesAndDiffs(next);

    expect(screen.getByTestId("review-draft")).toHaveTextContent("this leaks the connection");
  });

  it("submits every draft as one prompt and clears them", async () => {
    const client = new FakeWsClient();
    render(<DiffViewerPane client={client} task={TASK} />);
    await resolveFilesAndDiffs(client);

    clickDiffLine("diff-container-file.txt", "line2 added");
    await flush();
    await writeComment("first comment");

    clickDiffLine("diff-container-new.txt", "line2 added");
    await flush();
    await writeComment("second comment");

    expect(screen.getAllByTestId("review-draft")).toHaveLength(2);
    expect(screen.getByTestId("submit-review-button")).toHaveTextContent("Submit review (2)");

    fireEvent.click(screen.getByTestId("submit-review-button"));
    await flush();

    // The provider comes from the daemon's list, never a hardcoded one.
    expect(client.nth("provider.list", 0)).toBeTruthy();
    client.nth("provider.list", 0).resolve({ providers: [{ id: "glm" }, { id: "claude-native" }] });
    await flush();

    const started = client.nth("run.start", 0).params as { taskId: number; provider: string; prompt: string };
    expect(started.taskId).toBe(TASK.ID);
    expect(started.provider).toBe("glm");
    expect(started.prompt).toContain("first comment");
    expect(started.prompt).toContain("second comment");
    // One prompt, not one run per comment.
    expect(client.calls.filter((c) => c.method === "run.start")).toHaveLength(1);

    client.nth("run.start", 0).resolve({ runId: "run-1" });
    await flush();

    expect(screen.queryByTestId("review-draft")).not.toBeInTheDocument();
    expect(screen.queryByTestId("submit-review-button")).not.toBeInTheDocument();
    expect(getReviewDrafts(TASK.ID)).toEqual([]);
  });

  it("keeps the drafts and surfaces the error when submitting fails", async () => {
    const client = new FakeWsClient();
    render(<DiffViewerPane client={client} task={TASK} />);
    await resolveFilesAndDiffs(client);

    clickDiffLine("diff-container-file.txt", "line2 added");
    await flush();
    await writeComment("keep me");

    fireEvent.click(screen.getByTestId("submit-review-button"));
    await flush();
    client.nth("provider.list", 0).resolve({ providers: [{ id: "claude-native" }] });
    await flush();
    client.nth("run.start", 0).reject(new Error("daemon said no"));
    await flush();

    expect(screen.getByTestId("review-error")).toHaveTextContent("daemon said no");
    expect(screen.getByTestId("review-draft")).toHaveTextContent("keep me");
  });

  it("removes a single draft without touching the others", async () => {
    const client = new FakeWsClient();
    render(<DiffViewerPane client={client} task={TASK} />);
    await resolveFilesAndDiffs(client);

    clickDiffLine("diff-container-file.txt", "line2 added");
    await flush();
    await writeComment("first");
    clickDiffLine("diff-container-file.txt", "line1");
    await flush();
    await writeComment("second");

    expect(screen.getAllByTestId("review-draft")).toHaveLength(2);

    fireEvent.click(screen.getAllByTestId("review-draft-remove")[0]!);
    await flush();

    const remaining = screen.getAllByTestId("review-draft");
    expect(remaining).toHaveLength(1);
    expect(remaining[0]).toHaveTextContent("second");
  });

  it("ignores a click that didn't land on a diff line", async () => {
    const client = new FakeWsClient();
    render(<DiffViewerPane client={client} task={TASK} />);
    await resolveFilesAndDiffs(client);

    fireEvent.click(screen.getByTestId("diff-container-file.txt"));
    await flush();

    expect(screen.queryByTestId("review-composer")).not.toBeInTheDocument();
  });

  it("attributes a whole-diff-view comment to the right file", async () => {
    const client = new FakeWsClient();
    render(<DiffViewerPane client={client} task={TASK} />);
    await resolveFilesAndDiffs(client);
    client.nth("task.diff", 0).resolve({ diff: WHOLE_DIFF });
    await flush();

    fireEvent.click(screen.getByTestId("diff-view-toggle-whole"));
    await flush();

    clickDiffLine("whole-diff-container", "brand new");
    await flush();

    expect(screen.getByTestId("review-composer")).toHaveAttribute("data-path", "new.txt");
    await writeComment("nice");

    expect(getReviewDrafts(TASK.ID)[0]!.path).toBe("new.txt");
  });
});
