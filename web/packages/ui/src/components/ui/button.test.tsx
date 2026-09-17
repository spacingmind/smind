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

  it("execute uses the status-running color family", () => {
    const classes = buttonVariants({ variant: "execute" });
    expect(classes).toContain("bg-status-running/10");
    expect(classes).toContain("text-status-running");
  });

  it("approval uses the status-success color family", () => {
    const classes = buttonVariants({ variant: "approval" });
    expect(classes).toContain("bg-status-success/10");
    expect(classes).toContain("text-status-success");
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
