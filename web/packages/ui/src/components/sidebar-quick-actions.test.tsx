import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { SidebarQuickActions } from "@/components/sidebar-quick-actions";

function renderQuickActions(
  props: Partial<{ onNewTask: () => void; onSearch: () => void; onAutomations: () => void; searchShortcut?: string | null }> = {},
) {
  return render(
    <SidebarQuickActions onNewTask={() => {}} onSearch={() => {}} onAutomations={() => {}} {...props} />,
  );
}

describe("SidebarQuickActions", () => {
  it("renders New task, Search and Automations", () => {
    renderQuickActions();
    expect(screen.getByRole("button", { name: "New task" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Search" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Automations" })).toBeInTheDocument();
  });

  it("Search shows the palette shortcut", () => {
    renderQuickActions({ searchShortcut: "⌘K" });
    expect(screen.getByText("⌘K")).toBeInTheDocument();

    const { container } = renderQuickActions();
    expect(container.querySelector("kbd")).toBeNull();
  });

  it("rows call their handlers", () => {
    const onNewTask = vi.fn();
    const onSearch = vi.fn();
    const onAutomations = vi.fn();
    renderQuickActions({ onNewTask, onSearch, onAutomations });
    fireEvent.click(screen.getByRole("button", { name: "New task" }));
    fireEvent.click(screen.getByRole("button", { name: "Search" }));
    fireEvent.click(screen.getByRole("button", { name: "Automations" }));
    expect(onNewTask).toHaveBeenCalledTimes(1);
    expect(onSearch).toHaveBeenCalledTimes(1);
    expect(onAutomations).toHaveBeenCalledTimes(1);
  });
});
