import { act } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { App } from "@/App";
import { LoginRequiredError, clearSession, type Session } from "@/lib/auth";
import { WsClient } from "@/lib/ws-client";
import { FakeSocket } from "@/test/fake-socket";

const SESSION: Session = { token: "t", expiresAt: "2999-01-01T00:00:00Z" };

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
  window.location.hash = "";
  clearSession();
});

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

describe("App password gate", () => {
  it("shows the login form when the daemon requires a password", async () => {
    const connect = vi.fn().mockRejectedValue(new LoginRequiredError("required"));
    render(<App connect={connect} />);
    await flush();

    expect(screen.getByTestId("login-screen")).toBeInTheDocument();
    expect(screen.getByLabelText("Password")).toBeInTheDocument();
    // Not the "can't reach daemon" state.
    expect(screen.queryByText(/disconnected/i)).not.toBeInTheDocument();
  });

  it("does not show it on loopback", async () => {
    const connect = vi.fn().mockResolvedValue(new WsClient(new FakeSocket()));
    render(<App connect={connect} />);
    await flush();

    expect(screen.queryByTestId("login-screen")).not.toBeInTheDocument();
    expect(screen.getByText("Connected to daemon")).toBeInTheDocument();
  });

  it("connects again after a successful sign-in and leaves the login form", async () => {
    const connect = vi
      .fn()
      .mockRejectedValueOnce(new LoginRequiredError("required"))
      .mockResolvedValueOnce(new WsClient(new FakeSocket()));
    render(<App connect={connect} />);
    await flush();

    // Sign in through the real form, with its login call stubbed at fetch.
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(new Response(JSON.stringify(SESSION), { status: 200 })),
    );
    fireEvent.change(screen.getByLabelText("Password"), { target: { value: "pw" } });
    fireEvent.submit(screen.getByLabelText("Password").closest("form")!);
    await flush();
    await flush();
    vi.unstubAllGlobals();

    expect(connect).toHaveBeenCalledTimes(2);
    expect(screen.queryByTestId("login-screen")).not.toBeInTheDocument();
    expect(screen.getByText("Connected to daemon")).toBeInTheDocument();
  });

  it("an expired session on reconnect goes back to the login form", async () => {
    const socket = new FakeSocket();
    const connect = vi
      .fn()
      .mockResolvedValueOnce(new WsClient(socket))
      .mockRejectedValue(new LoginRequiredError("expired"));
    render(<App connect={connect} />);
    await flush();
    expect(screen.getByText("Connected to daemon")).toBeInTheDocument();

    socket.emitClose();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(10_000);
    });

    expect(screen.getByTestId("login-screen")).toBeInTheDocument();
    expect(screen.getByText(/session expired/i)).toBeInTheDocument();
    expect(connect).toHaveBeenCalledTimes(2);
  });

  it("a handshake rejection on reconnect goes back to the login form without looping", async () => {
    const socket = new FakeSocket();
    const connect = vi
      .fn()
      .mockResolvedValueOnce(new WsClient(socket))
      .mockRejectedValue(new LoginRequiredError("rejected"));
    render(<App connect={connect} />);
    await flush();

    socket.emitClose();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(10_000);
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(30_000);
    });

    expect(screen.getByTestId("login-screen")).toBeInTheDocument();
    expect(connect).toHaveBeenCalledTimes(2);
  });

  it("a wrong password keeps the form up and does not reconnect", async () => {
    const connect = vi.fn().mockRejectedValue(new LoginRequiredError("required"));
    render(<App connect={connect} />);
    await flush();

    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response("{}", { status: 401 })));
    fireEvent.change(screen.getByLabelText("Password"), { target: { value: "bad" } });
    fireEvent.submit(screen.getByLabelText("Password").closest("form")!);
    await flush();
    await flush();
    vi.unstubAllGlobals();

    expect(screen.getByRole("alert")).toHaveTextContent("Wrong password");
    expect(connect).toHaveBeenCalledTimes(1);
  });
});
