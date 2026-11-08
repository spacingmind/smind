import { act } from "react";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { InboxScreen } from "@/components/inbox/inbox-screen";
import { findingsAsPrompt, type StewardAxis, type StewardReview } from "@/lib/steward-types";
import type { InboxItem } from "@/lib/inbox-types";
import { InboxDaemon } from "@/test/inbox-fixtures";
import { renderWithDaemon } from "@/test/render-with-daemon";

function reviewItem(over: Partial<InboxItem> = {}): InboxItem {
  return {
    key: "review:12",
    kind: "review",
    taskId: 9,
    title: "Add the login fix",
    summary: "Steward: changes requested",
    createdAt: new Date(Date.now() - 60_000).toISOString(),
    state: "new",
    attention: "normal",
    links: { taskId: 9, reviewId: 12 },
    ...over,
  };
}

function review(over: Partial<StewardReview> = {}): StewardReview {
  return {
    id: 12,
    taskId: 9,
    triggerKind: "human",
    headSha: "0123456789abcdef",
    validationId: 3,
    ciState: "success",
    status: "done",
    verdict: "CHANGES",
    summary: "Fix the two findings and push",
    findings: [
      { file: "a.go", line: 12, why: "unquoted input reaches a shell", check: "run the mutation check", severity: "high" },
      { file: "a.go", line: 4, why: "missing error check", check: "task test" },
      { file: "b.ts", line: 3, why: "test asserts nothing", check: "task test" },
    ],
    gates: [
      { id: "ci", state: "pass" },
      { id: "not_stacked", state: "fail", detail: "carries 2 commits of another PR" },
      { id: "pr_hygiene", state: "pass" },
    ],
    frames: [{ name: "overview", viewport: 1280, theme: "light", capturedSha: "0123456789abcdef" }],
    prNumber: 42,
    mergeReady: false,
    costUsd: 0.4,
    createdAt: "2026-11-01T00:00:00Z",
    ...over,
  };
}

/** An InboxDaemon that also answers the steward surface: `steward.get` from `reviews`, `steward.frame` from `frames`. */
class StewardDaemon extends InboxDaemon {
  reviews: Record<number, StewardReview> = {};
  frames: Record<string, { contentType: string; data: string }> = {};

  constructor() {
    super();
    const holder = this.client as unknown as { call: (m: string, p?: unknown) => Promise<unknown> };
    const inner = holder.call;
    holder.call = async (method, params) => {
      const p = (params ?? {}) as Record<string, unknown>;
      if (method === "steward.get") {
        this.calls.push({ method, params });
        const r = this.reviews[p.reviewId as number];
        if (!r) throw new Error(`no steward review ${String(p.reviewId)}`);
        return r;
      }
      if (method === "steward.frame") {
        this.calls.push({ method, params });
        const f = this.frames[`${p.reviewId}:${p.name}`];
        if (!f) throw new Error(`no frame ${String(p.name)}`);
        return f;
      }
      return inner.call(this.client, method, params);
    };
  }
}

async function flush(): Promise<void> {
  await act(async () => {
    for (let i = 0; i < 10; i++) await Promise.resolve();
  });
}

async function click(el: HTMLElement): Promise<void> {
  fireEvent.click(el);
  await flush();
}

/** Renders the inbox with one review row and opens its card. */
async function openCard(reviewOver: Partial<StewardReview> = {}, frames: Record<string, { contentType: string; data: string }> = {}): Promise<StewardDaemon> {
  const d = new StewardDaemon();
  d.reviews[12] = review(reviewOver);
  d.frames = frames;
  d.items = [reviewItem()];
  renderWithDaemon(<InboxScreen onNavigateBack={() => {}} onOpenTask={() => {}} />, d);
  await flush();
  await click(screen.getByTestId("inbox-review-review:12"));
  await screen.findByTestId("steward-review-review:12");
  return d;
}

describe("StewardReviewCard", () => {
  it("inbox review row renders verdict chip with icon and text", async () => {
    const d = new StewardDaemon();
    d.reviews[12] = review();
    d.items = [reviewItem()];
    renderWithDaemon(<InboxScreen onNavigateBack={() => {}} onOpenTask={() => {}} />, d);
    await flush();
    const chip = screen.getByTestId("inbox-kind-review:12");
    await waitFor(() => expect(chip).toHaveTextContent("Changes requested"));
    expect(chip.querySelector("svg")).not.toBeNull();
  });

  it("review card lists failing gates first", async () => {
    await openCard();
    const gates = within(screen.getByTestId("steward-gates")).getAllByRole("listitem");
    expect(gates[0]).toHaveTextContent("not_stacked");
    expect(gates[0]).toHaveTextContent("fail");
    expect(gates.map((g) => g.textContent)).toEqual([
      expect.stringContaining("not_stacked"),
      expect.stringContaining("ci"),
      expect.stringContaining("pr_hygiene"),
    ]);
  });

  it("review card shows frames and copy findings", async () => {
    const d = await openCard({}, { "12:overview": { contentType: "image/png", data: "aWN6" } });
    const img = await screen.findByTestId("steward-frame-overview");
    expect(img).toHaveAttribute("src", "data:image/png;base64,aWN6");
    expect(img).toHaveAttribute("alt", "overview (1280px light)");
    expect(d.callsOf("steward.frame")[0].params).toEqual({ reviewId: 12, name: "overview" });

    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
    await click(screen.getByTestId("steward-copy-review:12"));
    expect(writeText).toHaveBeenCalledWith(findingsAsPrompt(d.reviews[12]!));
    expect(screen.getByTestId("steward-copy-review:12")).toHaveTextContent("Copied");
    expect(screen.getByTestId("steward-live")).toHaveTextContent("Findings copied");
  });

  it("review card groups findings by file", async () => {
    await openCard();
    const findings = screen.getByTestId("steward-findings");
    const headings = within(findings).getAllByRole("heading", { level: 4 });
    expect(headings.map((h) => h.textContent)).toEqual(["a.go", "b.ts"]);
    // Each file's findings are by line, whatever order they arrived in.
    const aGo = within(headings[0]!.parentElement!).getAllByRole("listitem");
    expect(aGo.map((li) => li.textContent)).toEqual([expect.stringContaining("line 4:"), expect.stringContaining("line 12:")]);
  });

  it("review card shows an unrated chip for a finding without severity", async () => {
    await openCard({
      findings: [
        { file: "a.go", line: 3, why: "unquoted input reaches a shell", check: "run the mutation check", severity: "high" },
        { file: "a.go", line: 4, why: "missing error check", check: "task test" },
      ],
    });
    const chips = screen.getAllByTestId("steward-finding-severity");
    expect(chips.map((c) => c.textContent)).toEqual(["high", "unrated"]);
  });

  it("unknown review verdict renders as text", async () => {
    await openCard({ verdict: "FROBNICATED", gates: [{ id: "ci", state: "sideways" }] });
    // The row's chip carries the same verdict; assert the card's own chip.
    const card = screen.getByTestId("steward-review-review:12");
    expect(within(card).getByTestId("steward-verdict-chip")).toHaveTextContent("FROBNICATED");
    expect(screen.getByTestId("steward-gate-ci")).toHaveTextContent("sideways");
  });

  it("review card has no merge or approve control", async () => {
    await openCard();
    const card = screen.getByTestId("steward-review-review:12");
    expect(within(card).queryByRole("button", { name: /merge|approve|accept|always allow/i })).toBeNull();
    expect(card.textContent ?? "").not.toMatch(/approve all|always allow/i);
  });

  it("stale review shows the stale note", async () => {
    await openCard({ stale: "head moved on" });
    expect(screen.getByTestId("steward-stale")).toHaveTextContent("Stale: head moved on");
  });

  describe("two axes", () => {
    const axis = (over: Partial<StewardAxis> = {}): StewardAxis => ({ axis: "standards", status: "done", seatVerdict: "ACCEPTED", costUsd: 0.42, ...over });

    it("two axis rows render chip text and model", async () => {
      await openCard({
        axes: [axis({ summary: "Follows the rules", model: "sonnet-5.5" }), axis({ axis: "spec", seatVerdict: "ACCEPTED", model: "glm-5.3" })],
      });
      const rows = within(screen.getByTestId("steward-axes")).getAllByRole("listitem");
      expect(screen.getByRole("heading", { name: "Review axes" })).toBeInTheDocument();
      const std = screen.getByTestId("steward-axis-standards");
      expect(std).toHaveTextContent("Standards");
      expect(std).toHaveTextContent("Accepted");
      expect(std).toHaveTextContent("sonnet-5.5");
      expect(std).toHaveTextContent("Follows the rules");
      expect(std.querySelector("svg")).not.toBeNull();
      expect(screen.getByTestId("steward-axis-spec")).toHaveTextContent("glm-5.3");
      expect(rows.length).toBeGreaterThanOrEqual(2);
    });

    it("disagreement shows one accepted and one changes axis", async () => {
      await openCard({ axes: [axis(), axis({ axis: "spec", seatVerdict: "CHANGES" })] });
      expect(screen.getByTestId("steward-axis-standards")).toHaveTextContent("Accepted");
      expect(screen.getByTestId("steward-axis-spec")).toHaveTextContent("Changes requested");
      expect(screen.getByTestId("steward-verdict-chip")).toHaveTextContent("Changes requested");
    });

    it("skipped spec axis says Skipped · no spec available", async () => {
      await openCard({ axes: [axis(), axis({ axis: "spec", status: "skipped", seatVerdict: undefined, blockedReason: "no_spec" })] });
      expect(screen.getByTestId("steward-axis-spec")).toHaveTextContent("Skipped · no spec available");
    });

    it("blocked axis shows did not complete and its reason", async () => {
      await openCard({ axes: [axis({ status: "blocked", seatVerdict: undefined, blockedReason: "timeout" })] });
      const row = screen.getByTestId("steward-axis-standards");
      expect(row).toHaveTextContent("Did not complete");
      expect(row).toHaveTextContent("Timed out");
    });

    it("running axis shows Running", async () => {
      await openCard({ axes: [axis({ status: "running", seatVerdict: undefined })] });
      expect(screen.getByTestId("steward-axis-standards")).toHaveTextContent("Running");
    });

    it("spec criteria checklist lists met unmet and unverifiable with evidence", async () => {
      await openCard({
        axes: [
          axis({
            axis: "spec",
            seatVerdict: "CHANGES",
            criteria: [
              { id: "AC1", status: "met", evidence: "test covers it" },
              { id: "AC2", status: "met", evidence: "see handler" },
              { id: "AC3", status: "unmet", evidence: "no retry path" },
              { id: "AC4", status: "unverifiable", evidence: "needs a device" },
            ],
          }),
        ],
      });
      const items = within(screen.getByTestId("steward-criteria")).getAllByRole("listitem");
      expect(items).toHaveLength(4);
      expect(items[0]).toHaveTextContent("Met AC1 — test covers it");
      expect(items[2]).toHaveTextContent("Unmet AC3 — no retry path");
      expect(items[3]).toHaveTextContent("Unverifiable AC4 — needs a device");
      expect(items[0].querySelector("svg")).not.toBeNull();
      expect(screen.getByTestId("steward-criteria-count")).toHaveTextContent("2 met, 1 unmet, 1 unverifiable");
    });

    it("findings group by axis then file with the rule line", async () => {
      await openCard({
        axes: [axis(), axis({ axis: "spec" })],
        findings: [
          { file: "s.go", line: 2, why: "misses AC3", check: "task test", axis: "spec" },
          { file: "a.go", line: 7, why: "raw sql", check: "task lint", axis: "standards", rule: "no-raw-sql" },
        ],
      });
      const box = screen.getByTestId("steward-findings");
      const heads = within(box).getAllByRole("heading", { level: 4 }).map((h) => h.textContent);
      expect(heads).toEqual(["Standards", "a.go", "Spec", "s.go"]);
      expect(box).toHaveTextContent("rule: no-raw-sql");
    });

    it("no axes payload renders no axes section", async () => {
      await openCard();
      expect(screen.queryByTestId("steward-axes")).toBeNull();
      expect(screen.queryByText("Review axes")).toBeNull();
    });

    it("axes never render a dollar amount", async () => {
      await openCard({ axes: [axis(), axis({ axis: "spec" })] });
      const text = document.body.textContent ?? "";
      expect(text).not.toContain("$");
      expect(text).not.toContain("0.42");
    });

    it("card with axes still has no merge or approve control", async () => {
      await openCard({ axes: [axis(), axis({ axis: "spec" })] });
      const card = screen.getByTestId("steward-review-review:12");
      expect(within(card).queryByRole("button", { name: /merge|approve|accept|always allow/i })).toBeNull();
    });
  });
});
