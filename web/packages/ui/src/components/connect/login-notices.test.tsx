import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { LoginDoneNotice, LoginErrorNotice } from "@/components/connect/login-notices";

describe("login notices", () => {
  it("done notice words created, updated and adopted differently", () => {
    const { unmount: u1 } = render(
      <LoginDoneNotice result={{ label: "work", created: true }} onDismiss={() => {}} />,
    );
    expect(screen.getByTestId("login-done-notice").textContent).toContain(
      "Connected as 'work'.",
    );
    u1();

    const { unmount: u2 } = render(
      <LoginDoneNotice result={{ label: "main", created: false }} onDismiss={() => {}} />,
    );
    expect(screen.getByTestId("login-done-notice").textContent).toContain(
      "Updated 'main' — already connected.",
    );
    u2();

    render(
      <LoginDoneNotice
        result={{ label: "old", created: false, adopted: true }}
        onDismiss={() => {}}
      />,
    );
    expect(screen.getByTestId("login-done-notice").textContent).toContain(
      "Updated 'old' — now linked to the account you signed in with.",
    );
  });

  it("error notice distinguishes soft from explanatory errors", () => {
    const { unmount } = render(
      <LoginErrorNotice message="Login cancelled." soft onDismiss={() => {}} />,
    );
    const soft = screen.getByTestId("login-error-notice");
    expect(soft).toHaveAttribute("role", "alert");
    expect(soft.textContent).toContain("Login cancelled.");
    expect(soft.className).toContain("text-muted-foreground");
    unmount();

    render(
      <LoginErrorNotice message="Token exchange failed." soft={false} onDismiss={() => {}} />,
    );
    const hard = screen.getByTestId("login-error-notice");
    expect(hard.textContent).toContain("Token exchange failed.");
    expect(hard.className).not.toContain("text-muted-foreground");
    expect(hard.querySelector('[data-slot="alert"]') ?? hard).toBe(hard);
  });

  it("notices call onDismiss", () => {
    const onDismissDone = vi.fn();
    const onDismissError = vi.fn();
    const { unmount } = render(
      <LoginDoneNotice result={{ label: "work", created: true }} onDismiss={onDismissDone} />,
    );
    fireEvent.click(screen.getByTestId("login-notice-dismiss"));
    expect(onDismissDone).toHaveBeenCalledTimes(1);
    unmount();

    render(
      <LoginErrorNotice message="Login cancelled." soft onDismiss={onDismissError} />,
    );
    fireEvent.click(screen.getByTestId("login-notice-dismiss"));
    expect(onDismissError).toHaveBeenCalledTimes(1);
  });
});
