import { fireEvent } from "@testing-library/react";
import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { useQuickOpenShortcut } from "@/hooks/use-quick-open-shortcut";

afterEach(() => {
  vi.unstubAllGlobals();
});

function pressCtrlP(): void {
  fireEvent.keyDown(document, { key: "p", ctrlKey: true });
}

function pressCmdP(): void {
  fireEvent.keyDown(document, { key: "p", metaKey: true });
}

describe("useQuickOpenShortcut", () => {
  it("fires on Ctrl+P on a non-mac platform, and prevents the browser's own Print shortcut", () => {
    vi.stubGlobal("navigator", { ...navigator, platform: "Linux x86_64" });
    const onTrigger = vi.fn();
    renderHook(() => useQuickOpenShortcut(onTrigger));

    const event = new KeyboardEvent("keydown", { key: "p", ctrlKey: true, cancelable: true });
    act(() => {
      document.dispatchEvent(event);
    });

    expect(onTrigger).toHaveBeenCalledTimes(1);
    expect(event.defaultPrevented).toBe(true);
  });

  it("fires on Cmd+P (not Ctrl+P) on a mac platform", () => {
    vi.stubGlobal("navigator", { ...navigator, platform: "MacIntel" });
    const onTrigger = vi.fn();
    renderHook(() => useQuickOpenShortcut(onTrigger));

    pressCtrlP();
    expect(onTrigger).not.toHaveBeenCalled();

    pressCmdP();
    expect(onTrigger).toHaveBeenCalledTimes(1);
  });

  it("does not fire while disabled", () => {
    vi.stubGlobal("navigator", { ...navigator, platform: "Linux x86_64" });
    const onTrigger = vi.fn();
    renderHook(({ enabled }) => useQuickOpenShortcut(onTrigger, enabled), { initialProps: { enabled: false } });

    pressCtrlP();
    expect(onTrigger).not.toHaveBeenCalled();
  });

  it("stops listening on unmount", () => {
    vi.stubGlobal("navigator", { ...navigator, platform: "Linux x86_64" });
    const onTrigger = vi.fn();
    const { unmount } = renderHook(() => useQuickOpenShortcut(onTrigger));

    unmount();
    pressCtrlP();

    expect(onTrigger).not.toHaveBeenCalled();
  });

  it("ignores a bare 'p' with no modifier", () => {
    vi.stubGlobal("navigator", { ...navigator, platform: "Linux x86_64" });
    const onTrigger = vi.fn();
    renderHook(() => useQuickOpenShortcut(onTrigger));

    fireEvent.keyDown(document, { key: "p" });
    expect(onTrigger).not.toHaveBeenCalled();
  });
});
