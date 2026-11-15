// A task window holds no updater permission: useAppUpdate must not check, listen or install there. `vi.doMock` of the
// platform and task-window modules, so this lives in its own file.
import { renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

afterEach(() => {
  vi.resetModules();
  vi.doUnmock("@/lib/platform");
  vi.doUnmock("@/lib/task-window");
});

async function mountHook(taskWindow: boolean) {
  const desktop = {
    appUpdateCheck: vi.fn(() => Promise.resolve({ kind: "upToDate", currentVersion: "1.0.0" })),
    appUpdateInstall: vi.fn(() => Promise.resolve()),
    onAppUpdateCheckRequested: vi.fn(() => () => {}),
    onAppUpdateProgress: vi.fn(() => () => {}),
  };
  vi.doMock("@/lib/platform", () => ({ isDesktop: true, desktop }));
  vi.doMock("@/lib/task-window", () => ({ isTaskWindow: taskWindow }));
  vi.doMock("@/components/ui/toast", () => ({ toast: vi.fn() }));
  const { useAppUpdate } = await import("@/lib/app-update");
  const view = renderHook(() => useAppUpdate());
  return { desktop, view };
}

describe("useAppUpdate in a task window", () => {
  it("never calls the updater commands and subscribes to nothing", async () => {
    const { desktop, view } = await mountHook(true);
    view.result.current.confirm();
    expect(desktop.appUpdateCheck).not.toHaveBeenCalled();
    expect(desktop.appUpdateInstall).not.toHaveBeenCalled();
    expect(desktop.onAppUpdateCheckRequested).not.toHaveBeenCalled();
    expect(desktop.onAppUpdateProgress).not.toHaveBeenCalled();
  });

  it("control: in the main window the same hook checks once and subscribes", async () => {
    const { desktop } = await mountHook(false);
    expect(desktop.appUpdateCheck).toHaveBeenCalledTimes(1);
    expect(desktop.onAppUpdateCheckRequested).toHaveBeenCalledTimes(1);
    expect(desktop.onAppUpdateProgress).toHaveBeenCalledTimes(1);
  });
});
