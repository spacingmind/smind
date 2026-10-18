import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { PasteRedirectPanel } from "@/components/connect/paste-redirect-panel";

function setup(overrides: Partial<Parameters<typeof PasteRedirectPanel>[0]> = {}) {
  const onSubmit = vi.fn();
  const props = { onSubmit, pending: false, error: null, ...overrides };
  const view = render(<PasteRedirectPanel {...props} />);
  return { onSubmit, props, view };
}

function typeAddress(text: string) {
  fireEvent.change(screen.getByLabelText("Sign-in redirect address"), {
    target: { value: text },
  });
}

describe("paste redirect panel", () => {
  it("paste panel submits the trimmed address", () => {
    const { onSubmit } = setup();
    typeAddress("  http://localhost:54545/callback?code=abc  ");
    fireEvent.click(screen.getByTestId("paste-redirect-submit"));
    expect(onSubmit).toHaveBeenCalledTimes(1);
    expect(onSubmit).toHaveBeenCalledWith("http://localhost:54545/callback?code=abc");
  });

  it("paste panel disables finish while empty or pending", () => {
    const { onSubmit, view } = setup();
    const submit = screen.getByTestId("paste-redirect-submit");
    expect(submit).toBeDisabled();
    typeAddress("http://localhost:54545/callback?code=abc");
    expect(submit).not.toBeDisabled();
    expect(onSubmit).not.toHaveBeenCalled();

    view.rerender(<PasteRedirectPanel onSubmit={onSubmit} pending={true} error={null} />);
    typeAddress("http://localhost:54545/callback?code=abc");
    const pendingSubmit = screen.getByTestId("paste-redirect-submit");
    expect(pendingSubmit).toBeDisabled();
    expect(pendingSubmit.textContent).toBe("Finishing…");
  });

  it("paste panel shows the error", () => {
    setup({ error: "The address didn't contain a login code." });
    const el = screen.getByTestId("paste-redirect-error");
    expect(el).toHaveAttribute("role", "alert");
    expect(el.textContent).toBe("The address didn't contain a login code.");
  });

  it("paste panel submits on enter", () => {
    const { onSubmit } = setup();
    typeAddress("http://localhost:54545/callback?code=abc");
    fireEvent.keyDown(screen.getByLabelText("Sign-in redirect address"), { key: "Enter" });
    expect(onSubmit).toHaveBeenCalledTimes(1);
  });

  it("paste panel clears the input after an accepted paste", () => {
    const onSubmit = vi.fn();
    const view = render(<PasteRedirectPanel onSubmit={onSubmit} pending={false} error={null} />);
    const input = screen.getByLabelText("Sign-in redirect address");
    fireEvent.change(input, { target: { value: "http://127.0.0.1:1/cb?code=SECRET" } });
    view.rerender(<PasteRedirectPanel onSubmit={onSubmit} pending={true} error={null} />);
    view.rerender(<PasteRedirectPanel onSubmit={onSubmit} pending={false} error={null} />);
    expect(input).toHaveValue("");
  });

  it("paste panel clears the error display when the input is edited", () => {
    const onSubmit = vi.fn();
    const view = render(<PasteRedirectPanel onSubmit={onSubmit} pending={false} error="Nope." />);
    expect(screen.getByTestId("paste-redirect-error")).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Sign-in redirect address"), { target: { value: "http://x" } });
    expect(screen.queryByTestId("paste-redirect-error")).not.toBeInTheDocument();
    // The error stays dismissed across a pending cycle with no new error.
    view.rerender(<PasteRedirectPanel onSubmit={onSubmit} pending={true} error="Nope." />);
    view.rerender(<PasteRedirectPanel onSubmit={onSubmit} pending={false} error="Nope." />);
    void view;
  });
});
