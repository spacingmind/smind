import { StrictMode, act } from "react";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { AppUpdateCheck, AppUpdateProgress } from "@/lib/platform";

// `@/lib/platform` is mocked per scenario (`isDesktop` is a module-load
// constant), the components imported fresh, like desktop-daemon-banner.test.tsx.
const AVAILABLE: AppUpdateCheck = {
  kind: "available",
  version: "0.11.0",
  currentVersion: "0.10.0",
  notes: "- Fixed a bug\n- Added another",
  date: "2026-10-01T00:00:00Z",
};

interface Mocks {
  check: ReturnType<typeof vi.fn>;
  install: ReturnType<typeof vi.fn>;
  manualCheck: () => void;
  emitProgress: (p: AppUpdateProgress) => void;
}

const openExternalMock = vi.fn(() => Promise.resolve());

function mockPlatform(opts: { check: () => Promise<AppUpdateCheck>; install?: () => Promise<void>; isDesktop?: boolean }): Mocks {
  const check = vi.fn(opts.check);
  const install = vi.fn(opts.install ?? (() => new Promise<void>(() => {})));
  let requestCb: () => void = () => {};
  let progressCb: (p: AppUpdateProgress) => void = () => {};
  vi.doMock("@/lib/platform", () => ({
    isDesktop: opts.isDesktop ?? true,
    desktop: {
      appUpdateCheck: check,
      appUpdateInstall: install,
      openExternal: openExternalMock,
      onAppUpdateCheckRequested: (cb: () => void) => {
        requestCb = cb;
        return () => {};
      },
      onAppUpdateProgress: (cb: (p: AppUpdateProgress) => void) => {
        progressCb = cb;
        return () => {};
      },
    },
  }));
  return {
    check,
    install,
    manualCheck: () => act(() => requestCb()),
    emitProgress: (p: AppUpdateProgress) => act(() => progressCb(p)),
  };
}

async function flush(): Promise<void> {
  await act(async () => {
    for (let i = 0; i < 6; i++) await Promise.resolve();
  });
}

async function mountDialog(opts: { strictMode?: boolean } = {}): Promise<void> {
  const { DesktopUpdateDialog } = await import("@/components/desktop-update-dialog");
  const { Toaster } = await import("@/components/ui/toast");
  const tree = (
    <>
      <Toaster />
      <DesktopUpdateDialog />
    </>
  );
  render(opts.strictMode ? <StrictMode>{tree}</StrictMode> : tree);
  await flush();
}

/** Dismisses the prompt, then asks again from the menu so the next scenario starts from a fresh prompt. */
async function dismissAndReprompt(mocks: Mocks): Promise<void> {
  fireEvent.click(screen.getByTestId("desktop-update-dialog-later"));
  await flush();
  expect(screen.queryByTestId("desktop-update-dialog")).not.toBeInTheDocument();
  mocks.manualCheck();
  await flush();
  expect(screen.getByTestId("desktop-update-dialog")).toBeInTheDocument();
}

afterEach(async () => {
  const { clearToasts } = await import("@/components/ui/toast");
  clearToasts();
  cleanup();
  vi.resetModules();
  vi.restoreAllMocks();
  vi.doUnmock("@/lib/platform");
});

describe("DesktopUpdateDialog", () => {
  it("update-launch-check-runs-once", async () => {
    const mocks = mockPlatform({ check: () => Promise.resolve(AVAILABLE) });
    // StrictMode double-invokes effects on one mount; a re-render must not add calls either.
    await mountDialog({ strictMode: true });
    await flush();
    expect(mocks.check).toHaveBeenCalledTimes(1);
  });

  it("update-launch-check-error-is-silent", async () => {
    mockPlatform({ check: () => Promise.reject(new Error("offline")) });
    const { DesktopUpdateDialog } = await import("@/components/desktop-update-dialog");
    const { container } = render(<DesktopUpdateDialog />);
    await flush();
    expect(container).toBeEmptyDOMElement();
    expect(screen.queryByTestId("toast-host")).not.toBeInTheDocument();
  });

  it("update-launch-check-up-to-date-silent", async () => {
    mockPlatform({ check: () => Promise.resolve({ kind: "upToDate", currentVersion: "0.10.0" }) });
    const { DesktopUpdateDialog } = await import("@/components/desktop-update-dialog");
    const { container } = render(<DesktopUpdateDialog />);
    await flush();
    expect(container).toBeEmptyDOMElement();
  });

  it("update-prompt-shows-version-and-notes", async () => {
    mockPlatform({ check: () => Promise.resolve(AVAILABLE) });
    await mountDialog();
    expect(screen.getByText("Update available")).toBeInTheDocument();
    expect(screen.getByText(/smind v0\.11\.0 is available \(you have v0\.10\.0\)/)).toBeInTheDocument();
    const notes = screen.getByTestId("desktop-update-dialog-notes");
    expect(notes).toHaveTextContent("Fixed a bug");
    expect(notes.querySelectorAll("li")).toHaveLength(2);
  });

  it("update notes render release-please markdown without the version heading or commit links; links open externally", async () => {
    const notes = [
      "## [0.11.0](https://github.com/spacingmind/smind/compare/v0.10.0...v0.11.0) (2026-10-13)",
      "",
      "### Features",
      "",
      "* skills ([#279](https://github.com/spacingmind/smind/issues/279)) ([90469fb](https://github.com/spacingmind/smind/commit/90469fbc3b1eb78280108cee6dd8fb67dbc833dd))",
    ].join("\n");
    mockPlatform({ check: () => Promise.resolve({ ...AVAILABLE, notes }) });
    await mountDialog();
    const el = screen.getByTestId("desktop-update-dialog-notes");
    expect(el.querySelector("h3")).toHaveTextContent("Features");
    expect(el).not.toHaveTextContent("## [0.11.0]");
    expect(el).not.toHaveTextContent("90469fb");
    const link = within(el).getByRole("link", { name: "#279" });
    fireEvent.click(link);
    expect(openExternalMock).toHaveBeenCalledWith("https://github.com/spacingmind/smind/issues/279");
  });

  it("null notes show 'No release notes.'", async () => {
    mockPlatform({ check: () => Promise.resolve({ ...AVAILABLE, notes: null }) });
    await mountDialog();
    expect(screen.getByTestId("desktop-update-dialog-notes")).toHaveTextContent("No release notes.");
  });

  it("update-nothing-installed-before-confirm", async () => {
    const mocks = mockPlatform({ check: () => Promise.resolve(AVAILABLE) });
    await mountDialog();
    expect(mocks.install).not.toHaveBeenCalled();
    fireEvent.click(screen.getByTestId("desktop-update-dialog-confirm"));
    await waitFor(() => expect(mocks.install).toHaveBeenCalledTimes(1));
  });

  it("update-later-dismisses", async () => {
    const mocks = mockPlatform({ check: () => Promise.resolve(AVAILABLE) });
    await mountDialog();

    // Later
    fireEvent.click(screen.getByTestId("desktop-update-dialog-later"));
    await flush();
    expect(screen.queryByTestId("desktop-update-dialog")).not.toBeInTheDocument();

    // Esc (on a fresh prompt)
    mocks.manualCheck();
    await flush();
    expect(screen.getByTestId("desktop-update-dialog")).toBeInTheDocument();
    fireEvent.keyDown(screen.getByTestId("desktop-update-dialog"), { key: "Escape" });
    await flush();
    expect(screen.queryByTestId("desktop-update-dialog")).not.toBeInTheDocument();

    // The dialog's close button (on a fresh prompt)
    mocks.manualCheck();
    await flush();
    expect(screen.getByTestId("desktop-update-dialog")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    await flush();
    expect(screen.queryByTestId("desktop-update-dialog")).not.toBeInTheDocument();

    // No install from any of the three; the two re-prompts came from the menu.
    expect(mocks.install).not.toHaveBeenCalled();
    expect(mocks.check).toHaveBeenCalledTimes(3);
  });

  it("update-manual-check-reprompts-after-later", async () => {
    const mocks = mockPlatform({ check: () => Promise.resolve(AVAILABLE) });
    await mountDialog();
    await dismissAndReprompt(mocks);
    expect(mocks.check).toHaveBeenCalledTimes(2);
  });

  it("update-manual-check-up-to-date-toast", async () => {
    const mocks = mockPlatform({ check: () => Promise.resolve({ kind: "upToDate", currentVersion: "0.10.0" }) });
    await mountDialog();
    mocks.manualCheck();
    await flush();
    expect(await screen.findByText("smind is up to date (v0.10.0)")).toBeInTheDocument();
    expect(screen.queryByTestId("desktop-update-dialog")).not.toBeInTheDocument();
  });

  it("update-manual-check-error-toast", async () => {
    let call = 0;
    const mocks = mockPlatform({
      check: () => {
        call += 1;
        return call === 1 ? Promise.resolve({ kind: "upToDate" as const, currentVersion: "0.10.0" }) : Promise.reject(new Error("network down"));
      },
    });
    await mountDialog();
    mocks.manualCheck();
    await flush();
    expect(await screen.findByText("network down")).toBeInTheDocument();
    expect(screen.getByText("Update check failed")).toBeInTheDocument();
    expect(screen.queryByTestId("desktop-update-dialog")).not.toBeInTheDocument();
  });

  it("update-install-shows-progress", async () => {
    const mocks = mockPlatform({ check: () => Promise.resolve(AVAILABLE) });
    await mountDialog();
    fireEvent.click(screen.getByTestId("desktop-update-dialog-confirm"));
    await waitFor(() => expect(mocks.install).toHaveBeenCalledTimes(1));

    // Indeterminate until a total arrives.
    expect(screen.getByTestId("desktop-update-dialog-indeterminate")).toBeInTheDocument();

    mocks.emitProgress({ stage: "downloading", downloaded: 250, total: 500 });
    await flush();
    expect(screen.getByTestId("desktop-update-dialog-percent")).toHaveTextContent("50%");

    mocks.emitProgress({ stage: "installing" });
    await flush();
    expect(screen.getByText("Installing…")).toBeInTheDocument();
  });

  it("update-install-error-recoverable", async () => {
    let failures = 0;
    const mocks = mockPlatform({
      check: () => Promise.resolve(AVAILABLE),
      install: () => {
        failures += 1;
        return failures === 1 ? Promise.reject(new Error("signature verification failed")) : new Promise<void>(() => {});
      },
    });
    await mountDialog();
    fireEvent.click(screen.getByTestId("desktop-update-dialog-confirm"));
    await flush();

    // The error inline plus an error toast.
    expect(await screen.findByTestId("desktop-update-dialog-error")).toHaveTextContent("signature verification failed");
    expect(screen.getAllByText("signature verification failed").length).toBeGreaterThanOrEqual(2);

    // Retry: Update calls install a second time.
    fireEvent.click(screen.getByTestId("desktop-update-dialog-confirm"));
    await waitFor(() => expect(mocks.install).toHaveBeenCalledTimes(2));

    // Later still closes.
    fireEvent.click(screen.getByTestId("desktop-update-dialog-later"));
    await flush();
    expect(screen.queryByTestId("desktop-update-dialog")).not.toBeInTheDocument();
  });

  it("update-unsupported-renders-nothing", async () => {
    // `unsupported` in a desktop build: no dialog, no toast, no install.
    const mocks = mockPlatform({ check: () => Promise.resolve({ kind: "unsupported" }) });
    await mountDialog();
    expect(screen.queryByTestId("desktop-update-dialog")).not.toBeInTheDocument();
    mocks.manualCheck();
    await flush();
    expect(mocks.check).toHaveBeenCalledTimes(2);
    expect(screen.queryByTestId("toast-host")).not.toBeInTheDocument();
    expect(mocks.install).not.toHaveBeenCalled();

    // The browser build: nothing is called at all.
    cleanup();
    vi.resetModules();
    const browserMocks = mockPlatform({ check: () => Promise.resolve(AVAILABLE), isDesktop: false });
    await mountDialog();
    expect(browserMocks.check).not.toHaveBeenCalled();
    expect(browserMocks.install).not.toHaveBeenCalled();
  });

  it("dismissing during download only hides it", async () => {
    const mocks = mockPlatform({ check: () => Promise.resolve(AVAILABLE) });
    await mountDialog();
    fireEvent.click(screen.getByTestId("desktop-update-dialog-confirm"));
    await waitFor(() => expect(mocks.install).toHaveBeenCalledTimes(1));
    fireEvent.click(screen.getByTestId("desktop-update-dialog-later"));
    await flush();
    expect(screen.queryByTestId("desktop-update-dialog")).not.toBeInTheDocument();
  });
});
