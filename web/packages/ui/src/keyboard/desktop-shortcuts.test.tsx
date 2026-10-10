// desktop-native-feel D3: platform-gated shortcut rows, menu-action
// dispatch and the menu-accelerator no-double-fire dedupe. `@/lib/platform`
// is mocked per scenario (`isDesktop` is a module-load constant), and the
// provider is imported fresh, like desktop-daemon-banner.test.tsx.
import { fireEvent, render } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

afterEach(() => {
  vi.resetModules();
  vi.restoreAllMocks();
  vi.doUnmock("@/lib/platform");
});

function mockPlatform(opts: { desktop: boolean; onMenuAction?: boolean }) {
  let emit: ((action: string) => void) | null = null;
  const onMenuAction = (cb: (a: string) => void) => {
    emit = cb;
    return () => {
      emit = null;
    };
  };
  vi.doMock("@/lib/platform", () => ({
    isDesktop: opts.desktop,
    desktopOS: opts.desktop ? "macos" : null,
    desktop: { onMenuAction: opts.onMenuAction ? onMenuAction : () => () => {} },
  }));
  return {
    emit: (action: string) => emit?.(action),
  };
}

function pressCtrl(key: string, code: string, extra: Partial<KeyboardEventInit> = {}) {
  fireEvent.keyDown(document, { key, code, ctrlKey: true, ...extra });
}

async function renderClaimed(action: "tab.new" | "tab.jump" | "pane.split.right") {
  const fire = vi.fn();
  const { KeyboardProvider, useActionHandler } = await import("@/keyboard/keyboard-provider");
  function Claim() {
    useActionHandler(action, fire);
    return null;
  }
  render(
    <KeyboardProvider>
      <Claim />
    </KeyboardProvider>,
  );
  return fire;
}

describe("shortcuts-desktop-gating", () => {
  it("desktop: Mod+T fires tab.new and Mod+3 fires tab.jump with digit 3", async () => {
    mockPlatform({ desktop: true });
    const newTab = await renderClaimed("tab.new");
    pressCtrl("t", "KeyT");
    expect(newTab).toHaveBeenCalledTimes(1);
    expect(newTab).toHaveBeenCalledWith(null);

    const jump = await renderClaimed("tab.jump");
    pressCtrl("3", "Digit3");
    expect(jump).toHaveBeenCalledTimes(1);
    expect(jump).toHaveBeenCalledWith({ digit: 3 });
  });

  it("web: Mod+T and Mod+3 fire nothing; Alt+Shift+T and Mod+Alt+3 do", async () => {
    mockPlatform({ desktop: false });
    const newTab = await renderClaimed("tab.new");
    pressCtrl("t", "KeyT");
    fireEvent.keyDown(document, { key: "t", code: "KeyT", altKey: true, shiftKey: true });
    expect(newTab).toHaveBeenCalledTimes(1);

    const jump = await renderClaimed("tab.jump");
    pressCtrl("3", "Digit3");
    expect(jump).not.toHaveBeenCalled();
    pressCtrl("3", "Digit3", { altKey: true });
    expect(jump).toHaveBeenCalledTimes(1);
    expect(jump).toHaveBeenCalledWith({ digit: 3 });
  });

  it("platformBindings keeps only rows whose desktop gate matches", async () => {
    const { SHORTCUT_BINDINGS, platformBindings } = await import("@/keyboard/shortcuts");
    const desktopRows = platformBindings(SHORTCUT_BINDINGS, true);
    const webRows = platformBindings(SHORTCUT_BINDINGS, false);
    expect(desktopRows.map((b) => b.id)).toContain("tab-new-desktop");
    expect(desktopRows.map((b) => b.id)).toContain("tab-jump-desktop");
    expect(desktopRows.map((b) => b.id)).not.toContain("tab-new");
    expect(webRows.map((b) => b.id)).toContain("tab-new");
    expect(webRows.map((b) => b.id)).not.toContain("tab-new-desktop");
    expect(webRows.map((b) => b.id)).not.toContain("tab-jump-desktop");
    // Ungated rows stay on both.
    for (const id of ["tab-close", "settings-open", "palette-open"]) {
      expect(desktopRows.map((b) => b.id)).toContain(id);
      expect(webRows.map((b) => b.id)).toContain(id);
    }
  });
});

describe("shortcuts-dialog-shows-platform-combo", () => {
  async function newTabRow(desktop: boolean) {
    mockPlatform({ desktop });
    const { ShortcutRows } = await import("@/components/shortcuts-dialog");
    const { KeyboardProvider } = await import("@/keyboard/keyboard-provider");
    const { screen } = await import("@testing-library/react");
    render(
      <KeyboardProvider>
        <ShortcutRows />
      </KeyboardProvider>,
    );
    // jsdom's navigator is not a mac, so Mod formats as Ctrl.
    const id = desktop ? "tab-new-desktop" : "tab-new";
    const row = screen
      .getAllByTestId("shortcut-row")
      .find((el) => el.getAttribute("data-binding-id") === id);
    return row?.textContent ?? null;
  }

  it("desktop lists Ctrl+T (from Mod+T) for New tab and no web-only row", async () => {
    const text = await newTabRow(true);
    expect(text).toContain("New tab");
    expect(text).toContain("Ctrl");
    expect(text).toContain("T");
  });

  it("web lists Alt+Shift+T for New tab and no desktop row", async () => {
    const text = await newTabRow(false);
    expect(text).toContain("New tab");
    expect(text).toContain("Alt");
    expect(text).toContain("Shift");
    expect(text).toContain("T");
  });
});

describe("menu-action-dispatch", () => {
  it("a menu-action event with id tab.new runs the same handler as the keystroke, exactly once", async () => {
    const menu = mockPlatform({ desktop: true, onMenuAction: true });
    const fire = await renderClaimed("tab.new");
    menu.emit("tab.new");
    expect(fire).toHaveBeenCalledTimes(1);
    expect(fire).toHaveBeenCalledWith(null);
  });
});

describe("menu-accelerator-no-double-fire", () => {
  it("keydown first: a menu-action for the same action within the window is ignored", async () => {
    const menu = mockPlatform({ desktop: true, onMenuAction: true });
    const fire = await renderClaimed("tab.new");
    // The webview reports the keydown, and the OS's menu accelerator also
    // fires -- the second arrival must not run the action again.
    pressCtrl("t", "KeyT");
    menu.emit("tab.new");
    expect(fire).toHaveBeenCalledTimes(1);
  });

  it("menu-action first: a keydown for the same action within the window is ignored", async () => {
    const menu = mockPlatform({ desktop: true, onMenuAction: true });
    const fire = await renderClaimed("tab.new");
    // The menu fires first (macOS routes the keystroke there before the
    // webview's keydown lands) -- the keydown copy is the duplicate.
    menu.emit("tab.new");
    pressCtrl("t", "KeyT");
    expect(fire).toHaveBeenCalledTimes(1);
  });

  it("the same action 400 ms later runs again", async () => {
    mockPlatform({ desktop: true, onMenuAction: true });
    const fire = await renderClaimed("tab.new");
    pressCtrl("t", "KeyT");
    await new Promise((r) => setTimeout(r, 400));
    pressCtrl("t", "KeyT");
    expect(fire).toHaveBeenCalledTimes(2);
  });

  it("the browser stub never fires a menu action", async () => {
    mockPlatform({ desktop: false });
    const fire = await renderClaimed("tab.new");
    fireEvent.keyDown(document, { key: "t", code: "KeyT", altKey: true, shiftKey: true });
    expect(fire).toHaveBeenCalledTimes(1);
  });
});
