import { act, cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { PRPanelPane } from "@/components/pr-panel/pr-panel-pane";
import { clearToasts } from "@/components/ui/toast";
import type { PRDetails, TaskGit, TaskGitPr } from "@/lib/types";
import { useComposerAttachmentsStore } from "@/stores/composer-attachments-store";
import { useFocusedChatStore } from "@/stores/focused-chat-store";
import { FakeWsClient } from "@/test/fake-ws-client";
import { check, checksOf, prDetails, thread } from "@/test/pr-details-fixture";
import { renderWithClientProp } from "@/test/render-with-daemon";

const gitPr = (): TaskGitPr => ({
  number: 7,
  repo: "o/r",
  url: "https://github.com/o/r/pull/7",
  title: "Add thing",
  state: "open",
  draft: false,
  headRef: "b",
  headSha: "aaa",
  baseRef: "main",
  fromFork: false,
  mergeable: "mergeable",
  reviewDecision: "none",
  fetchedAt: "2026-10-23T00:00:00Z",
  stale: "",
  refreshAvailableAt: "",
});
const gitWith = (pr?: TaskGitPr): TaskGit => ({ taskId: 1, untrusted: false, computedAt: "2026-10-23T00:00:00Z", pr });

function Harness({ focused = true }: { client: FakeWsClient; focused?: boolean }) {
  return <PRPanelPane taskId={1} focused={focused} />;
}

const names = (c: FakeWsClient) => c.calls.map((x) => x.method);

/** Walks the panel's load: task.git, the focus call, then the first read. */
async function load(client: FakeWsClient, details: PRDetails | null, pr: TaskGitPr | null = gitPr()) {
  await waitFor(() => client.nth("task.git", 0));
  await act(async () => client.nth("task.git", 0).resolve(gitWith(pr ?? undefined)));
  if (details === null) return;
  await waitFor(() => client.nth("task.prDetailsFocus", 0));
  await act(async () => client.nth("task.prDetailsFocus", 0).resolve({}));
  await waitFor(() => client.nth("task.prDetails", 0));
  await act(async () => client.nth("task.prDetails", 0).resolve(details));
  await screen.findByTestId("pr-header");
}

async function show(details: PRDetails, focused = true) {
  const client = new FakeWsClient();
  renderWithClientProp(<Harness client={client} focused={focused} />);
  await load(client, details);
  return client;
}

beforeEach(() => {
  useComposerAttachmentsStore.setState({ byKey: {}, mentions: {}, pendingInserts: {}, pendingTexts: {} });
  useFocusedChatStore.setState({ byTask: { 1: 10 }, revealRequest: null });
});
afterEach(() => {
  cleanup();
  clearToasts();
});

const checkRows = () => screen.getAllByTestId("pr-check-row");

describe("PRPanelPane", () => {
  it("renders checks headline and counts for passing / failing / pending", async () => {
    const ok = check({ id: "run:2", name: "lint", state: "success", conclusion: "success", durationMs: 80_000 });
    const run = check({ id: "run:3", name: "e2e", state: "pending", conclusion: "" });
    const bad = check({ id: "run:1:job:2", name: "build" });

    await show(prDetails({ checks: checksOf([ok]) }));
    expect(screen.getByTestId("pr-checks-headline").textContent).toBe("All checks passed");
    expect(screen.getByText("1 passed")).toBeTruthy();
    expect(screen.getByText("1m 20s")).toBeTruthy();
    cleanup();

    const skipped = check({ id: "run:4", name: "optional", state: "skipped", conclusion: "skipped", durationMs: 0 });
    await show(prDetails({ checks: checksOf([skipped, { ...run, id: "run:5", durationMs: undefined }]) }));
    const [skippedRow, runningRow] = checkRows();
    expect(within(skippedRow!).getByRole("img", { name: "Skipped" })).toBeTruthy();
    expect(skippedRow!.textContent).not.toContain("0s");
    expect(runningRow!.textContent).not.toMatch(/\ds\b/);
    cleanup();

    await show(prDetails({ checks: checksOf([bad, { ...bad, id: "run:9", name: "vet" }, run, ok]) }));
    expect(screen.getByTestId("pr-checks-headline").textContent).toBe("Some checks were not successful");
    expect(screen.getByText("2 failing · 1 running · 1 passed")).toBeTruthy();
    cleanup();

    await show(prDetails({ checks: checksOf([run, ok]) }));
    expect(screen.getByTestId("pr-checks-headline").textContent).toBe("Checks are running");
    expect(screen.getByText("1 running · 1 passed")).toBeTruthy();
    cleanup();

    await show(prDetails({ checks: checksOf([]) }));
    expect(screen.getByTestId("pr-checks-headline").textContent).toBe("No checks");
  });

  it("renders review decision and approvals with and without a required count", async () => {
    const review = { author: "bob", state: "approved" as const, submittedAt: "2026-10-23T00:00:00Z", body: "lgtm", bodyTruncated: false };
    await show(
      prDetails({
        reviewDecision: "approved",
        reviews: { approvals: 1, required: 2, items: [review], awaiting: ["carol", "dave"], truncated: false },
      }),
    );
    expect(screen.getByTestId("pr-review-decision").textContent).toBe("Approved");
    expect(screen.getByTestId("pr-approvals").textContent).toBe("1 of 2 approvals");
    expect(screen.getAllByText("1 of 2 approvals")).toHaveLength(1);
    expect(within(screen.getByTestId("pr-review-row")).getByText("bob")).toBeTruthy();
    expect(screen.getByTestId("pr-awaiting").textContent).toBe("Awaiting: carol, dave");
    cleanup();

    await show(prDetails({ reviewDecision: "review_required", reviews: { approvals: 1, required: null, items: [review], awaiting: [], truncated: false } }));
    expect(screen.getByTestId("pr-review-decision").textContent).toBe("Review required");
    expect(screen.getByTestId("pr-approvals").textContent).toBe("1 approval");
    expect(screen.queryByTestId("pr-awaiting")).toBeNull();
    cleanup();

    await show(prDetails({ reviewDecision: "none" }));
    expect(screen.queryByTestId("pr-review-decision")).toBeNull();
  });

  it("threads collapsed with latest comment and expand", async () => {
    const t = thread({
      comments: [
        { author: "alice", createdAt: "2026-10-23T00:00:00Z", body: "first comment", bodyTruncated: false },
        { author: "bob", createdAt: "2026-10-23T00:01:00Z", body: "latest reply", bodyTruncated: false },
      ],
    });
    await show(prDetails({ threads: { items: [t], truncated: false } }));
    expect(screen.getByTestId("pr-thread-location").textContent).toBe("src/a.go:12");
    expect(screen.getByTestId("pr-thread-latest").textContent).toContain("latest reply");
    expect(screen.getByTestId("pr-thread-latest").textContent).toContain("bob");
    expect(screen.queryByText("first comment")).toBeNull();

    fireEvent.click(screen.getByTestId("pr-thread-toggle"));
    expect(screen.getAllByTestId("pr-thread-comment")).toHaveLength(2);
    expect(screen.getByText("first comment")).toBeTruthy();
    expect(screen.queryByTestId("pr-thread-latest")).toBeNull();
  });

  it("resolved and outdated badges", async () => {
    await show(
      prDetails({
        threads: { items: [thread({ id: "a", isResolved: true }), thread({ id: "b", isOutdated: true, line: null }), thread({ id: "c" })], truncated: true },
      }),
    );
    const rows = screen.getAllByTestId("pr-thread");
    expect(within(rows[0]!).getByText("Resolved")).toBeTruthy();
    expect(within(rows[0]!).queryByText("Outdated")).toBeNull();
    expect(within(rows[1]!).getByText("Outdated")).toBeTruthy();
    expect(within(rows[1]!).getByTestId("pr-thread-location").textContent).toBe("src/a.go");
    expect(within(rows[2]!).queryByText("Resolved")).toBeNull();
    expect(screen.getByTestId("pr-threads-truncated")).toBeTruthy();
  });

  it("empty state", async () => {
    const client = new FakeWsClient();
    renderWithClientProp(<Harness client={client} />);
    await load(client, null, null);
    expect(await screen.findByText("No pull request yet")).toBeTruthy();
    expect(screen.getByText("Create a pull request for this task to see its details here.")).toBeTruthy();
    expect(names(client)).not.toContain("task.prDetails");
    cleanup();

    await show(prDetails({ checks: checksOf([]), threads: { items: [], truncated: false } }));
    expect(screen.getByText("No checks have reported for this pull request.")).toBeTruthy();
    expect(screen.getByText("No reviews yet.")).toBeTruthy();
    expect(screen.getByText("No review threads.")).toBeTruthy();
  });

  it("shows a skeleton while loading", async () => {
    const client = new FakeWsClient();
    renderWithClientProp(<Harness client={client} />);
    expect(screen.getByTestId("pr-skeleton")).toBeTruthy();
    await load(client, null);
    expect(screen.getByTestId("pr-skeleton")).toBeTruthy();
  });

  it("stale budget line", async () => {
    await show(prDetails({ stale: "budget", refreshAvailableAt: "2026-10-23T10:30:00Z" }));
    expect(screen.getByTestId("pr-notice").textContent).toMatch(/GitHub refresh limit reached; showing the last data\. Next refresh .*\d.*\./);
    cleanup();
    await show(prDetails({ stale: "rate_limited" }));
    expect(screen.getByTestId("pr-notice").textContent).toBe("GitHub is rate limiting smind; showing the last data.");
    cleanup();
    await show(prDetails({ stale: "unavailable" }));
    expect(screen.getByTestId("pr-notice").textContent).toBe("Couldn't reach GitHub; showing the last data.");
    cleanup();
    await show(prDetails({ stale: "unauthenticated" }));
    expect(screen.getByTestId("pr-notice").textContent).toContain("Not signed in to GitHub on this machine.");
    expect(screen.getByTestId("forge-settings-link")).toBeTruthy();
    cleanup();
    await show(prDetails({ stale: "not_focused" }));
    expect(screen.queryByTestId("pr-notice")).toBeNull();
  });

  it("refetches once at refreshAvailableAt when budget-limited with nothing to show", async () => {
    const at = new Date(Date.now() + 60).toISOString();
    const empty = prDetails({
      title: "",
      stale: "budget",
      refreshAvailableAt: at,
      checks: checksOf([]),
      threads: { items: [], truncated: false },
    });
    const client = await show(empty);
    expect(screen.getByText("Nothing to show yet.")).toBeTruthy();
    await waitFor(() => expect(client.calls.filter((c) => c.method === "task.prDetails")).toHaveLength(2));
    await act(async () => {
      await new Promise((r) => setTimeout(r, 150));
    });
    expect(client.calls.filter((c) => c.method === "task.prDetails")).toHaveLength(2);
  });

  it("refresh button", async () => {
    const client = await show(prDetails());
    expect(screen.queryByTestId("pr-refresh-spinner")).toBeNull();
    fireEvent.click(screen.getByTestId("pr-refresh"));
    await waitFor(() => expect(client.calls.filter((c) => c.method === "task.prDetails")).toHaveLength(2));
    expect(client.calls.filter((c) => c.method === "task.prDetails")[1]!.params).toEqual({ taskId: 1, refresh: true });
    expect(screen.getByTestId("pr-refresh-spinner")).toBeTruthy();
    expect((screen.getByTestId("pr-refresh") as HTMLButtonElement).disabled).toBe(true);
    await act(async () => client.calls.filter((c) => c.method === "task.prDetails")[1]!.resolve(prDetails({ title: "Renamed" })));
    await waitFor(() => expect(screen.queryByTestId("pr-refresh-spinner")).toBeNull());
    expect(screen.getByText("Renamed")).toBeTruthy();
  });

  it("hostile comment renders as text", async () => {
    const long = "A".repeat(5000);
    const bodies = [
      '<img src=x onerror="window.__pwned=1">',
      "<script>window.__pwned=2</script>",
      "[click me](javascript:alert(1))",
      "[docs](https://evil.example/x) and **bold** and `code`",
      long,
    ];
    const hostile = bodies.map((body, i) => ({ author: `<b>mallory${i}</b>`, createdAt: "2026-10-23T00:00:00Z", body, bodyTruncated: false }));
    await show(
      prDetails({
        title: "<img src=x onerror=alert(1)> title",
        checks: checksOf([check({ name: "<script>x</script>", url: "javascript:alert(1)" })]),
        threads: { items: [thread({ path: "<img src=x>.go", comments: hostile })], truncated: false },
      }),
    );
    fireEvent.click(screen.getByTestId("pr-thread-toggle"));
    const panel = screen.getByTestId("pr-panel");
    expect(panel.querySelector("img")).toBeNull();
    expect(panel.querySelector("script")).toBeNull();
    const thr = screen.getByTestId("pr-thread");
    expect(thr.querySelector("a, b, strong, code")).toBeNull();
    for (const body of bodies) expect(thr.textContent).toContain(body);
    expect(screen.getByText("<img src=x onerror=alert(1)> title")).toBeTruthy();
    expect(screen.getByText("<script>x</script>")).toBeTruthy();
    expect(screen.getAllByTestId("pr-thread-comment")[4]!.firstElementChild!.nextElementSibling!.className).toContain("break-words");
    // The only anchor in the panel is the header's daemon-provided PR link; the javascript: check URL made none.
    const hrefs = Array.from(panel.querySelectorAll("a")).map((a) => a.getAttribute("href"));
    expect(hrefs).toEqual(["https://github.com/o/r/pull/7"]);
    expect(screen.queryByTestId("pr-check-link")).toBeNull();
    expect((window as unknown as { __pwned?: number }).__pwned).toBeUndefined();
  });

  it("links from daemon URLs open safely", async () => {
    await show(prDetails());
    const link = screen.getByTestId("pr-check-link");
    expect(link.getAttribute("href")).toBe("https://github.com/o/r/actions/runs/1/job/2");
    expect(link.getAttribute("rel")).toBe("noopener noreferrer");
    expect(link.getAttribute("target")).toBe("_blank");
    expect(screen.getByTestId("pr-open-link").getAttribute("rel")).toBe("noopener noreferrer");
  });

  it("send to chat buttons call the hook", async () => {
    const ok = check({ id: "run:2", name: "lint", state: "success", conclusion: "success" });
    const bad = check({ id: "run:1:job:2", name: "build" });
    const client = await show(prDetails({ checks: checksOf([bad, ok]), threads: { items: [thread()], truncated: false } }));
    const rows = checkRows();
    expect(within(rows[0]!).getByTestId("pr-check-send")).toBeTruthy();
    expect(within(rows[1]!).queryByTestId("pr-check-send")).toBeNull();

    fireEvent.click(within(rows[0]!).getByTestId("pr-check-send"));
    await waitFor(() => client.nth("task.prCheckLog", 0));
    expect(client.nth("task.prCheckLog", 0).params).toEqual({ taskId: 1, checkId: "run:1:job:2" });
    expect((within(checkRows()[0]!).getByTestId("pr-check-send") as HTMLButtonElement).getAttribute("aria-busy")).toBe("true");
    await act(async () =>
      client.nth("task.prCheckLog", 0).resolve({ checkId: "run:1:job:2", text: "boom", lines: 1, truncated: false, fetchedAt: "" }),
    );
    await waitFor(() => expect(Object.values(useComposerAttachmentsStore.getState().pendingTexts).flat().join("")).toContain("boom"));

    fireEvent.click(screen.getByTestId("pr-thread-send"));
    expect(Object.values(useComposerAttachmentsStore.getState().pendingTexts).flat().join("")).toContain("please rename");
    fireEvent.click(screen.getByTestId("pr-threads-send-all"));
    expect(Object.values(useComposerAttachmentsStore.getState().pendingTexts).flat().join("")).toContain("1 unresolved review thread");
  });

  it("disables send buttons without a chat or an unresolved thread", async () => {
    useFocusedChatStore.setState({ byTask: {} });
    await show(prDetails({ threads: { items: [thread({ isResolved: true })], truncated: false } }));
    expect((screen.getByTestId("pr-check-send") as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByTestId("pr-thread-send") as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByTestId("pr-threads-send-all") as HTMLButtonElement).disabled).toBe(true);
  });

  it("blurred pane makes no reads", async () => {
    const client = new FakeWsClient();
    renderWithClientProp(<Harness client={client} focused={false} />);
    await load(client, null);
    await act(async () => {});
    expect(names(client)).not.toContain("task.prDetails");
    expect(names(client)).not.toContain("task.prDetailsFocus");
    expect(screen.getByTestId("pr-skeleton")).toBeTruthy();
  });

  it("shows a query error as one sentence", async () => {
    const client = new FakeWsClient();
    renderWithClientProp(<Harness client={client} />);
    await load(client, null);
    await waitFor(() => client.nth("task.prDetailsFocus", 0));
    await act(async () => client.nth("task.prDetailsFocus", 0).resolve({}));
    await waitFor(() => client.nth("task.prDetails", 0));
    await act(async () => client.nth("task.prDetails", 0).reject(Object.assign(new Error("x"), { code: "forge_unauthenticated" })));
    expect(await screen.findByTestId("pr-query-error")).toBeTruthy();
    expect(screen.getByTestId("pr-query-error-settings-link")).toBeTruthy();
  });
});
