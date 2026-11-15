import { fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { FirstRunCards } from "@/components/home/first-run-cards";
import type { ProviderInfo } from "@/lib/types";

const PROVIDERS: ProviderInfo[] = [
  { id: "claude-native", label: "Claude Code", models: [] },
  { id: "glm", label: "GLM", models: [] },
];

function renderCards(over: Partial<React.ComponentProps<typeof FirstRunCards>> = {}) {
  const ensure = vi.fn();
  const onAddProject = vi.fn();
  const utils = render(
    <FirstRunCards providers={PROVIDERS} health={{}} rpcFailed={new Set()} ensure={ensure} onAddProject={onAddProject} {...over} />,
  );
  return { ...utils, ensure, onAddProject };
}

afterEach(() => vi.restoreAllMocks());

describe("FirstRunCards", () => {
  it("add-a-project-card-opens-the-add-project-flow", () => {
    const { onAddProject } = renderCards();
    fireEvent.click(within(screen.getByTestId("first-run-card-project")).getByRole("button", { name: "Add a project" }));
    expect(onAddProject).toHaveBeenCalledTimes(1);
  });

  it("set-up-providers-card-opens-providers-settings", () => {
    const seen: unknown[] = [];
    const listener = (e: Event) => seen.push((e as CustomEvent).detail);
    window.addEventListener("smind:open-settings", listener);
    try {
      renderCards();
      fireEvent.click(within(screen.getByTestId("first-run-card-providers")).getByRole("button", { name: "Open Providers" }));
      expect(seen).toEqual([{ sectionId: "providers" }]);
    } finally {
      window.removeEventListener("smind:open-settings", listener);
    }
  });

  it("cards-are-buttons-with-text-labels: real buttons, named by their text, in the tab order", () => {
    renderCards();
    const buttons = screen.getAllByRole("button");
    expect(buttons.map((b) => b.textContent)).toEqual(["Add a project", "Open Providers"]);
    for (const b of buttons) {
      expect(b.tagName).toBe("BUTTON");
      expect(b.getAttribute("tabindex")).not.toBe("-1");
      expect(b).toBeEnabled();
    }
  });

  it("no-import-card-yet: exactly two cards, no import card and no empty cell", () => {
    renderCards();
    const section = screen.getByTestId("first-run-cards");
    expect(within(section).getAllByRole("heading").map((h) => h.textContent)).toEqual(["Add a project", "Set up providers"]);
    expect(section.children).toHaveLength(2);
    expect(section).not.toHaveTextContent(/import/i);
  });

  it("the providers card shows the same readiness sentence as the Get started card, and Done wording once an agent is signed in", () => {
    const { unmount } = renderCards({ health: { "claude-native": { ok: false, detail: "not found on PATH" }, glm: { ok: false, detail: "no account" } } });
    expect(screen.getByTestId("first-run-providers-status").textContent).toMatch(/not installed|signed out|No agent/i);
    unmount();
    renderCards({ health: { "claude-native": { ok: true, detail: "ok" } } });
    expect(screen.getByTestId("first-run-providers-status")).toHaveTextContent("An agent is signed in");
  });

  it("checks the providers once on mount (ensure), never on a plain re-render", () => {
    const { ensure, rerender } = renderCards();
    expect(ensure).toHaveBeenCalledTimes(1);
    rerender(<FirstRunCards providers={PROVIDERS} health={{}} rpcFailed={new Set()} ensure={ensure} />);
    expect(ensure).toHaveBeenCalledTimes(1);
  });

  it("without an onAddProject handler the project card shows no dead button", () => {
    renderCards({ onAddProject: undefined });
    expect(screen.queryByTestId("first-run-add-project")).not.toBeInTheDocument();
  });
});
