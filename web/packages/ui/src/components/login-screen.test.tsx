import { act } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { LoginScreen } from "@/components/login-screen";
import { LoginError, type Session } from "@/lib/auth";

const SESSION: Session = { token: "t", expiresAt: "2999-01-01T00:00:00Z" };

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

function field(): HTMLInputElement {
  return screen.getByLabelText("Password") as HTMLInputElement;
}

function submitWith(password: string): void {
  fireEvent.change(field(), { target: { value: password } });
  fireEvent.submit(field().form!);
}

describe("LoginScreen", () => {
  it("renders an accessible password field and a submit button", () => {
    render(<LoginScreen onLoggedIn={() => {}} />);
    const input = field();
    expect(input).toHaveAttribute("type", "password");
    expect(input).toHaveAttribute("autocomplete", "current-password");
    expect(screen.getByRole("button", { name: "Sign in" })).toBeEnabled();
  });

  it("signs in on submit and reports success", async () => {
    const login = vi.fn().mockResolvedValue(SESSION);
    const onLoggedIn = vi.fn();
    render(<LoginScreen onLoggedIn={onLoggedIn} login={login} />);

    submitWith("correct horse");
    await flush();

    expect(login).toHaveBeenCalledExactlyOnceWith("correct horse");
    expect(onLoggedIn).toHaveBeenCalledTimes(1);
    expect(field().value).toBe("");
  });

  it("disables the button and shows a spinner while the request is pending", async () => {
    let resolve!: (s: Session) => void;
    const login = vi.fn(() => new Promise<Session>((r) => (resolve = r)));
    render(<LoginScreen onLoggedIn={() => {}} login={login} />);

    submitWith("pw");
    await flush();

    const button = screen.getByTestId("login-submit");
    expect(button).toBeDisabled();
    expect(button).toHaveTextContent("Signing in");
    // A second submit while pending is ignored.
    fireEvent.submit(field().form!);
    expect(login).toHaveBeenCalledTimes(1);

    await act(async () => resolve(SESSION));
  });

  it("shows 'Wrong password' for a 401 and clears the field", async () => {
    const login = vi.fn().mockRejectedValue(new LoginError({ kind: "wrong-password" }));
    render(<LoginScreen onLoggedIn={() => {}} login={login} />);

    submitWith("nope");
    await flush();

    expect(screen.getByRole("alert")).toHaveTextContent("Wrong password");
    expect(field().value).toBe("");
    expect(field()).toHaveFocus();
    expect(screen.getByTestId("login-submit")).toBeEnabled();
  });

  it("counts down after a 429 and re-enables the button when it elapses", async () => {
    const login = vi.fn().mockRejectedValue(new LoginError({ kind: "rate-limited", retryAfterSeconds: 3 }));
    render(<LoginScreen onLoggedIn={() => {}} login={login} />);

    submitWith("pw");
    await flush();
    expect(screen.getByTestId("login-error")).toHaveTextContent("Try again in 3s");
    expect(screen.getByTestId("login-submit")).toBeDisabled();

    // Submitting while limited is a no-op.
    submitWith("pw2");
    expect(login).toHaveBeenCalledTimes(1);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1000);
    });
    expect(screen.getByTestId("login-error")).toHaveTextContent("Try again in 2s");

    // One tick per act: each tick's re-render schedules the next one.
    for (let i = 0; i < 2; i++) {
      await act(async () => {
        await vi.advanceTimersByTimeAsync(1000);
      });
    }
    expect(screen.getByTestId("login-error")).toBeEmptyDOMElement();
    expect(screen.getByTestId("login-submit")).toBeEnabled();
  });

  it("explains 403 (password login disabled) and network errors", async () => {
    const login = vi
      .fn()
      .mockRejectedValueOnce(new LoginError({ kind: "disabled" }))
      .mockRejectedValueOnce(new LoginError({ kind: "network" }));
    render(<LoginScreen onLoggedIn={() => {}} login={login} />);

    submitWith("pw");
    await flush();
    expect(screen.getByRole("alert")).toHaveTextContent(/isn't enabled/i);

    submitWith("pw");
    await flush();
    expect(screen.getByRole("alert")).toHaveTextContent(/can't reach the daemon/i);
  });

  it("says why the user is back here after an expired or rejected session", () => {
    const { rerender } = render(<LoginScreen onLoggedIn={() => {}} reason="expired" />);
    expect(screen.getByText(/session expired/i)).toBeInTheDocument();
    rerender(<LoginScreen onLoggedIn={() => {}} reason="rejected" />);
    expect(screen.getByText(/ended your session/i)).toBeInTheDocument();
  });

  it("never stores the password: not in either web storage, not logged, not left in the DOM", async () => {
    const log = vi.spyOn(console, "log");
    const error = vi.spyOn(console, "error");
    const login = vi.fn().mockRejectedValue(new LoginError({ kind: "wrong-password" }));
    const { container } = render(<LoginScreen onLoggedIn={() => {}} login={login} />);

    submitWith("super-secret-pw");
    await flush();

    expect(JSON.stringify({ ...window.localStorage })).not.toContain("super-secret-pw");
    expect(JSON.stringify({ ...window.sessionStorage })).not.toContain("super-secret-pw");
    expect(container.innerHTML).not.toContain("super-secret-pw");
    expect(field().value).toBe("");
    expect(log).not.toHaveBeenCalled();
    expect(error).not.toHaveBeenCalled();
  });
});
