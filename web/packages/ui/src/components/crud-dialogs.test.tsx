import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { CreateWorkspaceDialog } from "@/components/crud-dialogs";

// visual-identity-console Item 4: a dialog's Cancel button is the "quiet"
// call site -- non-committal next to the primary Create action, via the
// shared `FormActions` row every CRUD dialog renders through.
describe("crud dialog Cancel button", () => {
  it("renders with the quiet variant", () => {
    render(
      <CreateWorkspaceDialog client={null} open={true} onOpenChange={vi.fn()} onCreated={vi.fn()} />,
    );

    expect(screen.getByTestId("dialog-new-workspace-cancel")).toHaveAttribute("data-variant", "quiet");
  });
});
