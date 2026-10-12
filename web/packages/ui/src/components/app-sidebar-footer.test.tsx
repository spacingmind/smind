// sidebar-footer-resize-scrollbar plan, step 2: the Paseo-style sidebar footer.
import { fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { AppSidebar } from "@/components/app-sidebar";
import { SidebarFooterActions } from "@/components/sidebar-footer";
import { SidebarProvider } from "@/components/ui/sidebar";
import { FakeWsClient } from "@/test/fake-ws-client";

const runAction = vi.fn();
vi.mock("@/keyboard/keyboard-provider", () => ({
  useKeyboard: () => ({ runAction }),
}));

afterEach(() => {
  runAction.mockClear();
  vi.restoreAllMocks();
  vi.resetModules();
  vi.doUnmock("@/lib/platform");
});

type FooterProps = Partial<React.ComponentProps<typeof SidebarFooterActions>>;

function renderFooter(props: FooterProps = {}) {
  const onOpenSettings = vi.fn();
  const onAddWorkspace = vi.fn();
  const view = render(
    <SidebarProvider>
      <SidebarFooterActions
        providerHealth={null}
        agentCount={0}
        onAddWorkspace={onAddWorkspace}
        onOpenSettings={onOpenSettings}
        {...props}
      />
    </SidebarProvider>,
  );
  return { onOpenSettings, onAddWorkspace, ...view };
}

const openHost = () => fireEvent.click(screen.getByTestId("sidebar-footer-host"));
const openMenu = (testId: string) => fireEvent.pointerDown(screen.getByTestId(testId), { button: 0 });

describe("sidebar footer", () => {
  it("header has no theme toggle or settings button", () => {
    render(
      <SidebarProvider>
        <AppSidebar client={new FakeWsClient() as never} selectedTaskId={null} />
      </SidebarProvider>,
    );
    const header = screen.getByTestId("sidebar-expanded-header");
    expect(within(header).queryByTestId("theme-toggle-trigger")).not.toBeInTheDocument();
    expect(within(header).queryByTestId("sidebar-settings-button")).not.toBeInTheDocument();
    expect(within(header).queryByRole("button")).not.toBeInTheDocument();
    expect(within(header).getByTestId("sidebar-logo")).toBeInTheDocument();
    expect(screen.queryByTestId("sidebar-collapsed-header-actions")).not.toBeInTheDocument();
    expect(screen.queryByTestId("sidebar-footer-providers")).not.toBeInTheDocument();
    expect(screen.queryByTestId("sidebar-footer-agents")).not.toBeInTheDocument();
  });

  it("expanded footer icon line order and accessible names", () => {
    renderFooter();
    const actions = screen.getByTestId("sidebar-footer-actions");
    expect(screen.getByTestId("sidebar-footer").className).toContain("border-t");
    const children = Array.from(actions.children) as HTMLElement[];
    const ids = children.map((el) => el.dataset.testid);
    expect(ids).toEqual([
      "sidebar-footer-add-workspace",
      "sidebar-footer-usage",
      "sidebar-footer-host",
      "sidebar-footer-spacer",
      "sidebar-footer-help",
      "theme-toggle-trigger",
      "sidebar-settings-button",
    ]);
    const names = children.filter((el) => el.tagName === "BUTTON").map((el) => el.getAttribute("aria-label"));
    expect(names).toEqual(["Add workspace", "Usage", "Host", "Help & support", "Theme: system", "Settings"]);
  });

  it("settings, usage and add-workspace actions", () => {
    const { onOpenSettings, onAddWorkspace } = renderFooter();
    fireEvent.click(screen.getByTestId("sidebar-settings-button"));
    expect(onOpenSettings).toHaveBeenLastCalledWith();
    fireEvent.click(screen.getByTestId("sidebar-footer-usage"));
    expect(onOpenSettings).toHaveBeenLastCalledWith("usage");
    fireEvent.click(screen.getByTestId("sidebar-footer-add-workspace"));
    expect(onAddWorkspace).toHaveBeenCalledTimes(1);
  });

  it("add workspace opens the create-workspace dialog from the sidebar", () => {
    render(
      <SidebarProvider>
        <AppSidebar client={new FakeWsClient() as never} selectedTaskId={null} />
      </SidebarProvider>,
    );
    fireEvent.click(screen.getByTestId("sidebar-footer-add-workspace"));
    expect(screen.getByRole("dialog")).toBeInTheDocument();
  });

  it("host popover: connection text, provider health, providers/agents rows deep-link", () => {
    const { onOpenSettings } = renderFooter({
      connectionStatusText: "Connected to 127.0.0.1",
      providerHealth: { healthy: 1, total: 2 },
      agentCount: 3,
    });
    openHost();
    expect(screen.getByTestId("sidebar-footer-host-connection")).toHaveTextContent("Connected to 127.0.0.1");
    expect(screen.getByTestId("sidebar-footer-host-providers")).toHaveTextContent("1 healthy");
    expect(screen.getByTestId("sidebar-footer-providers-health-dot")).toHaveAttribute("data-status", "warning");
    expect(screen.getByTestId("sidebar-footer-host-agents")).toHaveTextContent("3");

    fireEvent.click(screen.getByTestId("sidebar-footer-host-providers"));
    expect(onOpenSettings).toHaveBeenLastCalledWith("providers");
    // selecting closes the popover
    expect(screen.queryByTestId("sidebar-footer-host-popover")).not.toBeInTheDocument();

    openHost();
    fireEvent.click(screen.getByTestId("sidebar-footer-host-agents"));
    expect(onOpenSettings).toHaveBeenLastCalledWith("agents");
    expect(screen.queryByTestId("sidebar-footer-host-popover")).not.toBeInTheDocument();
  });

  it("host status dot: disconnected is danger, partial provider health is warning", () => {
    const view = renderFooter();
    // connected + providers unknown -> no dot
    expect(screen.queryByTestId("sidebar-footer-host-dot")).not.toBeInTheDocument();
    view.unmount();

    const warn = renderFooter({ providerHealth: { healthy: 1, total: 2 } });
    expect(screen.getByTestId("sidebar-footer-host-dot")).toHaveAttribute("data-status", "warning");
    warn.unmount();

    const ok = renderFooter({ providerHealth: { healthy: 2, total: 2 } });
    expect(screen.getByTestId("sidebar-footer-host-dot")).toHaveAttribute("data-status", "success");
    ok.unmount();

    renderFooter({ connectionStatus: "disconnected", providerHealth: { healthy: 2, total: 2 } });
    expect(screen.getByTestId("sidebar-footer-host-dot")).toHaveAttribute("data-status", "danger");
  });

  it("host popover shows daemon rows only on desktop", async () => {
    const web = renderFooter();
    openHost();
    expect(screen.queryByTestId("sidebar-footer-host-daemon")).not.toBeInTheDocument();
    expect(screen.queryByTestId("sidebar-footer-host-connections")).not.toBeInTheDocument();
    web.unmount();

    vi.resetModules();
    vi.doMock("@/lib/platform", () => ({
      isDesktop: true,
      desktopOS: "macos",
      desktop: { openExternal: vi.fn(() => Promise.resolve()) },
    }));
    const { SidebarFooterActions: DesktopFooter } = await import("@/components/sidebar-footer");
    const { SidebarProvider: Provider } = await import("@/components/ui/sidebar");
    const onOpenSettings = vi.fn();
    render(
      <Provider>
        <DesktopFooter providerHealth={null} agentCount={0} onAddWorkspace={() => {}} onOpenSettings={onOpenSettings} />
      </Provider>,
    );
    openHost();
    fireEvent.click(screen.getByTestId("sidebar-footer-host-daemon"));
    expect(onOpenSettings).toHaveBeenLastCalledWith("daemon");
    openHost();
    fireEvent.click(screen.getByTestId("sidebar-footer-host-connections"));
    expect(onOpenSettings).toHaveBeenLastCalledWith("connections");
  });

  it("help menu items and external links", () => {
    const open = vi.spyOn(window, "open").mockReturnValue(null);
    const { onOpenSettings } = renderFooter();

    openMenu("sidebar-footer-help");
    fireEvent.click(screen.getByTestId("sidebar-footer-help-shortcuts"));
    expect(onOpenSettings).toHaveBeenLastCalledWith("shortcuts");

    openMenu("sidebar-footer-help");
    fireEvent.click(screen.getByTestId("sidebar-footer-help-palette"));
    expect(runAction).toHaveBeenCalledWith("palette.open");

    openMenu("sidebar-footer-help");
    fireEvent.click(screen.getByTestId("sidebar-footer-help-docs"));
    expect(open).toHaveBeenLastCalledWith("https://github.com/spacingmind/smind#readme", "_blank", "noopener,noreferrer");

    openMenu("sidebar-footer-help");
    fireEvent.click(screen.getByTestId("sidebar-footer-help-issue"));
    expect(open).toHaveBeenLastCalledWith(
      "https://github.com/spacingmind/smind/issues/new",
      "_blank",
      "noopener,noreferrer",
    );
  });

  it("theme options in the footer", () => {
    renderFooter();
    openMenu("theme-toggle-trigger");
    for (const id of ["light", "dark", "system"]) {
      expect(screen.getByTestId(`theme-option-${id}`)).toBeInTheDocument();
    }
    expect(screen.getByTestId("theme-option-system")).toHaveAttribute("aria-checked", "true");
    expect(screen.getByTestId("theme-option-light")).toHaveAttribute("aria-checked", "false");
  });

  it("collapsed rail footer is a vertical icon-only stack", () => {
    renderFooter();
    const actions = screen.getByTestId("sidebar-footer-actions");
    expect(actions.className).toContain("group-data-[collapsible=icon]:flex-col");
    expect(screen.getByTestId("sidebar-footer-spacer").className).toContain("group-data-[collapsible=icon]:hidden");
    // icon-only: every control is a button whose only content is its svg (names come from aria-label)
    for (const button of within(actions).getAllByRole("button")) {
      expect(button.textContent?.trim()).toBe("");
      expect(button.querySelector("svg")).not.toBeNull();
    }
  });

  it("footer rows slot renders nothing when empty", () => {
    const empty = renderFooter();
    expect(screen.queryByTestId("sidebar-footer-rows")).not.toBeInTheDocument();
    empty.unmount();

    renderFooter({ rows: <div>usage</div> });
    expect(within(screen.getByTestId("sidebar-footer-rows")).getByText("usage")).toBeInTheDocument();
  });
});
