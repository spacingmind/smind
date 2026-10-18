import { act } from "react";
import { fireEvent, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { listSettingsSections } from "@/components/settings/settings-registry";
import "@/components/settings/learned-skills-section";
import type { LearningStatus } from "@/lib/learning-types";
import { RpcError } from "@/lib/ws-client";
import { learningDaemon, learningStatus } from "@/test/learning-test-client";
import { renderWithDaemon } from "@/test/render-with-daemon";

async function flush(): Promise<void> {
  await act(async () => {
    for (let i = 0; i < 6; i++) await Promise.resolve();
  });
}

const STATUS: LearningStatus = learningStatus({
  workspaces: [
    { id: 1, title: "alpha", learningMode: "off", pendingProposals: 0, activeSkills: 0, disabledSkills: 0 },
    { id: 2, title: "beta", learningMode: "propose", pendingProposals: 0, activeSkills: 0, disabledSkills: 0 },
  ],
});

function setup(status: LearningStatus = STATUS, onSetMode?: (params: unknown) => unknown) {
  const d = learningDaemon((method, params) => {
    if (method === "learning.status") return status;
    if (method === "learning.setMode") return onSetMode ? onSetMode(params) : null;
    return [];
  });
  const section = listSettingsSections().find((s) => s.id === "learned-skills")!;
  renderWithDaemon(<>{section.render({ client: d.client, events: d.events })}</>, d);
  return d;
}

describe("learning opt-in", () => {
  it("toggling the workspace to Propose calls learning.setMode for that workspace", async () => {
    const d = setup();
    await flush();
    expect(screen.getByTestId("learning-optin-subtitle").textContent).toMatch(/transcripts/);
    expect(screen.getByTestId("learning-optin-subtitle").textContent).toMatch(/approve/);
    fireEvent.click(screen.getByRole("radio", { name: "Propose skills" }));
    await flush();
    expect(d.calls.find((c) => c.method === "learning.setMode")?.params).toEqual({ workspaceId: 1, mode: "propose" });
    // The status is re-read so the control reflects the daemon's answer.
    expect(d.countCalls("learning.status")).toBe(2);
  });

  it("shows the read-only kill-switch notice and disables the control when learning is globally off", async () => {
    const d = setup({ ...STATUS, enabled: false });
    await flush();
    expect(screen.getByTestId("learning-kill-switch")).toBeTruthy();
    const propose = screen.getByRole("radio", { name: "Propose skills" });
    expect(propose.hasAttribute("disabled") || propose.getAttribute("data-disabled") !== null).toBe(true);
    fireEvent.click(propose);
    await flush();
    expect(d.countCalls("learning.setMode")).toBe(0);
  });

  it("no notice while learning is enabled", async () => {
    setup();
    await flush();
    expect(screen.queryByTestId("learning-kill-switch")).toBeNull();
  });

  it("switching workspace shows that workspace's mode and targets it", async () => {
    const d = setup();
    await flush();
    expect(screen.getByRole("radio", { name: "Off" }).getAttribute("data-state")).toBe("on");
    fireEvent.keyDown(screen.getByTestId("learning-workspace-select"), { key: "ArrowDown" });
    fireEvent.click(await screen.findByRole("option", { name: "beta" }));
    await flush();
    expect(screen.getByRole("radio", { name: "Propose skills" }).getAttribute("data-state")).toBe("on");
    fireEvent.click(screen.getByRole("radio", { name: "Off" }));
    await flush();
    expect(d.calls.find((c) => c.method === "learning.setMode")?.params).toEqual({ workspaceId: 2, mode: "off" });
  });

  it("surfaces a setMode failure", async () => {
    setup(STATUS, () => {
      throw new RpcError("workspace not found");
    });
    await flush();
    fireEvent.click(screen.getByRole("radio", { name: "Propose skills" }));
    await flush();
    expect(screen.getByTestId("learning-optin-error").textContent).toBe("workspace not found");
  });
});

describe("pending badge", () => {
  it("shows a quiet dot in the Settings nav and sidebar button only while proposals are pending", async () => {
    const { PendingProposalBadge } = await import("@/components/learning/pending-badge");
    const pending = learningStatus({
      workspaces: [{ id: 1, title: "alpha", learningMode: "propose", pendingProposals: 2, activeSkills: 0, disabledSkills: 0 }],
    });
    const d = learningDaemon(() => pending);
    renderWithDaemon(<PendingProposalBadge />, d);
    expect(screen.queryByTestId("learning-pending-badge")).toBeNull();
    await flush();
    expect(screen.getByLabelText("2 pending skill proposals")).toBeTruthy();
  });

  it("renders nothing outside a daemon connection", async () => {
    const { PendingProposalBadge } = await import("@/components/learning/pending-badge");
    const { render } = await import("@testing-library/react");
    render(<PendingProposalBadge />);
    expect(screen.queryByTestId("learning-pending-badge")).toBeNull();
  });
});
