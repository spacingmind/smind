import { setSettingsBackGuard } from "@/lib/settings-back-guard";
import { act } from "react";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { registerSettingsSection } from "@/components/settings/settings-registry";
import { SettingsScreen } from "@/components/settings/settings-screen";
import { readStoredThemePreference } from "@/lib/theme";
import { FakeWsClient } from "@/test/fake-ws-client";
import { FakeUsageDaemon } from "@/test/usage-test-client";
import { renderWithDaemon } from "@/test/render-with-daemon";

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

function renderScreen(client = new FakeWsClient(), initialSectionId?: string) {
  const daemon = new FakeUsageDaemon([], client);
  renderWithDaemon(
    <SettingsScreen
      client={daemon.client as never}
      onNavigateBack={() => {}}
      initialSectionId={initialSectionId}
    />,
    daemon,
  );
  return client;
}

afterEach(() => {
  window.localStorage.clear();
  document.documentElement.className = "";
  document.documentElement.style.cssText = "";
});

describe("SettingsScreen shell", () => {
  it("lists Usage in the nav under Agents & providers", () => {
    renderScreen();

    const usage = screen.getByTestId("settings-nav-usage");
    expect(usage).toBeInTheDocument();
    expect(usage).toHaveTextContent("Usage");
  });

  it("renders the built-in General and Appearance sections, General active by default (nav regroup puts it first)", () => {
    renderScreen();

    expect(screen.getByTestId("settings-nav-appearance")).toBeInTheDocument();
    expect(screen.getByTestId("settings-nav-general")).toBeInTheDocument();
    expect(screen.getByTestId("settings-section-general")).toBeInTheDocument();
    expect(screen.queryByTestId("settings-section-appearance")).not.toBeInTheDocument();
  });

  it("groups the nav per the run-config IA regroup: Agents & providers, and Connection (desktop only)", () => {
    renderScreen();

    // Agents & providers group: heading once, then its two members.
    expect(screen.getAllByText("Agents & providers").length).toBe(1);
    expect(screen.getByTestId("settings-nav-agents")).toHaveTextContent("Agents");
    expect(screen.getByTestId("settings-nav-providers")).toHaveTextContent("Providers");

    // Connection is desktop-only; in a web test neither entry renders.
    expect(screen.queryByText("Connection")).not.toBeInTheDocument();
    expect(screen.queryByTestId("settings-nav-connections")).not.toBeInTheDocument();
    expect(screen.queryByTestId("settings-nav-daemon")).not.toBeInTheDocument();

    // Order (Paseo-style): the ungrouped app sections first (General ·
    // Appearance · Notifications · Shortcuts), then groups -- Connection
    // (desktop only), then Agents & providers.
    const ids = screen
      .getAllByRole("listitem")
      .map((li) => li.querySelector("[data-testid^='settings-nav-']")?.getAttribute("data-testid"))
      .filter(Boolean);
    expect(ids).toEqual([
      "settings-nav-general",
      "settings-nav-appearance",
      "settings-nav-notifications",
      "settings-nav-shortcuts",
      "settings-nav-agents",
      "settings-nav-mcp-servers",
      "settings-nav-learned-skills",
      "settings-nav-providers",
      "settings-nav-usage",
    ]);
  });

  it("keeps Notifications and Shortcuts out of the Agents & providers group (they precede its heading)", () => {
    renderScreen();

    const nav = screen.getByRole("navigation", { name: "Settings sections" });
    const heading = within(nav).getByText("Agents & providers");
    for (const id of ["notifications", "shortcuts"]) {
      const item = within(nav).getByTestId(`settings-nav-${id}`);
      expect(item.compareDocumentPosition(heading) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    }
    for (const id of ["agents", "mcp-servers", "providers", "usage"]) {
      const item = within(nav).getByTestId(`settings-nav-${id}`);
      expect(heading.compareDocumentPosition(item) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    }
  });

  it("an ungrouped section registered after a group is set off by a separator", () => {
    const unregister = registerSettingsSection({ id: "late", label: "Late", order: 900, render: () => null });
    try {
      renderScreen();
      const nav = screen.getByRole("navigation", { name: "Settings sections" });
      const separator = within(nav).getByRole("separator");
      const late = within(nav).getByTestId("settings-nav-late");
      expect(separator.compareDocumentPosition(late) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
      expect(within(nav).getAllByRole("separator")).toHaveLength(1);
    } finally {
      unregister();
    }
  });

  it("does not repeat the section name as an in-body title (the header already shows it), but keeps the section's actions", async () => {
    renderScreen();

    for (const [id, label, action] of [
      ["agents", "Agents", "profile-new-button"],
      ["mcp-servers", "MCP servers", "mcp-new-button"],
      ["providers", "Providers", "providers-connect-toggle"],
      ["usage", "Usage", "usage-refresh"],
    ] as const) {
      fireEvent.click(screen.getByTestId(`settings-nav-${id}`));
      expect(screen.getByRole("heading", { level: 1, name: label })).toBeInTheDocument();
      expect(screen.queryByRole("heading", { level: 3, name: label })).not.toBeInTheDocument();
      expect(screen.getByTestId(action)).toBeInTheDocument();
    }

    fireEvent.click(screen.getByTestId("settings-nav-notifications"));
    expect(screen.queryByRole("heading", { level: 3, name: "Notifications" })).not.toBeInTheDocument();
  });

  it("the Providers nav entry renders ADR-0015's real Providers section, not the old open-accounts stub", () => {
    renderScreen();

    fireEvent.click(screen.getByTestId("settings-nav-providers"));
    expect(screen.getByTestId("settings-section-providers")).toBeInTheDocument();
  });

  it("gives the active and inactive nav items the same text-ui-* size (tailwind-merge font-size group regression, see lib/utils.ts's cn() comment)", () => {
    renderScreen();

    const active = screen.getByTestId("settings-nav-appearance");
    const inactive = screen.getByTestId("settings-nav-general");
    const sizeOf = (el: HTMLElement) => el.className.match(/\btext-ui-(?:xl|lg|base|caption|sm|xs)\b/)?.[0];

    expect(sizeOf(active)).toBeDefined();
    expect(sizeOf(active)).toBe(sizeOf(inactive));
  });

  it("switches the detail pane when a different section is clicked", async () => {
    renderScreen();

    fireEvent.click(screen.getByTestId("settings-nav-general"));
    await flush();

    expect(screen.getByTestId("settings-section-general")).toBeInTheDocument();
    expect(screen.queryByTestId("settings-section-appearance")).not.toBeInTheDocument();
  });

  it("is a full-pane screen, not a Dialog -- nothing is portalled and there is no dialog role", () => {
    renderScreen();

    expect(screen.getByTestId("settings-screen")).toBeInTheDocument();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("Escape and the Back button both navigate back to the previous view", () => {
    const onNavigateBack = vi.fn();
    render(<SettingsScreen client={null} onNavigateBack={onNavigateBack} />);

    fireEvent.click(screen.getByTestId("settings-back-button"));
    expect(onNavigateBack).toHaveBeenCalledTimes(1);

    fireEvent.keyDown(document, { key: "Escape" });
    expect(onNavigateBack).toHaveBeenCalledTimes(2);
  });

  it("initialSectionId opens on that section instead of the first registered one", () => {
    render(<SettingsScreen client={null} onNavigateBack={() => {}} initialSectionId="shortcuts" />);

    expect(screen.getByTestId("settings-section-shortcuts")).toBeInTheDocument();
    expect(screen.queryByTestId("settings-section-appearance")).not.toBeInTheDocument();
  });

  it("an unknown initialSectionId falls back to the first registered section instead of blanking the screen", () => {
    render(<SettingsScreen client={null} onNavigateBack={() => {}} initialSectionId="does-not-exist" />);

    expect(screen.getByTestId("settings-section-general")).toBeInTheDocument();
  });

  it("registering a new section in the test makes it appear without editing the shell", () => {
    const unregister = registerSettingsSection({
      id: "test-only",
      label: "Test Only",
      order: 999,
      render: () => <div data-testid="settings-section-test-only">hi</div>,
    });

    try {
      renderScreen();
      expect(screen.getByTestId("settings-nav-test-only")).toBeInTheDocument();

      fireEvent.click(screen.getByTestId("settings-nav-test-only"));
      expect(screen.getByTestId("settings-section-test-only")).toBeInTheDocument();
    } finally {
      unregister();
    }
  });
});

describe("SettingsScreen Appearance section", () => {
  it("changing the theme updates the document and persists", () => {
    renderScreen(undefined, "appearance");
    const appearance = within(screen.getByTestId("settings-section-appearance"));

    fireEvent.click(appearance.getByTestId("settings-theme-dark"));

    expect(document.documentElement.classList.contains("dark")).toBe(true);
    expect(readStoredThemePreference()).toBe("dark");
  });

  it("changing a font-size axis updates its CSS custom property and persists", () => {
    renderScreen(undefined, "appearance");
    const appearance = within(screen.getByTestId("settings-section-appearance"));

    fireEvent.click(appearance.getByTestId("settings-font-size-interface-large"));

    expect(document.documentElement.style.getPropertyValue("--font-scale-interface")).toBe("1.15");

    // Persisted across a remount.
    const client2 = new FakeWsClient();
    render(<SettingsScreen client={client2 as never} onNavigateBack={() => {}} initialSectionId="appearance" />);
    expect(screen.getAllByTestId("settings-font-size-interface-large")[1]).toHaveAttribute("aria-pressed", "true");
  });

  it("each font-size axis is independent -- changing content does not touch interface", () => {
    renderScreen(undefined, "appearance");
    const appearance = within(screen.getByTestId("settings-section-appearance"));

    fireEvent.click(appearance.getByTestId("settings-font-size-content-small"));

    expect(document.documentElement.style.getPropertyValue("--font-scale-content")).toBe("0.9");
    expect(document.documentElement.style.getPropertyValue("--font-scale-interface")).toBe("1");
  });
});

describe("SettingsScreen General section", () => {
  // The old default-provider/default-approval-policy controls moved to
  // Settings -> Agents' ★ default agent (run-config IA plan) -- General
  // keeps a pointer rather than going blank or landing on the next
  // section silently. profiles-section.test.tsx covers the ★ control
  // itself.
  it("points to Settings -> Agents instead of the removed default-provider/policy controls", () => {
    renderScreen();
    fireEvent.click(screen.getByTestId("settings-nav-general"));

    expect(screen.getByTestId("settings-section-general")).toHaveTextContent("Settings → Agents");
    expect(screen.queryByTestId("settings-default-provider")).not.toBeInTheDocument();
  });
});

describe("SettingsScreen Notifications section (AC3)", () => {
  function openNotifications(client = new FakeWsClient()) {
    renderScreen(client);
    fireEvent.click(screen.getByTestId("settings-nav-notifications"));
    return client;
  }

  /** jsdom has no Notification API -- installs a minimal fake so the toggle's "default" (clickable) state is reachable at all; without it useNotificationPermission reports "unsupported" and the button stays disabled, which a separate test below covers directly. */
  function installFakeNotification(initialPermission: NotificationPermission) {
    const requestPermission = vi.fn<() => Promise<NotificationPermission>>().mockResolvedValue("granted");
    const constructed: { title: string; options?: NotificationOptions }[] = [];
    class FakeNotification {
      static permission: NotificationPermission = initialPermission;
      static requestPermission = requestPermission;
      constructor(title: string, options?: NotificationOptions) {
        constructed.push({ title, options });
      }
    }
    vi.stubGlobal("Notification", FakeNotification);
    return { requestPermission, constructed };
  }

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("moved here, not duplicated -- General no longer renders the notifications toggle", () => {
    renderScreen();
    fireEvent.click(screen.getByTestId("settings-nav-general"));
    expect(screen.queryByTestId("settings-notifications-toggle")).not.toBeInTheDocument();
  });

  it("never requests Notification permission on mount -- only an explicit click on the toggle does", async () => {
    const { requestPermission } = installFakeNotification("default");
    openNotifications();

    expect(requestPermission).not.toHaveBeenCalled();

    fireEvent.click(screen.getByTestId("settings-notifications-toggle"));
    await flush();

    expect(requestPermission).toHaveBeenCalledTimes(1);
  });

  it("without a Notification API at all, the toggle renders disabled instead of throwing", () => {
    openNotifications();

    const button = screen.getByTestId("settings-notifications-toggle");
    expect(button).toBeDisabled();
    expect(() => fireEvent.click(button)).not.toThrow();
  });

  it("the sound toggle defaults off and persists a click", () => {
    installFakeNotification("granted");
    openNotifications();

    const toggle = screen.getByTestId("settings-notifications-sound-toggle");
    expect(toggle).toHaveAttribute("aria-checked", "false");

    fireEvent.click(toggle);

    expect(toggle).toHaveAttribute("aria-checked", "true");
  });

  it("the test-notification button is disabled until permission is granted, then sends one and shows success", async () => {
    installFakeNotification("default");
    openNotifications();
    expect(screen.getByTestId("settings-notifications-test-button")).toBeDisabled();

    fireEvent.click(screen.getByTestId("settings-notifications-toggle"));
    await flush();

    const testButton = screen.getByTestId("settings-notifications-test-button");
    expect(testButton).toBeEnabled();
    fireEvent.click(testButton);

    expect(await screen.findByTestId("settings-notifications-test-success")).toBeInTheDocument();
  });

  it("a Notification constructor that throws shows the failed state instead of crashing", async () => {
    class ThrowingNotification {
      static permission: NotificationPermission = "granted";
      static requestPermission = vi.fn();
      constructor() {
        throw new Error("blocked by platform policy");
      }
    }
    vi.stubGlobal("Notification", ThrowingNotification);
    openNotifications();

    fireEvent.click(screen.getByTestId("settings-notifications-test-button"));

    expect(await screen.findByTestId("settings-notifications-test-error")).toBeInTheDocument();
  });
});

describe("SettingsScreen narrow", () => {
  // Same shape as App.responsive.test.tsx: useIsMobile reads
  // window.innerWidth inside an effect, so each render is followed by a
  // flush to let it settle.
  const originalMatchMedia = window.matchMedia;

  beforeEach(() => {
    Object.defineProperty(window, "innerWidth", { writable: true, configurable: true, value: 390 });
    window.matchMedia = vi.fn().mockImplementation(
      (query: string) =>
        ({
          matches: window.innerWidth < 768,
          media: query,
          onchange: null,
          addEventListener: () => {},
          removeEventListener: () => {},
          addListener: () => {},
          removeListener: () => {},
          dispatchEvent: () => false,
        }) as unknown as MediaQueryList,
    );
  });

  afterEach(() => {
    window.matchMedia = originalMatchMedia;
    Object.defineProperty(window, "innerWidth", { writable: true, configurable: true, value: 1024 });
  });

  async function renderNarrow(props: Partial<Parameters<typeof SettingsScreen>[0]> = {}) {
    render(<SettingsScreen client={null} onNavigateBack={props.onNavigateBack ?? (() => {})} initialSectionId={props.initialSectionId} />);
    await flush();
  }

  it("shows only the section list first, then a detail page after tapping a row", async () => {
    await renderNarrow();

    // List first: nav visible, no content pane, no sidebar.
    expect(screen.getByTestId("settings-nav-appearance")).toBeInTheDocument();
    expect(screen.queryByTestId("settings-content")).not.toBeInTheDocument();
    expect(screen.queryByTestId("settings-sidebar")).not.toBeInTheDocument();

    fireEvent.click(screen.getByTestId("settings-nav-appearance"));
    await flush();

    expect(screen.queryByTestId("settings-nav-appearance")).not.toBeInTheDocument();
    expect(screen.getByTestId("settings-content")).toBeInTheDocument();
    expect(screen.getByTestId("settings-section-appearance")).toBeInTheDocument();
    expect(screen.getByTestId("settings-screen-header")).toHaveTextContent("Appearance");
  });

  it("the detail back runs a section's back guard before leaving the section", async () => {
    const guard = vi.fn().mockReturnValueOnce(true).mockReturnValue(false);
    const release = setSettingsBackGuard(guard);
    await renderNarrow({});
    fireEvent.click(screen.getByTestId("settings-nav-shortcuts"));
    await flush();

    fireEvent.click(screen.getByTestId("settings-detail-back"));
    await flush();
    // The guard handled the first press (an inner level popped): Settings stays on the section.
    expect(guard).toHaveBeenCalledTimes(1);
    expect(screen.getByTestId("settings-section-shortcuts")).toBeInTheDocument();

    fireEvent.click(screen.getByTestId("settings-detail-back"));
    await flush();
    expect(screen.getByTestId("settings-nav-shortcuts")).toBeInTheDocument();
    release();
  });

  it("back from the detail returns to the list, and back from the list calls onNavigateBack", async () => {
    const onNavigateBack = vi.fn();
    await renderNarrow({ onNavigateBack });

    fireEvent.click(screen.getByTestId("settings-nav-shortcuts"));
    await flush();
    expect(screen.getByTestId("settings-section-shortcuts")).toBeInTheDocument();
    expect(screen.queryByTestId("settings-back-button")).not.toBeInTheDocument();

    fireEvent.click(screen.getByTestId("settings-detail-back"));
    await flush();
    expect(screen.getByTestId("settings-nav-shortcuts")).toBeInTheDocument();
    expect(onNavigateBack).not.toHaveBeenCalled();

    fireEvent.click(screen.getByTestId("settings-back-button"));
    expect(onNavigateBack).toHaveBeenCalledTimes(1);
  });

  it("initialSectionId lands directly on that section's detail", async () => {
    await renderNarrow({ initialSectionId: "shortcuts" });

    expect(screen.getByTestId("settings-section-shortcuts")).toBeInTheDocument();
    expect(screen.queryByTestId("settings-nav-shortcuts")).not.toBeInTheDocument();
  });

  it("Escape on the detail returns to the list; Escape on the list calls onNavigateBack", async () => {
    const onNavigateBack = vi.fn();
    await renderNarrow({ onNavigateBack });

    fireEvent.click(screen.getByTestId("settings-nav-general"));
    await flush();
    fireEvent.keyDown(document, { key: "Escape" });
    await flush();
    expect(screen.getByTestId("settings-nav-general")).toBeInTheDocument();
    expect(onNavigateBack).not.toHaveBeenCalled();

    fireEvent.keyDown(document, { key: "Escape" });
    expect(onNavigateBack).toHaveBeenCalledTimes(1);
  });
});

describe("SettingsScreen layout", () => {
  it("renders a settings sidebar with a back control above the grouped nav and a content view", () => {
    renderScreen();

    const sidebar = screen.getByTestId("settings-sidebar");
    expect(sidebar).toBeInTheDocument();

    const back = screen.getByTestId("settings-back-button");
    expect(back).toHaveAccessibleName("Back to app");
    expect(back).toHaveTextContent("Back to app");

    const top = screen.getByTestId("settings-sidebar-top");
    const nav = within(sidebar).getByRole("navigation", { name: "Settings sections" });
    // top strip < back row < nav, in DOM order.
    expect(top.compareDocumentPosition(back) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(back.compareDocumentPosition(nav) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();

    expect(screen.getByTestId("settings-content-frame")).toBeInTheDocument();
    expect(screen.getByTestId("settings-screen-header")).toBeInTheDocument();
    expect(screen.getByTestId("settings-content")).toBeInTheDocument();
    expect(screen.getByTestId("settings-section-general")).toBeInTheDocument();
  });

  it("marks the active section with aria-current and switches content on nav click", () => {
    renderScreen();

    expect(screen.getByTestId("settings-nav-general")).toHaveAttribute("aria-current", "true");
    expect(screen.getByTestId("settings-nav-appearance")).toHaveAttribute("aria-current", "false");

    fireEvent.click(screen.getByTestId("settings-nav-appearance"));

    expect(screen.getByTestId("settings-nav-appearance")).toHaveAttribute("aria-current", "true");
    expect(screen.getByTestId("settings-nav-general")).toHaveAttribute("aria-current", "false");
    expect(screen.getByTestId("settings-section-appearance")).toBeInTheDocument();
    expect(screen.queryByTestId("settings-section-general")).not.toBeInTheDocument();
    expect(screen.getByTestId("settings-screen-header")).toHaveTextContent("Appearance");
  });

  it("Back button calls onNavigateBack", () => {
    const onNavigateBack = vi.fn();
    render(<SettingsScreen client={null} onNavigateBack={onNavigateBack} />);

    fireEvent.click(screen.getByTestId("settings-back-button"));
    expect(onNavigateBack).toHaveBeenCalledTimes(1);
  });

  it("Escape calls onNavigateBack when no dialog is open", () => {
    const onNavigateBack = vi.fn();
    render(<SettingsScreen client={null} onNavigateBack={onNavigateBack} />);

    fireEvent.keyDown(document, { key: "Escape" });
    expect(onNavigateBack).toHaveBeenCalledTimes(1);
  });

  it("Escape does not call onNavigateBack when a dialog is open", () => {
    const onNavigateBack = vi.fn();
    render(<SettingsScreen client={null} onNavigateBack={onNavigateBack} />);

    const dialog = document.createElement("div");
    dialog.setAttribute("role", "dialog");
    document.body.appendChild(dialog);
    try {
      fireEvent.keyDown(document, { key: "Escape" });
      expect(onNavigateBack).not.toHaveBeenCalled();
    } finally {
      dialog.remove();
    }
  });

  it("Escape does not call onNavigateBack when the event was default-prevented", () => {
    const onNavigateBack = vi.fn();
    render(<SettingsScreen client={null} onNavigateBack={onNavigateBack} />);

    const event = new KeyboardEvent("keydown", { key: "Escape", cancelable: true });
    event.preventDefault();
    document.dispatchEvent(event);
    expect(onNavigateBack).not.toHaveBeenCalled();
  });

  it("Escape ignores an open dialog that sits inside an inert subtree", () => {
    const onNavigateBack = vi.fn();
    render(<SettingsScreen client={null} onNavigateBack={onNavigateBack} />);

    const inert = document.createElement("div");
    inert.setAttribute("inert", "");
    const dialog = document.createElement("div");
    dialog.setAttribute("role", "dialog");
    inert.appendChild(dialog);
    document.body.appendChild(inert);
    try {
      fireEvent.keyDown(document, { key: "Escape" });
      expect(onNavigateBack).toHaveBeenCalledTimes(1);
    } finally {
      inert.remove();
    }
  });

  it("gives every registered section a nav icon so the labels align", () => {
    renderScreen();

    const navItems = document.querySelectorAll('[data-testid^="settings-nav-"]');
    expect(navItems.length).toBeGreaterThan(0);
    for (const item of navItems) {
      expect(item.querySelector("svg")).not.toBeNull();
    }
  });
});
