import { useState } from "react";
import { act, fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { ScratchSpaceGroup, formatScratchBytes } from "@/components/settings/scratch-space-group";
import { SettingsScreen } from "@/components/settings/settings-screen";
import { useScratchClean } from "@/hooks/use-scratch";
import { renderHookWithDaemon, renderWithDaemon } from "@/test/render-with-daemon";
import { FakeUsageDaemon } from "@/test/usage-test-client";

const LOADED = { totalBytes: 5 * 1024 * 1024, totalFiles: 42, dirs: 3, reclaimableDirs: 2, reclaimableBytes: 3 * 1024 * 1024 };

function setup(over: Partial<FakeUsageDaemon> = {}): FakeUsageDaemon {
  const d = new FakeUsageDaemon([]);
  Object.assign(d, over);
  renderWithDaemon(<ScratchSpaceGroup />, d);
  return d;
}

const calls = (d: FakeUsageDaemon, method: string) => d.calls.filter((c) => c.method === method).length;

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

describe("ScratchSpaceGroup", () => {
  it("empty: no task folders says so and offers no clean-up", async () => {
    setup();
    expect(await screen.findByTestId("scratch-space-empty")).toHaveTextContent("No scratch files");
    expect(screen.queryByTestId("scratch-space-clean")).not.toBeInTheDocument();
  });

  it("loading: skeleton rows inside the card (no inner box), announced once, then the numbers", async () => {
    let release = () => {};
    const gate = new Promise<void>((r) => (release = r));
    setup({ scratchUsage: LOADED, scratchUsageGate: gate });
    const loading = await screen.findByTestId("scratch-space-loading");
    expect(loading).toHaveTextContent("Loading scratch usage…");
    expect(loading.querySelector("[data-slot='loading-rows']")).toBeNull();
    expect(loading.querySelectorAll("[data-slot='settings-row']").length).toBe(2);
    await act(async () => release());
    expect(await screen.findByTestId("scratch-space-total")).toHaveTextContent("5.0 MiB");
    expect(screen.queryByTestId("scratch-space-loading")).not.toBeInTheDocument();
  });

  it("empty is a settings row like the loaded ones, not a nested card", async () => {
    setup();
    const empty = await screen.findByTestId("scratch-space-empty");
    expect(empty.getAttribute("data-slot")).toBe("settings-row");
    expect(empty.querySelector("[data-slot='settings-empty-card']")).toBeNull();
  });

  it("loaded: total, folders, files, reclaimable and a Clean up button", async () => {
    setup({ scratchUsage: LOADED });
    expect(await screen.findByTestId("scratch-space-total")).toHaveTextContent("5.0 MiB");
    expect(screen.getByTestId("scratch-space")).toHaveTextContent("3 task folders · 42 files");
    expect(screen.getByTestId("scratch-space-reclaimable")).toHaveTextContent("3.0 MiB");
    expect(screen.getByTestId("scratch-space-clean")).toBeEnabled();
  });

  it("nothing reclaimable keeps Clean up disabled", async () => {
    setup({ scratchUsage: { ...LOADED, reclaimableDirs: 0, reclaimableBytes: 0 } });
    await screen.findByTestId("scratch-space-total");
    expect(screen.getByTestId("scratch-space-clean")).toBeDisabled();
  });

  it("clean success: calls scratch.clean once, shows what was freed and re-reads the usage", async () => {
    const after = { ...LOADED, totalBytes: 2 * 1024 * 1024, totalFiles: 10, dirs: 1, reclaimableDirs: 0, reclaimableBytes: 0 };
    const d = setup({ scratchUsage: LOADED, scratchCleanResult: { files: 32, bytes: 3 * 1024 * 1024, dirs: 2 }, scratchAfterClean: after });
    fireEvent.click(await screen.findByTestId("scratch-space-clean"));
    expect(await screen.findByTestId("scratch-space-result")).toHaveTextContent("Freed 3.0 MiB (32 files).");
    expect(calls(d, "scratch.clean")).toBe(1);
    await waitFor(() => expect(calls(d, "scratch.usage")).toBe(2));
    await waitFor(() => expect(screen.getByTestId("scratch-space-total")).toHaveTextContent("2.0 MiB"));
    expect(screen.getByTestId("scratch-space-result")).not.toHaveTextContent("kept");
  });

  it("two quick clicks on Clean up send ONE scratch.clean (the pending state is set at once)", async () => {
    let release = () => {};
    const gate = new Promise<void>((r) => (release = r));
    const d = setup({ scratchUsage: LOADED, scratchCleanGate: gate });
    const button = await screen.findByTestId("scratch-space-clean");
    // Both clicks land before React re-renders the disabled button.
    await act(async () => {
      button.click();
      button.click();
    });
    expect(calls(d, "scratch.clean")).toBe(1);
    await act(async () => release());
  });

  it("the hook itself ignores a second clean() while one is in flight", async () => {
    let release = () => {};
    const gate = new Promise<void>((r) => (release = r));
    const d = new FakeUsageDaemon([]);
    d.scratchCleanGate = gate;
    const view = renderHookWithDaemon(() => useScratchClean(), d, { initialProps: undefined });
    await act(async () => {
      view.result.current.clean();
      view.result.current.clean();
    });
    expect(calls(d, "scratch.clean")).toBe(1);
    await act(async () => release());
    await act(async () => {
      view.result.current.clean(); // after it settled a new clean is allowed
    });
    expect(calls(d, "scratch.clean")).toBe(2);
  });

  it("clean in flight shows a spinner next to Cleaning…", async () => {
    let release = () => {};
    const gate = new Promise<void>((r) => (release = r));
    setup({ scratchUsage: LOADED, scratchCleanGate: gate });
    fireEvent.click(await screen.findByTestId("scratch-space-clean"));
    await flush();
    expect(screen.getByTestId("scratch-space-clean").querySelector("[data-testid='scratch-space-spinner']")).not.toBeNull();
    await act(async () => release());
    await waitFor(() => expect(screen.getByTestId("scratch-space-clean").querySelector("[data-testid='scratch-space-spinner']")).toBeNull());
  });

  it("clean in flight: the button says Cleaning… and is disabled", async () => {
    let release = () => {};
    const gate = new Promise<void>((r) => (release = r));
    setup({ scratchUsage: LOADED, scratchCleanGate: gate });
    fireEvent.click(await screen.findByTestId("scratch-space-clean"));
    await flush();
    expect(screen.getByTestId("scratch-space-clean")).toHaveTextContent("Cleaning…");
    expect(screen.getByTestId("scratch-space-clean")).toBeDisabled();
    await act(async () => release());
  });

  it("busy refusal: the daemon's own words are shown, the button works again and the usage is re-read", async () => {
    const d = setup({ scratchUsage: LOADED, scratchCleanError: new Error("scratch: task has a running run") });
    fireEvent.click(await screen.findByTestId("scratch-space-clean"));
    expect(await screen.findByTestId("scratch-space-clean-error")).toHaveTextContent("scratch: task has a running run");
    expect(screen.queryByTestId("scratch-space-result")).not.toBeInTheDocument();
    await waitFor(() => expect(calls(d, "scratch.usage")).toBe(2));
    expect(screen.getByTestId("scratch-space-clean")).toBeEnabled();
  });

  it("sweep skipped a busy task's folder: freed what it could and says what was kept", async () => {
    const after = { ...LOADED, totalBytes: 3 * 1024 * 1024, dirs: 1, reclaimableDirs: 1, reclaimableBytes: 1024 * 1024 };
    setup({ scratchUsage: LOADED, scratchCleanResult: { files: 20, bytes: 2 * 1024 * 1024, dirs: 2 }, scratchAfterClean: after });
    fireEvent.click(await screen.findByTestId("scratch-space-clean"));
    const result = await screen.findByTestId("scratch-space-result");
    await waitFor(() => expect(result).toHaveTextContent("1 task folder was kept because a run is still using it"));
    expect(result).toHaveTextContent("Freed 2.0 MiB (20 files).");
  });

  it("opening the card again re-reads the usage (a task may have closed meanwhile)", async () => {
    const d = new FakeUsageDaemon([]);
    d.scratchUsage = LOADED;
    function Toggle() {
      const [on, setOn] = useState(true);
      return (
        <>
          <button onClick={() => setOn((v) => !v)}>toggle</button>
          {on && <ScratchSpaceGroup />}
        </>
      );
    }
    renderWithDaemon(<Toggle />, d);
    await screen.findByTestId("scratch-space-total");
    expect(calls(d, "scratch.usage")).toBe(1);
    fireEvent.click(screen.getByText("toggle")); // close
    fireEvent.click(screen.getByText("toggle")); // open again
    await waitFor(() => expect(calls(d, "scratch.usage")).toBe(2));
  });

  it("a read error shows the daemon's text with Retry, which re-reads", async () => {
    const d = setup({ scratchUsageError: new Error("scratch.usage: permission denied") });
    expect(await screen.findByTestId("scratch-space-error")).toHaveTextContent("scratch.usage: permission denied");
    d.scratchUsageError = null;
    d.scratchUsage = LOADED;
    fireEvent.click(screen.getByTestId("scratch-space-retry"));
    expect(await screen.findByTestId("scratch-space-total")).toHaveTextContent("5.0 MiB");
    expect(calls(d, "scratch.usage")).toBe(2);
  });
});

describe("Settings > General", () => {
  it("lists the Scratch space card under General", async () => {
    const d = new FakeUsageDaemon([]);
    d.scratchUsage = LOADED;
    renderWithDaemon(<SettingsScreen client={d.client as never} onNavigateBack={() => {}} />, d);
    const general = await screen.findByTestId("settings-section-general");
    expect(general).toContainElement(await screen.findByTestId("scratch-space"));
    expect(await screen.findByTestId("scratch-space-total")).toHaveTextContent("5.0 MiB");
  });
});

describe("formatScratchBytes", () => {
  it("units: bytes, one decimal below 10, whole from there, capped at TiB; junk is 0 B", () => {
    expect(formatScratchBytes(0)).toBe("0 B");
    expect(formatScratchBytes(-5)).toBe("0 B");
    expect(formatScratchBytes(Number.NaN)).toBe("0 B");
    expect(formatScratchBytes(512)).toBe("512 B");
    expect(formatScratchBytes(2048)).toBe("2.0 KiB");
    expect(formatScratchBytes(10 * 1024 * 1024)).toBe("10 MiB");
    expect(formatScratchBytes(1.5 * 1024 ** 3)).toBe("1.5 GiB");
    expect(formatScratchBytes(5000 * 1024 ** 4)).toBe("5000 TiB");
  });
});
