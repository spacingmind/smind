import { act } from "react";
import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { RELAY_STATUS_POLL_MS, RelayConnectionStatus, useRelayStatus } from "@/components/settings/relay-connection-status";
import { desktop, type RelayStatus } from "@/lib/platform";

const DEVICE_ID = "abcdefghijklmnopqrstuvwxyz";

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe("RelayConnectionStatus", () => {
  it("tells a pending device exactly which command to run on the daemon's machine", () => {
    render(<RelayConnectionStatus status={{ state: "awaitingApproval", deviceId: DEVICE_ID }} />);
    const line = screen.getByTestId("connection-relay-status");
    expect(line).toHaveAttribute("data-state", "awaitingApproval");
    expect(line).toHaveAttribute("role", "status");
    expect(line).toHaveTextContent("Waiting for approval on the daemon");
    expect(line).toHaveTextContent(`smind relay devices approve ${DEVICE_ID}`);
    // The command is its own element: it is copied as one string and never split.
    expect(screen.getByTestId("connection-relay-command")).toHaveTextContent(`smind relay devices approve ${DEVICE_ID}`);
  });

  it("says a revoked device cannot connect and how to allow it again", () => {
    render(<RelayConnectionStatus status={{ state: "revoked", deviceId: DEVICE_ID }} />);
    const line = screen.getByTestId("connection-relay-status");
    expect(line).toHaveAttribute("data-state", "revoked");
    expect(line).toHaveTextContent("revoked on the daemon");
    expect(line).toHaveTextContent(`smind relay devices approve ${DEVICE_ID}`);
    expect(line).toHaveTextContent("remove this connection");
  });

  it("shows the daemon's refusal code and message for any other refusal", () => {
    render(<RelayConnectionStatus status={{ state: "refused", code: "too_many_devices", message: "too many connected devices" }} />);
    const line = screen.getByTestId("connection-relay-status");
    expect(line).toHaveTextContent("The daemon refused this device");
    expect(line).toHaveTextContent("(too_many_devices)");
    expect(line).toHaveTextContent("too many connected devices");
  });

  it.each([
    ["connected", "Connected through the relay"],
    ["connecting", "Connecting through the relay…"],
    ["reconnecting", "Reconnecting through the relay…"],
  ] as const)("renders the %s state quietly", (state, text) => {
    render(<RelayConnectionStatus status={{ state }} />);
    const line = screen.getByTestId("connection-relay-status");
    expect(line).toHaveAttribute("data-state", state);
    expect(line).toHaveTextContent(text);
    expect(line).not.toHaveAttribute("role");
  });
});

describe("useRelayStatus", () => {
  function Probe({ active }: { active: boolean }) {
    const s = useRelayStatus(active);
    return <span data-testid="state">{s ? s.state : "none"}</span>;
  }

  it("polls the desktop shell while active and follows state changes", async () => {
    vi.useFakeTimers();
    const states: RelayStatus[] = [{ state: "awaitingApproval", deviceId: DEVICE_ID }, { state: "connected" }];
    const spy = vi.spyOn(desktop, "relayStatus").mockImplementation(() => Promise.resolve(states.shift() ?? { state: "connected" }));
    render(<Probe active />);
    await act(async () => {
      await Promise.resolve();
    });
    expect(screen.getByTestId("state")).toHaveTextContent("awaitingApproval");
    await act(async () => {
      await vi.advanceTimersByTimeAsync(RELAY_STATUS_POLL_MS);
    });
    expect(screen.getByTestId("state")).toHaveTextContent("connected");
    expect(spy.mock.calls.length).toBeGreaterThanOrEqual(2);
  });

  it("does not poll when the selected connection is not a relay one", async () => {
    vi.useFakeTimers();
    const spy = vi.spyOn(desktop, "relayStatus").mockResolvedValue({ state: "connected" });
    render(<Probe active={false} />);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(RELAY_STATUS_POLL_MS * 3);
    });
    expect(spy).not.toHaveBeenCalled();
    expect(screen.getByTestId("state")).toHaveTextContent("none");
  });

  it("keeps the last state when a read fails", async () => {
    vi.useFakeTimers();
    let n = 0;
    vi.spyOn(desktop, "relayStatus").mockImplementation(() => (++n === 1 ? Promise.resolve({ state: "connected" }) : Promise.reject(new Error("boom"))));
    render(<Probe active />);
    await act(async () => {
      await Promise.resolve();
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(RELAY_STATUS_POLL_MS);
    });
    expect(screen.getByTestId("state")).toHaveTextContent("connected");
  });
});
