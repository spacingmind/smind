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

  it("an unconnected proxy-consumer provider says No proxy account, not Not connected", () => {
    const proxy: ProviderNavItem = { id: "claude-native", label: "Claude Code", kind: "oauth", accountProvider: "anthropic", accountIds: [], tone: "neutral", connected: false };
    const kimi: ProviderNavItem = { id: "kimi", label: "Kimi", kind: "api-key", accountProvider: "kimi", accountIds: [], tone: "neutral", connected: false };
    render(<ProviderNav items={[proxy, kimi]} selectedId={null} onSelect={() => {}} mode="full" />);
    expect(screen.getByRole("button", { name: "Claude Code, No proxy account" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Kimi, Not connected" })).toBeInTheDocument();
  });

  it("shows recipe and unverified badges only for recipe instances", () => {
    const base = { kind: "cli", accountIds: [], tone: "neutral", connected: false } as const;
    const list: ProviderNavItem[] = [
      { ...base, id: "plain", label: "Plain" },
      { ...base, id: "verified", label: "Verified", recipe: true },
      { ...base, id: "odd", label: "Odd", recipe: true, unverified: true },
    ];
    render(<ProviderNav items={list} selectedId={null} onSelect={() => {}} mode="full" />);
    expect(screen.queryByTestId("provider-nav-recipe-plain")).toBeNull();
    expect(screen.queryByTestId("provider-nav-unverified-plain")).toBeNull();
    expect(screen.getByTestId("provider-nav-recipe-verified")).toHaveTextContent("recipe");
    expect(screen.queryByTestId("provider-nav-unverified-verified")).toBeNull();
    expect(screen.getByTestId("provider-nav-recipe-odd")).toBeInTheDocument();
    expect(screen.getByTestId("provider-nav-unverified-odd")).toHaveTextContent("unverified");
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
    expect(button).toHaveAttribute("aria-label", "Claude, 2 accounts, All accounts healthy");
    expect(screen.queryByText("Claude")).not.toBeInTheDocument();
  });

  it("nav buttons expose the tone in their accessible name", () => {
    for (const mode of ["full", "rail"] as const) {
      const view = render(<ProviderNav items={items} selectedId={null} onSelect={() => {}} mode={mode} />);
      expect(screen.getByRole("button", { name: "Claude, 2 accounts, All accounts healthy" })).toBeInTheDocument();
      expect(screen.getByRole("button", { name: "Codex, Not connected" })).toBeInTheDocument();
      expect(screen.getByRole("button", { name: "Other accounts, 1 accounts, Needs attention" })).toBeInTheDocument();
      view.unmount();
    }
  });
});
