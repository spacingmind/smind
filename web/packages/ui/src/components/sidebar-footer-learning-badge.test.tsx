import { act } from "react";
import { screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { SidebarFooterActions } from "@/components/sidebar-footer";
import { SidebarProvider } from "@/components/ui/sidebar";
import { learningDaemon, learningStatus } from "@/test/learning-test-client";
import { renderWithDaemon } from "@/test/render-with-daemon";

vi.mock("@/keyboard/keyboard-provider", () => ({ useKeyboard: () => ({ runAction: vi.fn() }) }));

describe("sidebar Settings button", () => {
  it("carries the pending-proposals dot only while a proposal is pending", async () => {
    let pending = 1;
    const d = learningDaemon(() =>
      learningStatus({
        workspaces: [{ id: 1, title: "alpha", learningMode: "propose", pendingProposals: pending, activeSkills: 0, disabledSkills: 0 }],
      }),
    );
    renderWithDaemon(
      <SidebarProvider>
        <SidebarFooterActions providerHealth={null} agentCount={0} onAddWorkspace={() => {}} onOpenSettings={() => {}} />
      </SidebarProvider>,
      d,
    );
    await act(async () => {
      for (let i = 0; i < 6; i++) await Promise.resolve();
    });
    expect(within(screen.getByTestId("sidebar-settings-button")).getByTestId("learning-pending-badge")).toBeTruthy();

    pending = 0;
    await act(async () => d.emit("learnedSkill.changed", { workspaceId: 1, reason: "approved" }));
    await act(async () => {
      for (let i = 0; i < 6; i++) await Promise.resolve();
    });
    expect(screen.queryByTestId("learning-pending-badge")).toBeNull();
  });
});
