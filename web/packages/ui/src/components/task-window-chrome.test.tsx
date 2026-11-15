// A task window holds a smaller IPC capability: the surfaces that need main-only commands are not offered there.
import { screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { SidebarFooterActions } from "@/components/sidebar-footer";
import { SidebarProvider } from "@/components/ui/sidebar";
import { renderWithClientProp as render } from "@/test/render-with-daemon";

vi.mock("@/lib/task-window", () => ({ isTaskWindow: true, isTaskWindowLabel: () => true }));
vi.mock("@/keyboard/keyboard-provider", () => ({ useKeyboard: () => ({ runAction: vi.fn(), bindings: [], isMac: false }) }));

describe("the sidebar footer in a task window", () => {
  it("offers no daemon/connection button and no Settings button, but keeps Inbox and the rest", () => {
    render(
      <SidebarProvider>
        <SidebarFooterActions providerHealth={null} agentCount={0} onAddWorkspace={() => {}} onOpenSettings={() => {}} onOpenInbox={() => {}} />
      </SidebarProvider>,
    );
    expect(screen.queryByTestId("sidebar-footer-host")).toBeNull();
    expect(screen.queryByTestId("sidebar-settings-button")).toBeNull();
    expect(screen.getByTestId("sidebar-footer-inbox")).toBeInTheDocument();
    expect(screen.getByTestId("sidebar-footer-usage")).toBeInTheDocument();
  });
});

describe("the shell in a task window", () => {
  it("openSettings is a no-op there and the update surfaces are not rendered (source guards on App.tsx)", async () => {
    const { readFileSync } = await import("node:fs");
    const app = readFileSync("src/App.tsx", "utf8");
    const open = app.split("const openSettings = useCallback(")[1]!.split("}, [])")[0]!;
    expect(open).toContain("if (isTaskWindow) return;");
    expect(app).toContain("isDesktop && !isTaskWindow && (\n        <DesktopDaemonBanner");
    expect(app).toContain("{isDesktop && !isTaskWindow && <DesktopUpdateDialogView");
    expect(app).toContain("showUpdateBadge={(isMobile || !sidebarOpen) && !isTaskWindow}");
    const main = readFileSync("src/main.tsx", "utf8");
    expect(main).toContain("const FirstRunGate = isDesktop && !isTaskWindow ? lazy(");
  });
});

