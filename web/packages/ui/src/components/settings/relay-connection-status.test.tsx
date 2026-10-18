import { act } from "react";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
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

  it("says plainly when the device key cannot be read, instead of blaming the daemon", () => {
    render(
      <RelayConnectionStatus
        status={{ state: "refused", code: "device_key_unavailable", message: "remove this connection and add the link again" }}
      />,
    );
    const line = screen.getByTestId("connection-relay-status");
    expect(line).toHaveTextContent("This device's key cannot be read.");
    expect(line).not.toHaveTextContent("The daemon refused");
    expect(line).toHaveTextContent("remove this connection and add the link again");
  });

  it("tells an outdated daemon apart from a refusal", () => {
    render(
      <RelayConnectionStatus
        status={{ state: "refused", code: "daemon_outdated", message: "The daemon did not send a device challenge: update it." }}
      />,
    );
    const line = screen.getByTestId("connection-relay-status");
    expect(line).toHaveTextContent("different versions");
    expect(line).not.toHaveTextContent("The daemon refused");
    expect(line).toHaveTextContent("update it");
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

describe("daemon password prompt (ADR-0031 P13c)", () => {
  it("asks for the daemon password when the relay host requires it and never stores it", async () => {
    const setItem = vi.spyOn(Storage.prototype, "setItem");
    const set = vi.spyOn(desktop, "relaySetPassword").mockResolvedValue(undefined);
    render(<RelayConnectionStatus status={{ state: "passwordRequired" }} connectionId="relay:h1@r:7400" />);
    const line = screen.getByTestId("connection-relay-status");
    expect(line).toHaveAttribute("data-state", "passwordRequired");
    expect(line).toHaveTextContent("This daemon needs its password.");
    const input = screen.getByTestId("relay-password-input") as HTMLInputElement;
    expect(input).toHaveAttribute("type", "password");
    expect(input).toHaveAttribute("autocomplete", "off");
    expect(screen.getByTestId("relay-password-submit")).toBeDisabled();

    fireEvent.change(input, { target: { value: "correct horse battery" } });
    fireEvent.click(screen.getByTestId("relay-password-submit"));
    expect(set).toHaveBeenCalledTimes(1);
    expect(set).toHaveBeenCalledWith("relay:h1@r:7400", "correct horse battery");
    // Gone from the field at once, and written to no web storage.
    await waitFor(() => expect(input.value).toBe(""));
    expect(setItem).not.toHaveBeenCalled();
    expect(JSON.stringify({ ...localStorage })).not.toContain("correct horse");
    expect(JSON.stringify({ ...sessionStorage })).not.toContain("correct horse");
    expect(document.body.innerHTML).not.toContain("correct horse");
  });

  it("clears a previous connection's state at once when the selected connection changes", async () => {
    vi.useFakeTimers();
    vi.spyOn(desktop, "relayStatus")
      .mockResolvedValueOnce({ state: "passwordRequired" })
      .mockImplementation(() => new Promise<never>(() => {})); // the new connection's first read never lands during the test
    function Probe({ id }: { id: string }) {
      const s = useRelayStatus(true, id);
      return <span data-testid="state">{s ? s.state : "none"}</span>;
    }
    const { rerender } = render(<Probe id="a" />);
    await act(async () => {
      await Promise.resolve();
    });
    expect(screen.getByTestId("state")).toHaveTextContent("passwordRequired");
    rerender(<Probe id="b" />);
    expect(screen.getByTestId("state")).toHaveTextContent("none"); // not a stale prompt for the other relay
  });

  it("says a rejected password was not accepted and asks again", () => {
    render(<RelayConnectionStatus status={{ state: "incorrectPassword" }} />);
    const line = screen.getByTestId("connection-relay-status");
    expect(line).toHaveAttribute("data-state", "incorrectPassword");
    expect(line).toHaveTextContent("That password was not accepted.");
    expect(screen.getByTestId("relay-password-input")).toBeEnabled();
  });

  it("disables the form and counts down while the daemon rate-limits this device", async () => {
    vi.useFakeTimers();
    render(<RelayConnectionStatus status={{ state: "passwordLocked", retryAfterSecs: 8 }} />);
    const line = screen.getByTestId("connection-relay-status");
    expect(line).toHaveAttribute("data-state", "passwordLocked");
    expect(line).toHaveTextContent("Try again in 8 s.");
    expect(screen.getByTestId("relay-password-input")).toBeDisabled();
    expect(screen.getByTestId("relay-password-submit")).toBeDisabled();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(3000);
    });
    expect(line).toHaveTextContent("Try again in 5 s.");
  });

  it("shows Rust's error and keeps the field empty when handing over fails", async () => {
    vi.spyOn(desktop, "relaySetPassword").mockRejectedValue(new Error("no relay connection is selected"));
    render(<RelayConnectionStatus status={{ state: "passwordRequired" }} connectionId="relay:h1@r:7400" />);
    fireEvent.change(screen.getByTestId("relay-password-input"), { target: { value: "another long password" } });
    fireEvent.click(screen.getByTestId("relay-password-submit"));
    expect(await screen.findByTestId("relay-password-error")).toHaveTextContent("no relay connection is selected");
    expect((screen.getByTestId("relay-password-input") as HTMLInputElement).value).toBe("");
  });

  it("refuses an over-long password without sending it", async () => {
    const set = vi.spyOn(desktop, "relaySetPassword").mockResolvedValue(undefined);
    render(<RelayConnectionStatus status={{ state: "passwordRequired" }} connectionId="relay:h1@r:7400" />);
    fireEvent.change(screen.getByTestId("relay-password-input"), { target: { value: "x".repeat(200) } });
    fireEvent.click(screen.getByTestId("relay-password-submit"));
    expect(set).not.toHaveBeenCalled();
    expect(await screen.findByTestId("relay-password-error")).toHaveTextContent("too long");
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
