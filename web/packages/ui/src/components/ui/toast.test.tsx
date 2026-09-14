import { act, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { clearToasts, dismissToast, toast, Toaster } from "@/components/ui/toast";

afterEach(() => {
  clearToasts();
  vi.useRealTimers();
});

describe("Toaster", () => {
  it("renders nothing with no toasts queued", () => {
    render(<Toaster />);
    expect(screen.queryByTestId("toast-host")).not.toBeInTheDocument();
  });

  it("renders a toast queued before mount, and one queued after", () => {
    const id = toast({ title: "Before mount" });
    render(<Toaster />);
    expect(screen.getByText("Before mount")).toBeInTheDocument();

    act(() => {
      toast({ title: "After mount", description: "with a description" });
    });
    expect(screen.getByText("After mount")).toBeInTheDocument();
    expect(screen.getByText("with a description")).toBeInTheDocument();

    dismissToast(id);
  });

  it("dismissToast removes just that toast", () => {
    render(<Toaster />);
    let idA = "";
    act(() => {
      idA = toast({ title: "A", durationMs: 0 });
      toast({ title: "B", durationMs: 0 });
    });
    expect(screen.getByText("A")).toBeInTheDocument();
    expect(screen.getByText("B")).toBeInTheDocument();

    act(() => {
      dismissToast(idA);
    });
    expect(screen.queryByText("A")).not.toBeInTheDocument();
    expect(screen.getByText("B")).toBeInTheDocument();
  });

  it("auto-dismisses after durationMs", () => {
    vi.useFakeTimers();
    render(<Toaster />);
    act(() => {
      toast({ title: "Transient", durationMs: 1000 });
    });
    expect(screen.getByText("Transient")).toBeInTheDocument();

    act(() => {
      vi.advanceTimersByTime(1000);
    });
    expect(screen.queryByText("Transient")).not.toBeInTheDocument();
  });

  it("error variant gets role=alert; every other variant gets role=status", () => {
    render(<Toaster />);
    act(() => {
      toast({ title: "Failed", variant: "error", durationMs: 0 });
      toast({ title: "Done", variant: "success", durationMs: 0 });
    });
    expect(screen.getByText("Failed").closest('[data-testid="toast"]')).toHaveAttribute("role", "alert");
    expect(screen.getByText("Done").closest('[data-testid="toast"]')).toHaveAttribute("role", "status");
  });
});
