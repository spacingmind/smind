import { act } from "react";
import { fireEvent, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { LearnedSkillDetail } from "@/components/learning/learned-skill-detail";
import { RunLinkProvider } from "@/components/learning/run-link";
import type { LearnedSkillDetail as Detail } from "@/lib/learning-types";
import { RpcError } from "@/lib/ws-client";
import { evidence, skill } from "@/test/learning-fixtures";
import { learningDaemon } from "@/test/learning-test-client";
import { renderWithDaemon } from "@/test/render-with-daemon";

async function flush(): Promise<void> {
  await act(async () => {
    for (let i = 0; i < 8; i++) await Promise.resolve();
  });
}

const DETAIL: Detail = {
  skill: skill({ id: 5, name: "run-focused-tests", version: 3, pinned: false, probation: true }),
  versions: [
    { version: 1, description: "Run tests.", body: "# Tests\nRun go test.\n", actor: "reflector", createdAt: "2026-10-10T10:00:00Z" },
    { version: 2, description: "Run tests.", body: "# Tests\nRun go test ./pkg.\n", actor: "human", createdAt: "2026-10-12T10:00:00Z" },
    { version: 3, description: "Run one package first.", body: "# Tests\nRun go test ./pkg.\nThen the suite.\n", actor: "curator", createdAt: "2026-10-14T10:00:00Z" },
  ],
  ledger: [
    { id: 1, skillId: 5, skillName: "run-focused-tests", action: "created", reason: "approved", absorbedInto: "", runId: "run-7", actor: "human", createdAt: "2026-10-10T10:00:00Z" },
    { id: 2, skillId: 5, skillName: "run-focused-tests", action: "patched", reason: "tighten wording", absorbedInto: "old-tests", runId: "", actor: "curator", createdAt: "2026-10-14T10:00:00Z" },
  ],
  evidence,
};

const RUNS = [{ ID: "run-7", TaskID: 40, ChatID: 9 }];

function setup(opts: { detail?: Detail; fail?: Record<string, RpcError> } = {}) {
  const calls: { method: string; params: unknown }[] = [];
  const d = learningDaemon((method, params) => {
    calls.push({ method, params });
    const failure = opts.fail?.[method];
    if (failure) throw failure;
    if (method === "learning.skills.get") return opts.detail ?? DETAIL;
    if (method === "run.list") return RUNS;
    return null;
  });
  // run.list is not a learning.* call: answer it through the base fake's passthrough.
  const base = d.client.call.bind(d.client);
  (d.client as { call: unknown }).call = async (method: string, params?: unknown) =>
    method === "run.list" ? (calls.push({ method, params }), RUNS) : base(method, params);
  let back = 0;
  let closed = 0;
  renderWithDaemon(
    <RunLinkProvider closeSettings={() => closed++}>
      <LearnedSkillDetail id={5} onBack={() => back++} />
    </RunLinkProvider>,
    d,
  );
  return { d, calls, backs: () => back, closed: () => closed };
}

describe("learned skill detail", () => {
  it("shows the body read-only, the usage and the badges", async () => {
    setup();
    await flush();
    expect(screen.getByTestId("skill-body").textContent).toContain("Then the suite.");
    expect(screen.getByTestId("skill-body").className).toContain("font-mono");
    expect(screen.getByTestId("badge-probation")).toBeTruthy();
    expect(screen.getByTestId("skill-section-usage").textContent).toContain("Load rate");
  });

  it("provenance lists evidence with kind labels and links resolvable runs to their chat", async () => {
    const view = setup();
    await flush();
    const items = screen.getAllByTestId("evidence-item");
    expect(items).toHaveLength(2);
    expect(within(items[0]!).getByTestId("evidence-kind").textContent).toBe("You said");
    expect(within(items[0]!).getByText("just run the one package, not everything")).toBeTruthy();
    const link = within(items[0]!).getByTestId("run-link");
    expect(link.textContent).toBe("run run-7 · event 12");
    expect(link.getAttribute("href")).toBe("#/workspace/1/task/40/chat/9");
    // seq -1 is the run's prompt record.
    expect(within(items[1]!).getByTestId("run-link").textContent).toBe("run run-7 · prompt");
    fireEvent.click(link);
    expect(window.location.hash).toBe("#/workspace/1/task/40/chat/9");
    expect(view.closed()).toBe(1);
  });

  it("a run the registry does not know is shown without a link", async () => {
    setup({ detail: { ...DETAIL, evidence: [{ kind: "user_message", runId: "gone", seq: 3, quote: "x" }] } });
    await flush();
    expect(screen.getByTestId("run-link-unresolved").textContent).toBe("run gone · event 3");
  });

  it("version history shows a diff against the previous version", async () => {
    setup();
    await flush();
    expect(screen.getByTestId("version-3").textContent).toContain("current");
    expect(screen.queryByTestId("version-diff-3")).toBeNull();
    fireEvent.click(screen.getByTestId("version-toggle-3"));
    const diff = screen.getByTestId("version-diff-3");
    expect(diff.textContent).toContain("Then the suite.");
    expect(diff.textContent).toContain("Run one package first.");
    expect(screen.getByTestId("version-1").textContent).toContain("First version");
    expect(screen.queryByTestId("version-toggle-1")).toBeNull();
  });

  it("restoring an earlier version asks first, then calls learning.skills.undo", async () => {
    const view = setup();
    await flush();
    expect(screen.queryByTestId("version-restore-3")).toBeNull();
    fireEvent.click(screen.getByTestId("version-restore-2"));
    expect(screen.getByText("Restore version 2?")).toBeTruthy();
    expect(view.calls.some((c) => c.method === "learning.skills.undo")).toBe(false);
    fireEvent.click(screen.getByTestId("skill-undo-dialog-confirm"));
    await flush();
    expect(view.calls.find((c) => c.method === "learning.skills.undo")?.params).toEqual({ id: 5, version: 2 });
    // The skill is re-read afterwards.
    expect(view.calls.filter((c) => c.method === "learning.skills.get")).toHaveLength(2);
    expect(screen.queryByTestId("skill-undo-dialog")).toBeNull();
  });

  it("delete needs a confirm, then removes the skill and goes back", async () => {
    const view = setup();
    await flush();
    fireEvent.click(screen.getByTestId("skill-delete"));
    expect(view.calls.some((c) => c.method === "learning.skills.delete")).toBe(false);
    fireEvent.click(screen.getByText("Cancel"));
    await flush();
    expect(view.calls.some((c) => c.method === "learning.skills.delete")).toBe(false);
    fireEvent.click(screen.getByTestId("skill-delete"));
    fireEvent.click(screen.getByTestId("skill-delete-dialog-confirm"));
    await flush();
    expect(view.calls.find((c) => c.method === "learning.skills.delete")?.params).toEqual({ id: 5 });
    expect(view.backs()).toBe(1);
  });

  it("a failing action shows its error and keeps the skill", async () => {
    setup({ fail: { "learning.skills.pin": new RpcError("skill is archived", "conflict") } });
    await flush();
    fireEvent.click(screen.getByTestId("skill-pin"));
    await flush();
    expect(screen.getByTestId("learned-skill-error").textContent).toContain("skill is archived");
    expect(screen.getByTestId("learned-skill-detail")).toBeTruthy();
  });

  it("pin, disable and archive call their RPCs with the skill id", async () => {
    const view = setup();
    await flush();
    for (const [testId, method] of [
      ["skill-pin", "learning.skills.pin"],
      ["skill-disable", "learning.skills.disable"],
      ["skill-archive", "learning.skills.archive"],
    ] as const) {
      fireEvent.click(screen.getByTestId(testId));
      await flush();
      expect(view.calls.find((c) => c.method === method)?.params).toEqual({ id: 5 });
    }
  });

  it("renders the ledger timeline with merge targets and run links", async () => {
    setup();
    await flush();
    const entries = screen.getAllByTestId("ledger-entry");
    expect(entries).toHaveLength(2);
    expect(entries[0]!.textContent).toContain("created");
    expect(within(entries[0]!).getByTestId("run-link")).toBeTruthy();
    expect(entries[1]!.textContent).toContain("merged into old-tests");
  });

  it("an archived skill offers Restore, not Archive", async () => {
    setup({ detail: { ...DETAIL, skill: { ...DETAIL.skill, status: "archived" } } });
    await flush();
    expect(screen.getByTestId("skill-restore")).toBeTruthy();
    expect(screen.queryByTestId("skill-archive")).toBeNull();
  });
});
