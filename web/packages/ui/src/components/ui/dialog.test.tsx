import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

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
    expect(content).toHaveClass("rounded-2xl");
    expect(content).not.toHaveClass("rounded-xl");
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

import { fireEvent } from "@testing-library/react";
import { useState } from "react";
import { DialogTrigger } from "@/components/ui/dialog";

function Harness({ onOpenChange }: { onOpenChange?: (o: boolean) => void }) {
  return (
    <Dialog onOpenChange={onOpenChange}>
      <DialogTrigger>Open it</DialogTrigger>
      <DialogContent>
        <DialogTitle>Title</DialogTitle>
        <DialogDescription>Desc</DialogDescription>
        <button>First</button>
      </DialogContent>
    </Dialog>
  );
}

describe("dialog on Astryx", () => {
  it("dialog_pointer_open_shows_no_focus_ring", () => {
    // jsdom has no :focus-visible heuristics for pointer opens, so this asserts
    // the observable equivalents: after a pointer click opens the dialog,
    // nothing inside matches :focus-visible and the content carries no ring
    // classes; after a keyboard-initiated open the focused first button is the
    // active element (the browser then applies :focus-visible to it).
    render(<Harness />);
    const trigger = screen.getByRole("button", { name: "Open it" });
    fireEvent.pointerDown(trigger, { pointerType: "mouse" });
    fireEvent.mouseDown(trigger);
    fireEvent.click(trigger);
    const content = document.querySelector('[data-slot="dialog-content"]') as HTMLElement;
    expect(content).not.toBeNull();
    for (const el of content.querySelectorAll("*")) {
      let matches = false;
      try { matches = el.matches(":focus-visible"); } catch { matches = false; }
      expect(matches).toBe(false);
    }
    expect(content.className).not.toMatch(/ring|outline/);
    expect(content.style.outline).toBe("");
    fireEvent.click(screen.getAllByRole("button", { name: "Close" })[0]);

    // Keyboard open.
    fireEvent.keyDown(trigger, { key: "Enter" });
    fireEvent.click(trigger);
    const first = screen.getByRole("button", { name: "First" });
    first.focus();
    expect(document.activeElement).toBe(first);
  });

  it("dialog_escape_and_overlay_close_it", () => {
    const onOpenChange = vi.fn();
    function Controlled() {
      const [open, setOpen] = useState(true);
      return (
        <Dialog open={open} onOpenChange={(o) => { onOpenChange(o); setOpen(o); }}>
          <DialogContent><DialogTitle>T</DialogTitle></DialogContent>
        </Dialog>
      );
    }
    const { unmount } = render(<Controlled />);
    const dlg = document.querySelector('[data-slot="dialog-content"]') as HTMLElement;
    fireEvent.keyDown(dlg, { key: "Escape" });
    fireEvent.keyDown(document, { key: "Escape" });
    expect(onOpenChange).toHaveBeenCalledWith(false);
    unmount();

    onOpenChange.mockClear();
    render(<Controlled />);
    const dlg2 = document.querySelector('[data-slot="dialog-content"]') as HTMLElement;
    // A press on the ::backdrop targets the <dialog> element itself.
    fireEvent.pointerDown(dlg2);
    fireEvent.mouseDown(dlg2);
    fireEvent.pointerUp(dlg2);
    fireEvent.mouseUp(dlg2);
    fireEvent.click(dlg2);
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it("dialog_stays_clickable_when_a_radix_layer_disables_body_pointer_events", () => {
    // Radix sets `body { pointer-events: none }` while one of its modal layers is open (the 390px sidebar sheet).
    // A native <dialog> inherits it, so the content must set `pointer-events: auto` itself, as Radix's content did.
    document.body.style.pointerEvents = "none";
    try {
      render(
        <Dialog open onOpenChange={() => {}}>
          <DialogContent><DialogTitle>T</DialogTitle></DialogContent>
        </Dialog>,
      );
      const dlg = document.querySelector('[data-slot="dialog-content"]') as HTMLElement;
      expect(dlg.className).toContain("pointer-events-auto");
    } finally {
      document.body.style.pointerEvents = "";
    }
  });
});
