import { act, screen, waitFor } from "@testing-library/react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { describe, expect, it } from "vitest";

import { usePRDetails, useRefreshPRDetails } from "@/hooks/use-pr-details";
import { queryKeys } from "@/lib/query-keys";
import type { PRDetails, TaskGit, TaskGitPr } from "@/lib/types";
import { FakeWsClient } from "@/test/fake-ws-client";
import { renderWithClientProp } from "@/test/render-with-daemon";

const pr = (over: Partial<TaskGitPr> = {}): TaskGitPr => ({
  number: 7,
  repo: "o/r",
  url: "https://github.com/o/r/pull/7",
  title: "T",
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
  ...over,
});
const gitWith = (p?: TaskGitPr): TaskGit => ({ taskId: 4, untrusted: false, computedAt: "2026-10-23T00:00:00Z", pr: p });

const details = (checks: PRDetails["checks"]["state"] = "success"): PRDetails => ({
  taskId: 4,
  number: 7,
  repo: "o/r",
  url: "https://github.com/o/r/pull/7",
  title: "T",
  state: "open",
  draft: false,
  mergeable: "mergeable",
  reviewDecision: "none",
  body: "",
  bodyTruncated: false,
  checks: { state: checks, passed: 0, failed: 0, pending: 0, total: 0, items: [], truncated: false },
  reviews: { approvals: 0, required: null, items: [], awaiting: [], truncated: false },
  threads: { items: [], truncated: false },
  fetchedAt: "2026-10-23T00:00:00Z",
  stale: "",
  refreshAvailableAt: "",
});

let qc: ReturnType<typeof useQueryClient>;

function Probe({ focused }: { client: FakeWsClient; focused: boolean }) {
  qc = useQueryClient();
  const q = usePRDetails(4, { focused });
  return <span data-testid="title">{q.data?.title ?? "none"}</span>;
}

function Refresh({ taskId }: { client: FakeWsClient; taskId: number }) {
  const r = useRefreshPRDetails(taskId);
  const { data: stored } = useQuery<PRDetails>({ queryKey: queryKeys.taskPRDetails(taskId), enabled: false });
  return (
    <>
      <span data-testid="stored">{stored?.title ?? "none"}</span>
      <button onClick={() => r.refresh()}>refresh</button>
      <span data-testid="err">{r.error?.message ?? ""}</span>
    </>
  );
}

const names = (c: FakeWsClient) => c.calls.map((x) => x.method);
const focusParams = (c: FakeWsClient) => c.calls.filter((x) => x.method === "task.prDetailsFocus").map((x) => (x.params as { taskId: number | null }).taskId);

async function open(client: FakeWsClient, p: TaskGitPr | null = pr()) {
  await waitFor(() => client.nth("task.git", 0));
  await act(async () => client.nth("task.git", 0).resolve(gitWith(p ?? undefined)));
}

describe("usePRDetails", () => {
  it("disabled while the tab is blurred", async () => {
    const client = new FakeWsClient();
    renderWithClientProp(<Probe client={client} focused={false} />);
    await open(client);
    await act(async () => {});
    expect(names(client)).not.toContain("task.prDetails");
    expect(names(client)).not.toContain("task.prDetailsFocus");
  });

  it("does not read without a PR", async () => {
    const client = new FakeWsClient();
    renderWithClientProp(<Probe client={client} focused />);
    await open(client, null);
    await act(async () => {});
    expect(names(client)).not.toContain("task.prDetails");
    expect(names(client)).not.toContain("task.prDetailsFocus");
  });

  it("sends task.prDetailsFocus on focus and null on blur/unmount, and reads only after the focus resolved", async () => {
    const client = new FakeWsClient();
    const view = renderWithClientProp(<Probe client={client} focused />);
    await open(client);
    await waitFor(() => client.nth("task.prDetailsFocus", 0));
    expect(client.nth("task.prDetailsFocus", 0).params).toEqual({ taskId: 4 });
    // The focus call is still pending: no read yet.
    await act(async () => {});
    expect(names(client)).not.toContain("task.prDetails");
    await act(async () => client.nth("task.prDetailsFocus", 0).resolve({}));
    await waitFor(() => client.nth("task.prDetails", 0));
    expect(client.nth("task.prDetails", 0).params).toEqual({ taskId: 4 });
    await act(async () => client.nth("task.prDetails", 0).resolve(details()));
    await waitFor(() => expect(screen.getByTestId("title").textContent).toBe("T"));

    view.rerender(<Probe client={client} focused={false} />);
    await waitFor(() => expect(focusParams(client)).toEqual([4, null]));
    view.rerender(<Probe client={client} focused />);
    await waitFor(() => expect(focusParams(client)).toEqual([4, null, 4]));
    view.unmount();
    expect(focusParams(client)).toEqual([4, null, 4, null]);
  });

  it("re-sends the focus on a new connection", async () => {
    const first = new FakeWsClient();
    const view = renderWithClientProp(<Probe client={first} focused />);
    await open(first);
    await waitFor(() => first.nth("task.prDetailsFocus", 0));
    const second = new FakeWsClient();
    view.rerender(<Probe client={second} focused />);
    await waitFor(() => second.nth("task.prDetailsFocus", 0));
    expect(second.nth("task.prDetailsFocus", 0).params).toEqual({ taskId: 4 });
  });

  it("refetch interval only while checks are pending", async () => {
    const client = new FakeWsClient();
    renderWithClientProp(<Probe client={client} focused />);
    await open(client);
    await waitFor(() => client.nth("task.prDetailsFocus", 0));
    await act(async () => client.nth("task.prDetailsFocus", 0).resolve({}));
    await waitFor(() => client.nth("task.prDetails", 0));
    await act(async () => client.nth("task.prDetails", 0).resolve(details("pending")));
    const interval = () => {
      const obs = qc.getQueryCache().find({ queryKey: queryKeys.taskPRDetails(4) })!.observers[0]!;
      const fn = obs.options.refetchInterval as (q: unknown) => number | false;
      return fn(qc.getQueryCache().find({ queryKey: queryKeys.taskPRDetails(4) }));
    };
    expect(interval()).toBe(20_000);
    act(() => qc.setQueryData(queryKeys.taskPRDetails(4), details("success")));
    expect(interval()).toBe(false);
  });

  it("invalidates the details when the PR head or checks change", async () => {
    const client = new FakeWsClient();
    renderWithClientProp(<Probe client={client} focused />);
    await open(client);
    await waitFor(() => client.nth("task.prDetailsFocus", 0));
    await act(async () => client.nth("task.prDetailsFocus", 0).resolve({}));
    await waitFor(() => client.nth("task.prDetails", 0));
    await act(async () => client.nth("task.prDetails", 0).resolve(details()));
    act(() => qc.setQueryData(queryKeys.taskGit(4), gitWith(pr({ headSha: "bbb" }))));
    await waitFor(() => client.nth("task.prDetails", 1));
  });
});

describe("useRefreshPRDetails", () => {
  it("refresh stores the result and surfaces refresh_limited", async () => {
    const client = new FakeWsClient();
    renderWithClientProp(<Refresh client={client} taskId={4} />);
    act(() => screen.getByText("refresh").click());
    await waitFor(() => client.nth("task.prDetails", 0));
    expect(client.nth("task.prDetails", 0).params).toEqual({ taskId: 4, refresh: true });
    await act(async () => client.nth("task.prDetails", 0).resolve({ ...details(), title: "fresh" }));
    await waitFor(() => expect(screen.getByTestId("stored").textContent).toBe("fresh"));
    act(() => screen.getByText("refresh").click());
    await waitFor(() => client.nth("task.prDetails", 1));
    await act(async () => client.nth("task.prDetails", 1).reject(Object.assign(new Error("slow down"), { code: "refresh_limited" })));
    await waitFor(() => expect(screen.getByTestId("err").textContent).toBe("slow down"));
  });
});
