import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { ProviderNav } from "@/components/providers/provider-nav";
import type { ProviderNavItem } from "@/lib/providers-view";

const items: ProviderNavItem[] = [
  { id: "anthropic", label: "Claude", kind: "oauth", accountIds: [1, 2], tone: "ok", connected: true },
  { id: "openai", label: "Codex", kind: "oauth", accountIds: [], tone: "neutral", connected: false },
  { id: "other", label: "Other accounts", kind: "other", accountIds: [3], tone: "warning", connected: true },
];

describe("ProviderNav", () => {
  it("nav renders one button per provider with tone dots", () => {
    render(<ProviderNav items={items} selectedId={null} onSelect={() => {}} mode="full" />);
    const nav = screen.getByTestId("provider-nav");
    expect(nav).toHaveAttribute("data-mode", "full");
    for (const item of items) {
      expect(screen.getByTestId(`provider-nav-item-${item.id}`)).toBeInTheDocument();
    }
    expect(screen.getByTitle("All accounts healthy")).toBeInTheDocument();
    expect(screen.getByTitle("Not connected")).toBeInTheDocument();
    expect(screen.getByTitle("Needs attention")).toBeInTheDocument();
    expect(screen.getByText("2")).toBeInTheDocument();
  });

  it("nav marks the selected provider", () => {
    render(<ProviderNav items={items} selectedId="anthropic" onSelect={() => {}} mode="full" />);
    expect(screen.getByTestId("provider-nav-item-anthropic")).toHaveAttribute("aria-current", "true");
    expect(screen.getByTestId("provider-nav-item-openai")).not.toHaveAttribute("aria-current");
  });

  it("nav calls onSelect with the provider id", async () => {
    const onSelect = vi.fn();
    render(<ProviderNav items={items} selectedId={null} onSelect={onSelect} mode="full" />);
    fireEvent.click(screen.getByTestId("provider-nav-item-openai"));
    expect(onSelect).toHaveBeenCalledWith("openai");
  });

  it("rail mode shows icons only with titles", () => {
    render(<ProviderNav items={items} selectedId={null} onSelect={() => {}} mode="rail" />);
    expect(screen.getByTestId("provider-nav")).toHaveAttribute("data-mode", "rail");
    const button = screen.getByTestId("provider-nav-item-anthropic");
    expect(button).toHaveAttribute("title", "Claude");
    expect(button).toHaveAttribute("aria-label", "Claude");
    expect(screen.queryByText("Claude")).not.toBeInTheDocument();
  });
});
