import { afterEach, describe, expect, it, vi } from "vitest";

// Like desktop-webview.test.ts / platform.test.ts: the install entry point
// gates on lib/platform.ts's build-time isDesktop, so each scenario stubs
// the env var and re-imports the module fresh.

afterEach(() => {
  vi.unstubAllEnvs();
  vi.resetModules();
});

/** Appends target to the body, dispatches a cancelable bubbling contextmenu on it, and returns the event. */
function contextMenuOn(target: HTMLElement): MouseEvent {
  document.body.appendChild(target);
  const event = new MouseEvent("contextmenu", { bubbles: true, cancelable: true });
  target.dispatchEvent(event);
  return event;
}

describe("desktop-context-menu (D2.2/D2.8)", () => {
  it("install is a no-op in the browser build, installs the policy in the desktop build", async () => {
    vi.stubEnv("VITE_SMIND_DESKTOP", "");
    const web = await import("@/lib/desktop-context-menu");
    web.installDesktopContextMenu();
    expect(contextMenuOn(document.createElement("div")).defaultPrevented).toBe(false);

    vi.stubEnv("VITE_SMIND_DESKTOP", "1");
    vi.resetModules();
    const desktop = await import("@/lib/desktop-context-menu");
    desktop.installDesktopContextMenu();
    try {
      expect(contextMenuOn(document.createElement("div")).defaultPrevented).toBe(true);
    } finally {
      document.removeEventListener("contextmenu", desktop.handleContextMenu);
    }
  });

  it("context-menu-suppressed-on-chrome: plain chrome prevents the default menu", async () => {
    vi.stubEnv("VITE_SMIND_DESKTOP", "1");
    const { handleContextMenu } = await import("@/lib/desktop-context-menu");

    const sidebarRow = document.createElement("div");
    sidebarRow.setAttribute("data-testid", "sidebar-task-row");
    const rowEvent = contextMenuOn(sidebarRow);
    handleContextMenu(rowEvent);
    expect(rowEvent.defaultPrevented).toBe(true);

    const paneSpace = document.createElement("div");
    paneSpace.setAttribute("data-testid", "pane-tab-strip");
    const paneEvent = contextMenuOn(paneSpace);
    handleContextMenu(paneEvent);
    expect(paneEvent.defaultPrevented).toBe(true);
  });

  it("context-menu-allowed-in-editable: input, textarea, contenteditable and the file editor keep the native menu", async () => {
    vi.stubEnv("VITE_SMIND_DESKTOP", "1");
    const { handleContextMenu } = await import("@/lib/desktop-context-menu");

    for (const selector of ["input", "textarea"]) {
      const el = document.createElement(selector);
      const event = contextMenuOn(el);
      handleContextMenu(event);
      expect(event.defaultPrevented).toBe(false);
    }

    const editable = document.createElement("div");
    editable.setAttribute("contenteditable", "true");
    const editableEvent = contextMenuOn(editable);
    handleContextMenu(editableEvent);
    expect(editableEvent.defaultPrevented).toBe(false);

    // The file editor: CodeMirror's contenteditable .cm-content.
    const cm = document.createElement("div");
    const cmContent = document.createElement("div");
    cmContent.setAttribute("contenteditable", "true");
    cmContent.className = "cm-content";
    cm.appendChild(cmContent);
    document.body.appendChild(cm);
    const cmEvent = new MouseEvent("contextmenu", { bubbles: true, cancelable: true });
    cmContent.dispatchEvent(cmEvent);
    handleContextMenu(cmEvent);
    expect(cmEvent.defaultPrevented).toBe(false);
  });

  it("context-menu-allowed-in-editable: a non-empty selection keeps the native menu, an empty one does not", async () => {
    vi.stubEnv("VITE_SMIND_DESKTOP", "1");
    const { handleContextMenu } = await import("@/lib/desktop-context-menu");

    const text = document.createElement("span");
    text.textContent = "conversation text";
    // Append before selecting: jsdom clears the selection when the
    // selected node is moved (which contextMenuOn's appendChild would do).
    document.body.appendChild(text);
    const range = document.createRange();
    range.selectNodeContents(text);
    const selection = window.getSelection()!;
    selection.removeAllRanges();
    selection.addRange(range);
    const selectedEvent = new MouseEvent("contextmenu", { bubbles: true, cancelable: true });
    text.dispatchEvent(selectedEvent);
    handleContextMenu(selectedEvent);
    expect(selectedEvent.defaultPrevented).toBe(false);

    selection.removeAllRanges();
    const noneEvent = new MouseEvent("contextmenu", { bubbles: true, cancelable: true });
    text.dispatchEvent(noneEvent);
    handleContextMenu(noneEvent);
    expect(noneEvent.defaultPrevented).toBe(true);
  });

  it("context-menu-own-menu-still-opens: a handler that already ran (e.g. a smind ContextMenu's) is not re-prevented or blocked", async () => {
    vi.stubEnv("VITE_SMIND_DESKTOP", "1");
    const { handleContextMenu } = await import("@/lib/desktop-context-menu");

    // What Radix's ContextMenuTrigger does on a file row: its own
    // contextmenu handler runs first (capture-to-bubble order puts the
    // row's own listener ahead of the document's) and opens the menu.
    const row = document.createElement("div");
    row.setAttribute("data-testid", "file-row");
    const rowHandler = vi.fn((e: Event) => e.preventDefault());
    row.addEventListener("contextmenu", rowHandler);

    const { installDesktopContextMenu } = await import("@/lib/desktop-context-menu");
    installDesktopContextMenu();
    try {
      const event = contextMenuOn(row);
      expect(rowHandler).toHaveBeenCalledTimes(1);
      expect(event.defaultPrevented).toBe(true); // by the row's own handler; the menu opened

      // And the document-level policy is a no-op for an already-prevented
      // event -- no double handling.
      const alreadyPrevented = new MouseEvent("contextmenu", { cancelable: true });
      alreadyPrevented.preventDefault();
      const before = alreadyPrevented.defaultPrevented;
      handleContextMenu(alreadyPrevented);
      expect(alreadyPrevented.defaultPrevented).toBe(before);
    } finally {
      document.removeEventListener("contextmenu", handleContextMenu);
    }
  });
});
