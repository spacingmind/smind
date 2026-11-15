import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { ProviderRail, providerInitial, type ProviderRailHandle } from "@/components/pickers/provider-rail";

const PROVIDERS = [
  { id: "claude-native", label: "Claude" },
  { id: "codex-native", label: "Codex" },
  { id: "glm-native", label: "GLM", unavailableReason: "No account" },
];

describe("ProviderRail", () => {
  it("picker-lists-providers-in-a-rail", () => {
    render(<ProviderRail providers={PROVIDERS} selectedId="claude-native" onSelect={() => {}} />);

    expect(screen.getByTestId("model-picker-rail")).toHaveAttribute("role", "tablist");
    expect(screen.getAllByRole("tab")).toHaveLength(3);
    expect(screen.getByTestId("model-provider-claude-native")).toHaveAttribute("aria-selected", "true");
    expect(screen.getByTestId("model-provider-codex-native")).toHaveAttribute("aria-selected", "false");
    expect(screen.getByTestId("model-provider-glm-native")).toHaveAttribute("aria-selected", "false");
  });

  it("picker-rail-is-keyboard-operable", () => {
    const onSelect = vi.fn();
    const handleRef: { current: ProviderRailHandle | null } = { current: null };
    const { rerender } = render(
      <ProviderRail ref={handleRef} providers={PROVIDERS} selectedId="claude-native" onSelect={onSelect} />,
    );

    const claude = screen.getByTestId("model-provider-claude-native");
    const codex = screen.getByTestId("model-provider-codex-native");
    const glm = screen.getByTestId("model-provider-glm-native");
    claude.focus();
    expect(claude).toHaveFocus();

    fireEvent.keyDown(claude, { key: "ArrowDown" });
    expect(onSelect).toHaveBeenCalledWith("codex-native");
    expect(codex).toHaveFocus();
    rerender(<ProviderRail ref={handleRef} providers={PROVIDERS} selectedId="codex-native" onSelect={onSelect} />);

    fireEvent.keyDown(codex, { key: "End" });
    expect(onSelect).toHaveBeenCalledWith("glm-native");
    expect(glm).toHaveFocus();
    rerender(<ProviderRail ref={handleRef} providers={PROVIDERS} selectedId="glm-native" onSelect={onSelect} />);

    fireEvent.keyDown(glm, { key: "Home" });
    expect(onSelect).toHaveBeenCalledWith("claude-native");
    expect(claude).toHaveFocus();
    rerender(<ProviderRail ref={handleRef} providers={PROVIDERS} selectedId="claude-native" onSelect={onSelect} />);

    fireEvent.keyDown(claude, { key: "ArrowUp" });
    expect(onSelect).toHaveBeenCalledWith("glm-native");
    expect(glm).toHaveFocus();
    rerender(<ProviderRail ref={handleRef} providers={PROVIDERS} selectedId="glm-native" onSelect={onSelect} />);

    // Roving tabindex: only the selected tab is in the tab order.
    expect(claude).toHaveAttribute("tabIndex", "-1");
    expect(codex).toHaveAttribute("tabIndex", "-1");
    expect(glm).toHaveAttribute("tabIndex", "0");

    // The imperative handle focuses the selected tab.
    handleRef.current?.focusSelected();
    expect(glm).toHaveFocus();
  });

  it("picker-provider-without-account-says-why", () => {
    const onSelect = vi.fn();
    render(<ProviderRail providers={PROVIDERS} selectedId="claude-native" onSelect={onSelect} />);

    const glm = screen.getByTestId("model-provider-glm-native");
    expect(glm).toHaveAttribute("data-unavailable", "true");
    expect(glm).toHaveAttribute("aria-label", "GLM, unavailable: No account");
    expect(glm).toHaveAttribute("title", "GLM: No account");
    expect(screen.getByTestId("model-provider-warning-glm-native")).toBeInTheDocument();
    expect(screen.queryByTestId("model-provider-warning-claude-native")).not.toBeInTheDocument();

    // Still selectable: its view explains why there is no account.
    fireEvent.click(glm);
    expect(onSelect).toHaveBeenCalledWith("glm-native");
  });

  it("rail-provider-without-a-brand-mark-gets-its-initial", () => {
    render(
      <ProviderRail
        providers={[
          { id: "claude-native", label: "Claude Code" },
          { id: "glm", label: "GLM" },
          { id: "kimi", label: "Kimi" },
          { id: "my-acp", label: "my acp agent" },
        ]}
        selectedId="glm"
        onSelect={() => {}}
      />,
    );
    // GLM, Kimi and an unknown ACP agent share the generic glyph: each shows its own initial instead
    expect(screen.getByTestId("model-rail-initial-glm")).toHaveTextContent("G");
    expect(screen.getByTestId("model-rail-initial-kimi")).toHaveTextContent("K");
    expect(screen.getByTestId("model-rail-initial-my-acp")).toHaveTextContent("M");
    // the initial is decoration: the tab keeps its accessible name
    expect(screen.getByTestId("model-provider-glm")).toHaveAttribute("aria-label", "GLM");
    expect(screen.getByTestId("model-rail-initial-glm")).toHaveAttribute("aria-hidden", "true");
  });

  it("rail-branded-provider-keeps-its-mark", () => {
    render(<ProviderRail providers={[{ id: "claude-native", label: "Claude Code" }, { id: "codex-native", label: "Codex" }]} selectedId="claude-native" onSelect={() => {}} />);
    expect(screen.queryByTestId("model-rail-initial-claude-native")).not.toBeInTheDocument();
    expect(screen.queryByTestId("model-rail-initial-codex-native")).not.toBeInTheDocument();
    expect(screen.getByTestId("model-provider-claude-native").querySelector("svg")).not.toBeNull();
  });

  it("providerInitial takes the first letter or digit, upper-cased, and is empty for a label without one", () => {
    expect(providerInitial("glm")).toBe("G");
    expect(providerInitial("  my-acp")).toBe("M");
    expect(providerInitial("(work) claude")).toBe("W");
    expect(providerInitial("7zip agent")).toBe("7");
    expect(providerInitial("Émile")).toBe("É");
    expect(providerInitial("---")).toBe("");
    expect(providerInitial("")).toBe("");
  });

  it("a label without a letter falls back to the generic glyph, not an empty tile", () => {
    render(<ProviderRail providers={[{ id: "weird", label: "---" }, { id: "glm", label: "GLM" }]} selectedId="glm" onSelect={() => {}} />);
    expect(screen.queryByTestId("model-rail-initial-weird")).not.toBeInTheDocument();
    expect(screen.getByTestId("model-provider-weird").querySelector("svg")).not.toBeNull();
  });
});
