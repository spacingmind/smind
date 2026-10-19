import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { ConfirmDialog } from "@/components/ui/confirm-dialog";

function renderDialog(props: Partial<React.ComponentProps<typeof ConfirmDialog>> = {}) {
  const onConfirm = vi.fn();
  const onOpenChange = vi.fn();
  render(
    <ConfirmDialog
      open
      onOpenChange={onOpenChange}
      title="Delete thing?"
      description="It goes away."
      confirmLabel="Delete"
      pending={false}
      onConfirm={onConfirm}
      {...props}
    />,
  );
  return { onConfirm, onOpenChange };
}

describe("ConfirmDialog", () => {
  it("renders title and description; confirm calls onConfirm, Cancel closes", () => {
    const { onConfirm, onOpenChange } = renderDialog();
    expect(screen.getByText("Delete thing?")).toBeInTheDocument();
    expect(screen.getByText("It goes away.")).toBeInTheDocument();
    fireEvent.click(screen.getByTestId("confirm-dialog-confirm"));
    expect(onConfirm).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByTestId("confirm-dialog-cancel"));
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it("derives its test ids from testId, and lets a surface keep its own", () => {
    renderDialog({ testId: "x", confirmTestId: "keep-confirm", errorTestId: "keep-error", error: "nope" });
    expect(screen.getByTestId("x")).toBeInTheDocument();
    expect(screen.getByTestId("keep-confirm")).toBeInTheDocument();
    expect(screen.getByTestId("x-cancel")).toBeInTheDocument();
    expect(screen.getByTestId("keep-error")).toHaveTextContent("nope");
    expect(screen.getByTestId("keep-error")).toHaveAttribute("role", "alert");
  });

  it("pending disables both buttons and shows the pending label", () => {
    renderDialog({ pending: true, pendingLabel: "Deleting…" });
    expect(screen.getByTestId("confirm-dialog-confirm")).toBeDisabled();
    expect(screen.getByTestId("confirm-dialog-confirm")).toHaveTextContent("Deleting…");
    expect(screen.getByTestId("confirm-dialog-cancel")).toBeDisabled();
  });

  it("requireText blocks the confirm until the exact text is typed", () => {
    renderDialog({ requireText: "my-workspace" });
    const confirm = screen.getByTestId("confirm-dialog-confirm");
    expect(confirm).toBeDisabled();
    fireEvent.change(screen.getByTestId("confirm-dialog-input"), { target: { value: "my-workspac" } });
    expect(confirm).toBeDisabled();
    fireEvent.change(screen.getByTestId("confirm-dialog-input"), { target: { value: "my-workspace" } });
    expect(confirm).toBeEnabled();
  });

  it("destructive=false gives the confirm the primary look instead of the destructive one", () => {
    const { unmount } = render(
      <ConfirmDialog open onOpenChange={() => {}} title="t" description="d" confirmLabel="Go" pending={false} onConfirm={() => {}} />,
    );
    const destructiveClass = screen.getByTestId("confirm-dialog-confirm").className;
    unmount();
    renderDialog({ destructive: false });
    expect(screen.getByTestId("confirm-dialog-confirm").className).not.toBe(destructiveClass);
  });
});
