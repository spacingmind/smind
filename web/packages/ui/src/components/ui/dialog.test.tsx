import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";

describe("dialog anatomy", () => {
  it("content uses the shared anatomy classes", () => {
    render(
      <Dialog open>
        <DialogContent>
          <DialogTitle>T</DialogTitle>
          <DialogDescription>D</DialogDescription>
        </DialogContent>
      </Dialog>,
    );
    const content = document.querySelector('[data-slot="dialog-content"]');
    expect(content).not.toBeNull();
    expect(content).toHaveClass("rounded-xl");
    expect(content).not.toHaveClass("rounded-2xl");
    expect(screen.getByRole("button", { name: "Close" })).toHaveClass("size-7");
  });

  it("footer and body slots render", () => {
    render(
      <Dialog open>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>T</DialogTitle>
          </DialogHeader>
          <DialogBody>Body copy</DialogBody>
          <DialogFooter>Footer copy</DialogFooter>
        </DialogContent>
      </Dialog>,
    );
    expect(screen.getByText("Body copy").closest('[data-slot="dialog-body"]')).not.toBeNull();
    expect(screen.getByText("Footer copy").closest('[data-slot="dialog-footer"]')).not.toBeNull();
  });
});
