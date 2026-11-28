import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { Button, buttonVariants } from "@/components/ui/button";

// visual-identity-console Item 4: execute/approval/quiet are additive --
// every existing variant must still resolve to a class string (no rename
// broke a call site), and each new one renders with its own distinct,
// semantic class output rather than falling back to `default`.
describe("buttonVariants", () => {
  it("keeps every pre-existing variant resolving to a class string", () => {
    for (const variant of ["default", "secondary", "destructive", "outline", "ghost", "link"] as const) {
      expect(buttonVariants({ variant })).toEqual(expect.any(String));
    }
  });

  it("execute uses the warning color family", () => {
    const classes = buttonVariants({ variant: "execute" });
    expect(classes).toContain("bg-warning/10");
    expect(classes).toContain("text-warning-text");
  });

  it("approval uses the success color family", () => {
    const classes = buttonVariants({ variant: "approval" });
    expect(classes).toContain("bg-success/10");
    expect(classes).toContain("text-success");
  });

  it("quiet has no fill, only muted text -- lower emphasis than ghost", () => {
    const classes = buttonVariants({ variant: "quiet" });
    expect(classes).toContain("text-foreground-muted");
    // Unlike every fill-bearing variant (including `ghost`'s hover fill),
    // `quiet` never introduces a background color -- it stays flat.
    expect(classes).not.toContain("hover:bg-");
    for (const bg of ["bg-status", "bg-primary", "bg-destructive", "bg-muted", "bg-secondary"]) {
      expect(classes).not.toContain(bg);
    }
  });

  it("renders the requested variant onto the data-variant attribute", () => {
    render(<Button variant="execute">Run</Button>);
    expect(screen.getByRole("button", { name: "Run" })).toHaveAttribute("data-variant", "execute");
  });
});

const VARIANTS = ["default", "outline", "secondary", "ghost", "destructive", "link", "execute", "approval", "quiet"] as const;
const SIZES = ["default", "xs", "sm", "lg", "icon", "icon-xs", "icon-sm", "icon-lg"] as const;

describe("Button on Astryx", () => {
  it("button_keeps_variants_sizes_and_data_attributes", () => {
    for (const variant of VARIANTS) {
      for (const size of SIZES) {
        const { unmount } = render(
          <Button variant={variant} size={size} className="my-extra" aria-label="X">
            {size.startsWith("icon") ? <svg /> : "X"}
          </Button>,
        );
        const el = screen.getByRole("button", { name: "X" });
        expect(el).toHaveAttribute("data-slot", "button");
        expect(el).toHaveAttribute("data-variant", variant);
        expect(el).toHaveAttribute("data-size", size);
        expect(el).toHaveClass("my-extra");
        unmount();
      }
    }
  });

  it("button_as_child_renders_the_child_element", () => {
    render(
      <Button asChild variant="outline" size="sm">
        <a href="/x">Go</a>
      </Button>,
    );
    const el = screen.getByRole("link", { name: "Go" });
    expect(el.tagName).toBe("A");
    expect(el).toHaveAttribute("data-slot", "button");
    expect(el).toHaveAttribute("data-variant", "outline");
  });
});
