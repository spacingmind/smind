import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { ProviderRail, type ProviderRailHandle } from "@/components/pickers/provider-rail";

const PROVIDERS = [
  { id: "claude-native", label: "Claude" },
  { id: "codex-native", label: "Codex" },
  { id: "glm-native", label: "GLM", unavailableReason: "No account" },
];

describe("ProviderRail", () => {
  it("picker-lists-providers-in-a-rail", () => {
    render(<ProviderRail providers={PROVIDERS} selectedId="claude-native" onSelect={() => {}} />);

    expect(screen.getByTestId("model-provider-rail")).toHaveAttribute("role", "tablist");
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
});
