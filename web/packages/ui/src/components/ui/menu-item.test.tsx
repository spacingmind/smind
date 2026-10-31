import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";

function renderOpenMenu(item: React.ReactElement) {
  return render(
    <DropdownMenu open>
      <DropdownMenuTrigger />
      <DropdownMenuContent>{item}</DropdownMenuContent>
    </DropdownMenu>,
  );
}

describe("MenuItemBody via DropdownMenuItem", () => {
  it("renders icon, title, description and shortcut", () => {
    renderOpenMenu(
      <DropdownMenuItem icon={<svg data-testid="i" />} description="Desc" shortcut="⌘N">
        Title
      </DropdownMenuItem>,
    );
    expect(screen.getByText("Title")).toBeInTheDocument();
    expect(screen.getByText("Desc")).toBeInTheDocument();
    expect(screen.getByText("⌘N")).toBeInTheDocument();
    expect(screen.getByTestId("i")).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: /^Title/ })).toBeInTheDocument();
  });

  it("plain children render unchanged", () => {
    renderOpenMenu(<DropdownMenuItem>Plain</DropdownMenuItem>);
    const item = screen.getByRole("menuitem");
    expect(item.textContent).toBe("Plain");
    expect(item.querySelector('[data-slot="menu-item-shortcut"]')).toBeNull();
  });
});
