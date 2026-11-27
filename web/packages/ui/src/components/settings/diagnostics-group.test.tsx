import { act } from "react";
import { fireEvent, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { DiagnosticsGroup } from "@/components/settings/diagnostics-group";
import type { DoctorSessions } from "@/hooks/use-doctor-sessions";
import { renderWithDaemon } from "@/test/render-with-daemon";
import { FakeUsageDaemon } from "@/test/usage-test-client";

// The relay test needs a desktop build whose current connection is a relay one; every other test stays
// on the web build, where the hook is ready at once and never touches `desktop`.
const platformState = vi.hoisted(() => ({
  desktop: false,
  connection: { id: "local", kind: "local", label: "Local", baseUrl: "http://127.0.0.1:4648" },
}));
vi.mock("@/lib/platform", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/platform")>();
  return {
    ...actual,
    get isDesktop() {
      return platformState.desktop;
    },
    get desktop() {
      return { ...actual.desktop, getCurrentConnection: () => Promise.resolve(platformState.connection) };
    },
  };
});

function resetPlatform() {
  platformState.desktop = false;
  platformState.connection = { id: "local", kind: "local", label: "Local", baseUrl: "http://127.0.0.1:4648" };
}

afterEach(() => {
  resetPlatform();
});

const COUNTS: DoctorSessions = { runs: 12, withRef: 7, readable: 5, tooLarge: 2, gone: 1, checked: 6 };

/** Wraps the fake daemon's client so `doctor.sessions` is answered by `answer` and every call is recorded. */
function doctorDaemon(answer: () => Promise<DoctorSessions>) {
  const daemon = new FakeUsageDaemon([]);
  const log: { method: string; params: unknown }[] = [];
  const holder = daemon.client as { call: (m: string, p?: unknown) => Promise<unknown> };
  const inner = holder.call.bind(holder);
  holder.call = async (method, params) => {
    if (method === "doctor.sessions") {
      log.push({ method, params });
      return answer();
    }
    return inner(method, params);
  };
  return {
    daemon,
    count: () => log.filter((c) => c.method === "doctor.sessions").length,
    params: () => log.map((c) => c.params),
  };
}

async function flush(): Promise<void> {
  await act(async () => {
    for (let i = 0; i < 12; i++) await Promise.resolve();
  });
}

async function click(el: HTMLElement): Promise<void> {
  fireEvent.click(el);
  await flush();
}

async function renderGroup(daemon: FakeUsageDaemon) {
  const view = renderWithDaemon(<DiagnosticsGroup />, daemon);
  await flush();
  return view;
}

describe("settings diagnostics group", () => {
  it("doctor_runs_once_per_click", async () => {
    const d = doctorDaemon(() => Promise.resolve({ ...COUNTS }));
    await renderGroup(d.daemon);
    expect(d.count()).toBe(0);

    await click(screen.getByTestId("doctor-run"));
    expect(d.count()).toBe(1);
    expect(d.params()[0]).toEqual({});

    await click(screen.getByTestId("doctor-run"));
    expect(d.count()).toBe(2);
    expect(d.params()).toEqual([{}, {}]);
  });

  it("doctor_shows_the_six_counts", async () => {
    const d = doctorDaemon(() => Promise.resolve({ ...COUNTS }));
    await renderGroup(d.daemon);
    await click(screen.getByTestId("doctor-run"));

    const rows: [string, string, number][] = [
      ["doctor-runs", "Finished runs", 12],
      ["doctor-withref", "With a provider session", 7],
      ["doctor-readable", "Readable", 5],
      ["doctor-toolarge", "Too large to read", 2],
      ["doctor-gone", "No longer on disk", 1],
      ["doctor-checked", "Checked", 6],
    ];
    for (const [id, label, value] of rows) {
      expect(screen.getByTestId(id)).toHaveTextContent(label);
      expect(screen.getByTestId(id)).toHaveTextContent(String(value));
    }
    expect(screen.queryByTestId("doctor-idle")).toBeNull();
  });

  it("doctor_error_is_a_sentence", async () => {
    const d = doctorDaemon(() => Promise.reject(new Error("doctor.sessions: store: disk is full")));
    await renderGroup(d.daemon);
    await click(screen.getByTestId("doctor-run"));

    const error = screen.getByTestId("doctor-error");
    expect(error).toHaveTextContent("Disk is full.");
    expect(error.textContent).not.toContain("doctor.sessions");
    expect(error.textContent).not.toContain("store");
    expect(screen.getByTestId("doctor-run")).toBeEnabled();
  });

  it("doctor_does_not_poll", async () => {
    vi.useFakeTimers();
    try {
      const d = doctorDaemon(() => Promise.resolve({ ...COUNTS }));
      await renderGroup(d.daemon);
      await click(screen.getByTestId("doctor-run"));
      expect(d.count()).toBe(1);
      expect(screen.getByTestId("doctor-result")).toBeInTheDocument();

      await act(async () => {
        await vi.advanceTimersByTimeAsync(10 * 60 * 1000);
      });
      expect(d.count()).toBe(1);
    } finally {
      vi.useRealTimers();
    }
  });

  it("doctor_button_disabled_while_running", async () => {
    const d = doctorDaemon(() => new Promise<DoctorSessions>(() => {}));
    await renderGroup(d.daemon);

    await click(screen.getByTestId("doctor-run"));
    const button = screen.getByTestId("doctor-run");
    expect(button).toBeDisabled();
    expect(button).toHaveTextContent("Running...");
    expect(d.count()).toBe(1);
  });

  it("doctor_relay_connection_is_read_only_and_makes_no_call", async () => {
    platformState.desktop = true;
    platformState.connection = { id: "relay:1", kind: "relay", label: "Relay", baseUrl: "wss://relay.example" };
    const d = doctorDaemon(() => Promise.resolve({ ...COUNTS }));
    await renderGroup(d.daemon);

    expect(await screen.findByTestId("doctor-read-only")).toHaveTextContent("Run diagnostics on the daemon's machine.");
    expect(screen.queryByTestId("doctor-run")).toBeNull();
    expect(screen.queryByTestId("doctor-idle")).toBeNull();
    expect(d.count()).toBe(0);
  });
});
