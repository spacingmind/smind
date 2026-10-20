import { act } from "react";
import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { ProposalReview } from "@/components/learning/proposal-review";
import type { ProposalDetail, ProposalPrevalence } from "@/lib/learning-types";
import { proposal } from "@/test/learning-fixtures";
import { learningDaemon } from "@/test/learning-test-client";
import { renderWithDaemon } from "@/test/render-with-daemon";

async function flush(): Promise<void> {
  await act(async () => {
    for (let i = 0; i < 8; i++) await Promise.resolve();
  });
}

function setup(detail: ProposalDetail, prevalence: ProposalPrevalence | Error) {
  const calls: { method: string; params: unknown }[] = [];
  const d = learningDaemon((method, params) => {
    calls.push({ method, params });
    if (method === "learning.proposals.get") return detail;
    if (method === "learning.proposals.prevalence") {
      if (prevalence instanceof Error) throw prevalence;
      return prevalence;
    }
    return null;
  });
  renderWithDaemon(<ProposalReview id={9} onBack={() => {}} />, d);
  return { calls };
}

const sig = (over: Partial<ProposalPrevalence["signatures"][number]> = {}): ProposalPrevalence["signatures"][number] => ({
  kind: "failure_recovery",
  tool: "Bash",
  label: "Bash failed: exit status #",
  runs: 7,
  chats: 3,
  lastSeenAt: "2026-10-20T10:00:00Z",
  truncated: false,
  ...over,
});

const P: ProposalDetail = { proposal: proposal({ id: 9 }) };

describe("proposal prevalence", () => {
  it("shows runs and chats with the window", async () => {
    setup(P, { windowDays: 30, signatures: [sig()] });
    await flush();
    const row = screen.getByTestId("prevalence-row");
    expect(row.textContent).toContain("Seen in 7 runs across 3 chats (last 30 days)");
    expect(row.textContent).toContain("Bash failed: exit status #");
  });

  it("uses singular for one run and one chat", async () => {
    setup(P, { windowDays: 14, signatures: [sig({ runs: 1, chats: 1 })] });
    await flush();
    expect(screen.getByTestId("prevalence-row").textContent).toContain("Seen in 1 run across 1 chat (last 14 days)");
  });

  it("says at least when truncated", async () => {
    setup(P, { windowDays: 30, signatures: [sig({ truncated: true })] });
    await flush();
    expect(screen.getByTestId("prevalence-row").textContent).toContain("Seen in at least 7 runs across 3 chats (last 30 days)");
  });

  it("shows nothing for empty signatures", async () => {
    setup(P, { windowDays: 30, signatures: [] });
    await flush();
    expect(screen.queryByTestId("proposal-prevalence")).toBeNull();
  });

  it("shows nothing for a non-pending proposal and never calls the RPC", async () => {
    const v = setup({ proposal: proposal({ id: 9, status: "approved" }) }, { windowDays: 30, signatures: [sig()] });
    await flush();
    expect(screen.queryByTestId("proposal-prevalence")).toBeNull();
    expect(v.calls.filter((c) => c.method === "learning.proposals.prevalence")).toHaveLength(0);
  });

  it("stays quiet when the RPC errors", async () => {
    setup(P, new Error("boom"));
    await flush();
    expect(screen.queryByTestId("proposal-prevalence")).toBeNull();
    expect(screen.queryByText(/boom/)).toBeNull();
  });

  it("shows a not-seen line for zero runs", async () => {
    setup(P, { windowDays: 30, signatures: [sig({ runs: 0 })] });
    await flush();
    expect(screen.getByTestId("prevalence-row").textContent).toContain("Not seen in the last 30 days");
  });
});
