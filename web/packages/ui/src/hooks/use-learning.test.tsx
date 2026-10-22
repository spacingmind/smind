import { act } from "react";
import { screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import {
  gateFindings,
  useApproveProposal,
  useExportSkill,
  useLearnedSkill,
  useLearnedSkills,
  useLearningStatus,
  usePendingProposalCount,
  useProposals,
  useCancelProbe,
  useProbe,
  useProbeEstimate,
  useStartProbe,
  useSetLearningMode,
  useSkillAction,
} from "@/hooks/use-learning";
import { clearToasts, Toaster } from "@/components/ui/toast";
import { RpcError } from "@/lib/ws-client";
import { learningDaemon, learningStatus } from "@/test/learning-test-client";
import { estimate, probeView, runningProbe } from "@/test/probe-fixtures";
import { proposal, skill } from "@/test/learning-fixtures";
import { renderHookWithDaemon, renderWithDaemon } from "@/test/render-with-daemon";

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

const STATUS = learningStatus({
  workspaces: [
    { id: 1, title: "alpha", learningMode: "propose", pendingProposals: 2, activeSkills: 1, disabledSkills: 0 },
    { id: 2, title: "beta", learningMode: "off", pendingProposals: 1, activeSkills: 0, disabledSkills: 0 },
  ],
});

afterEach(() => clearToasts());

describe("learning queries", () => {
  it("two consumers share one cached read", async () => {
    const d = learningDaemon((method) => (method === "learning.status" ? STATUS : undefined));
    function Two() {
      const a = useLearningStatus();
      const b = useLearningStatus();
      return <span data-testid="n">{`${a.data?.workspaces.length}/${b.data?.workspaces.length}`}</span>;
    }
    renderWithDaemon(<Two />, d);
    await flush();
    expect(screen.getByTestId("n").textContent).toBe("2/2");
    expect(d.countCalls("learning.status")).toBe(1);
  });

  it("usePendingProposalCount sums the shared status, per workspace or in total", async () => {
    const d = learningDaemon(() => STATUS);
    const view = renderHookWithDaemon((_: null) => [usePendingProposalCount(), usePendingProposalCount(2)], d, {
      initialProps: null,
    });
    expect(view.result.current).toEqual([0, 0]);
    await flush();
    expect(view.result.current).toEqual([3, 1]);
    expect(d.countCalls("learning.status")).toBe(1);
  });

  it("a disabled global switch reports no pending proposals", async () => {
    const d = learningDaemon(() => ({ ...STATUS, enabled: false }));
    const view = renderHookWithDaemon((_: null) => usePendingProposalCount(), d, { initialProps: null });
    await flush();
    expect(view.result.current).toBe(0);
  });

  it("passes workspaceId and statuses to learning.skills.list", async () => {
    const d = learningDaemon(() => [skill({ id: 5 })]);
    const view = renderHookWithDaemon((_: null) => useLearnedSkills(1, ["active", "disabled"]), d, { initialProps: null });
    await flush();
    expect(view.result.current.data).toHaveLength(1);
    expect(d.calls.find((c) => c.method === "learning.skills.list")?.params).toEqual({
      workspaceId: 1,
      statuses: ["active", "disabled"],
    });
  });
});

describe("learning mutations", () => {
  it("setMode invalidates the status read and nothing else", async () => {
    const d = learningDaemon((method) => (method === "learning.status" ? STATUS : null));
    const view = renderHookWithDaemon(
      (_: null) => ({ status: useLearningStatus(), set: useSetLearningMode(), skills: useLearnedSkills(1, ["active"]) }),
      d,
      { initialProps: null },
    );
    await flush();
    await act(async () => {
      view.result.current.set.mutate({ workspaceId: 1, mode: "propose" });
    });
    await flush();
    expect(d.countCalls("learning.setMode")).toBe(1);
    expect(d.countCalls("learning.status")).toBe(2);
    expect(d.countCalls("learning.skills.list")).toBe(1);
  });

  it("a skill action invalidates that workspace's skills and status", async () => {
    const d = learningDaemon((method) => (method === "learning.status" ? STATUS : method.endsWith(".list") ? [] : null));
    const view = renderHookWithDaemon(
      (_: null) => ({
        a: useLearnedSkills(1, ["active"]),
        b: useLearnedSkills(2, ["active"]),
        pin: useSkillAction("pin"),
      }),
      d,
      { initialProps: null },
    );
    await flush();
    await act(async () => {
      view.result.current.pin.mutate({ id: 5, workspaceId: 1 });
    });
    await flush();
    expect(d.calls.find((c) => c.method === "learning.skills.pin")?.params).toEqual({ id: 5 });
    const lists = d.calls.filter((c) => c.method === "learning.skills.list").map((c) => (c.params as { workspaceId: number }).workspaceId);
    expect(lists.sort()).toEqual([1, 1, 2]);
  });

  it("export sends the exact params, force only when set, and re-reads only that skill's detail", async () => {
    const result = { dir: "/home/u/.claude/skills/x", files: ["/home/u/.claude/skills/x/SKILL.md"], status: "installed", skipped: [] };
    const d = learningDaemon((method) => {
      if (method === "learning.skills.export") return result;
      if (method === "learning.skills.get") return { skill: skill({ id: 5 }), versions: [], ledger: [] };
      return null;
    });
    const view = renderHookWithDaemon(
      (_: null) => ({ detail: useLearnedSkill(5), other: useLearnedSkill(6), status: useLearningStatus(), exp: useExportSkill() }),
      d,
      { initialProps: null },
    );
    await flush();
    expect(d.countCalls("learning.skills.get")).toBe(2);
    expect(d.countCalls("learning.status")).toBe(1);
    await act(async () => {
      view.result.current.exp.mutate({ id: 5, target: "claude" });
    });
    await flush();
    expect(d.calls.find((c) => c.method === "learning.skills.export")?.params).toEqual({ id: 5, target: "claude" });
    expect(view.result.current.exp.data).toEqual(result);
    const gets = d.calls.filter((c) => c.method === "learning.skills.get").map((c) => (c.params as { id: number }).id);
    expect(gets.sort()).toEqual([5, 5, 6]);
    expect(d.countCalls("learning.status")).toBe(1);
    await act(async () => {
      view.result.current.exp.mutate({ id: 5, target: "repo", force: true });
    });
    await flush();
    expect(d.calls.filter((c) => c.method === "learning.skills.export")[1]?.params).toEqual({ id: 5, target: "repo", force: true });
  });

  it("a gate_rejected approve surfaces its findings and leaves the proposal list unread", async () => {
    const d = learningDaemon((method) => {
      if (method === "learning.proposals.list") return [proposal({ id: 9 })];
      if (method === "learning.proposals.approve") {
        throw new RpcError("gate rejected", "gate_rejected", { findings: [{ reason: "secret", detail: "looks like a token" }] });
      }
      return null;
    });
    const view = renderHookWithDaemon(
      (_: null) => ({ list: useProposals(1, "pending"), approve: useApproveProposal() }),
      d,
      { initialProps: null },
    );
    await flush();
    await act(async () => {
      view.result.current.approve.mutate({ id: 9, workspaceId: 1, body: "x" });
    });
    await flush();
    expect(gateFindings(view.result.current.approve.error)).toEqual([{ reason: "secret", detail: "looks like a token" }]);
    // Rejected, not changed: no proposals/skills re-read.
    expect(d.countCalls("learning.proposals.list")).toBe(1);
    expect(d.calls.find((c) => c.method === "learning.proposals.approve")?.params).toEqual({ id: 9, body: "x" });
  });

  it("gateFindings is null for other errors", () => {
    expect(gateFindings(new RpcError("nope", "stale"))).toBeNull();
    expect(gateFindings(new Error("x"))).toBeNull();
  });
});

describe("learning events", () => {
  it("learnedSkill.changed re-reads that workspace's skills, proposals and status only", async () => {
    const d = learningDaemon((method) => (method === "learning.status" ? STATUS : []));
    renderHookWithDaemon(
      (_: null) => ({
        s1: useLearnedSkills(1, ["active"]),
        s2: useLearnedSkills(2, ["active"]),
        p1: useProposals(1, "pending"),
        status: useLearningStatus(),
      }),
      d,
      { initialProps: null },
    );
    await flush();
    await act(async () => d.emit("learnedSkill.changed", { workspaceId: 1, reason: "approved" }));
    await flush();
    const byWs = (m: string) => d.calls.filter((c) => c.method === m).map((c) => (c.params as { workspaceId: number }).workspaceId);
    expect(byWs("learning.skills.list").sort()).toEqual([1, 1, 2]);
    expect(byWs("learning.proposals.list")).toEqual([1, 1]);
    expect(d.countCalls("learning.status")).toBe(2);
  });

  it("learnedSkill.proposed raises one toast with a Review action that opens Settings -> Skills (learned)", async () => {
    const d = learningDaemon((method) => (method === "learning.status" ? STATUS : []));
    renderWithDaemon(<Toaster />, d);
    const opened: string[] = [];
    const onOpen = (e: Event) => opened.push((e as CustomEvent<{ sectionId: string }>).detail.sectionId);
    window.addEventListener("smind:open-settings", onOpen);
    await flush();
    await act(async () =>
      d.emit("learnedSkill.proposed", { workspaceId: 1, proposalId: 4, name: "run-focused-tests", kind: "create" }),
    );
    expect(screen.getAllByTestId("toast")).toHaveLength(1);
    expect(screen.getByText("New skill proposal: run-focused-tests")).toBeTruthy();
    await act(async () => screen.getByTestId("toast-action").click());
    window.removeEventListener("smind:open-settings", onOpen);
    expect(opened).toEqual(["learned-skills"]);
    expect(screen.queryByTestId("toast")).toBeNull();
  });

  it("learning.job re-reads that workspace's jobs", async () => {
    const d = learningDaemon(() => []);
    const { useLearningJobs } = await import("@/hooks/use-learning");
    renderHookWithDaemon((_: null) => useLearningJobs(1), d, { initialProps: null });
    await flush();
    await act(async () => d.emit("learning.job", { workspaceId: 1, jobId: 3, status: "done" }));
    await flush();
    expect(d.countCalls("learning.jobs.list")).toBe(2);
  });
});

describe("probe hooks", () => {
  it("useProbeEstimate is keyed by quick and calls the RPC with it", async () => {
    const d = learningDaemon((method) => (method === "learning.probe.estimate" ? estimate() : undefined));
    renderHookWithDaemon((q: boolean) => useProbeEstimate(9, q, true), d, { initialProps: false });
    await flush();
    expect(d.calls.filter((c) => c.method === "learning.probe.estimate").map((c) => c.params)).toEqual([{ id: 9, quick: false }]);
  });

  it("useProbeEstimate does nothing while disabled", async () => {
    const d = learningDaemon(() => estimate());
    renderHookWithDaemon((_: null) => useProbeEstimate(9, false, false), d, { initialProps: null });
    await flush();
    expect(d.countCalls("learning.probe.estimate")).toBe(0);
  });

  it("useProbe reads once when done and re-reads on a probe event", async () => {
    const d = learningDaemon((method) => (method === "learning.probe.get" ? probeView() : undefined));
    const view = renderHookWithDaemon((_: null) => useProbe(9), d, { initialProps: null });
    await flush();
    expect(view.result.current.data?.verdict).toBe("helped");
    expect(d.countCalls("learning.probe.get")).toBe(1);
    await act(async () => d.emit("learnedSkill.probe.finished", { probeId: 5, proposalId: 9, workspaceId: 1, status: "done" }));
    await flush();
    expect(d.countCalls("learning.probe.get")).toBe(2);
  });

  it("useProbe maps an absent probe to null", async () => {
    const d = learningDaemon(() => null);
    const view = renderHookWithDaemon((_: null) => useProbe(9), d, { initialProps: null });
    await flush();
    expect(view.result.current.data).toBeNull();
  });

  it("useProbe polls every 2 s only while queued or running", async () => {
    const running = runningProbe();
    const d = learningDaemon(() => running);
    const view = renderHookWithDaemon((_: null) => useProbe(9), d, { initialProps: null });
    await flush();
    const query = view.queryClient().getQueryCache().find({ queryKey: ["learning.probe.get", { id: 9 }] });
    const options = query?.observers[0]?.options as { refetchInterval?: (q: unknown) => unknown } | undefined;
    expect(options?.refetchInterval?.(query)).toBe(2000);
    query?.setData(probeView());
    expect(options?.refetchInterval?.(query)).toBe(false);
  });

  it("useStartProbe sends quick only when set, seeds the probe entry and surfaces a precondition error", async () => {
    const d = learningDaemon((method, params) => {
      if (method === "learning.probe.start") {
        if ((params as { id: number }).id === 8) throw new RpcError("Another probe is running.", "failed_precondition");
        return runningProbe();
      }
      return null;
    });
    const view = renderHookWithDaemon((_: null) => ({ start: useStartProbe(), probe: useProbe(9, false) }), d, { initialProps: null });
    await act(async () => view.result.current.start.mutate({ id: 9, quick: true, workspaceId: 1 }));
    await flush();
    expect(d.calls.find((c) => c.method === "learning.probe.start")?.params).toEqual({ id: 9, quick: true });
    expect(view.queryClient().getQueryData(["learning.probe.get", { id: 9 }])).toMatchObject({ status: "running" });
    await act(async () => view.result.current.start.mutate({ id: 8 }));
    await flush();
    expect(view.result.current.start.error?.message).toBe("Another probe is running.");
  });

  it("useCancelProbe calls cancel and invalidates the probe and estimate", async () => {
    const d = learningDaemon(() => ({}));
    const view = renderHookWithDaemon((_: null) => useCancelProbe(), d, { initialProps: null });
    const invalidate = vi.spyOn(view.queryClient(), "invalidateQueries");
    await act(async () => view.result.current.mutate({ id: 9, workspaceId: 1 }));
    await flush();
    expect(d.calls.find((c) => c.method === "learning.probe.cancel")?.params).toEqual({ id: 9 });
    expect(invalidate.mock.calls.map((c) => c[0]?.queryKey)).toEqual(
      expect.arrayContaining([["learning.probe.get", { id: 9 }], ["learning.probe.estimate", { id: 9 }]]),
    );
  });

  it("learnedSkill.probe.finished raises one quiet toast with a Review action", async () => {
    const d = learningDaemon(() => null);
    renderWithDaemon(<Toaster />, d);
    await flush();
    await act(async () => d.emit("learnedSkill.probe.finished", { probeId: 5, proposalId: 9, workspaceId: 1, status: "done" }));
    expect(screen.getAllByTestId("toast")).toHaveLength(1);
    expect(screen.getByTestId("toast-action").textContent).toBe("Review");
  });
});
