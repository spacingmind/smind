import { act } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { Pencil } from "lucide-react";

import { CopyAction, MessageAction, MessageActions } from "@/components/timeline/message-actions";

afterEach(() => {
  vi.useRealTimers();
  Reflect.deleteProperty(navigator, "clipboard");
});

describe("MessageActions", () => {
  it("the bar is hidden until hover or focus-within and always visible on touch", () => {
    render(<MessageActions group="prompt" data-testid="bar" />);
    const cls = screen.getByTestId("bar").className.split(/\s+/);
    expect(cls).toContain("opacity-0");
    expect(cls).toContain("group-hover/prompt:opacity-100");
    expect(cls).toContain("focus-within:opacity-100");
    expect(cls).toContain("[@media(hover:none)]:opacity-100");
  });

  it("Copy writes the text, shows the check, and stays silent when the clipboard is unavailable", async () => {
    vi.useFakeTimers();
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
    render(
      <MessageActions group="run">
        <CopyAction getText={() => "hello"} data-testid="copy" />
      </MessageActions>,
    );

    await act(async () => {
      fireEvent.click(screen.getByTestId("copy"));
    });
    expect(writeText).toHaveBeenCalledWith("hello");
    expect(screen.getByTestId("copy")).toHaveAccessibleName("Copied");
    act(() => {
      vi.advanceTimersByTime(1600);
    });
    expect(screen.getByTestId("copy")).toHaveAccessibleName("Copy");

    // Rejected and absent clipboards: no throw, no check.
    writeText.mockRejectedValue(new Error("denied"));
    await act(async () => {
      fireEvent.click(screen.getByTestId("copy"));
    });
    expect(screen.getByTestId("copy")).toHaveAccessibleName("Copy");
    Reflect.deleteProperty(navigator, "clipboard");
    await act(async () => {
      fireEvent.click(screen.getByTestId("copy"));
    });
    expect(screen.getByTestId("copy")).toHaveAccessibleName("Copied");
  });

  it("actions are focusable buttons with accessible names", () => {
    const onClick = vi.fn();
    render(
      <MessageActions group="run">
        <CopyAction getText={() => "x"} />
        <MessageAction label="Edit & resend" icon={<Pencil aria-hidden />} onClick={onClick} />
      </MessageActions>,
    );
    const buttons = screen.getAllByRole("button");
    expect(buttons.map((b) => b.getAttribute("aria-label"))).toEqual(["Copy", "Edit & resend"]);
    for (const b of buttons) {
      expect(b).not.toHaveAttribute("tabindex", "-1");
      b.focus();
      expect(b).toHaveFocus();
    }
    fireEvent.click(screen.getByRole("button", { name: "Edit & resend" }));
    expect(onClick).toHaveBeenCalledOnce();
  });
});
