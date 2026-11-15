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

  describe("the providers step per state", () => {
    const READY = { ok: true, detail: "ok" };
    const SIGNED_OUT = { ok: false, detail: "no account" };
    const MISSING = { ok: false, detail: '"x" not found on PATH' };
    const THREE: ProviderInfo[] = [...PROVIDERS, { id: "codex-native", label: "Codex", models: [] }];

    it("providers-card-when-ready-shows-done-with-the-agent-name-and-a-quiet-manage-link", () => {
      renderCards({ health: { "claude-native": READY, glm: SIGNED_OUT } });
      const card = screen.getByTestId("first-run-card-providers");
      expect(card).toHaveAttribute("data-state", "done");
      expect(screen.getByTestId("first-run-providers-done")).toHaveTextContent("Agents ready · Claude Code");
      expect(card).not.toHaveTextContent(/Set up providers/);
      expect(card).not.toHaveTextContent(/Open Providers/);
      expect(card.querySelector("svg")?.getAttribute("class")).toContain("lucide-check"); // the check icon, as in the Get started block
      const manage = within(card).getByRole("button", { name: "Manage" });
      expect(manage.className).toContain("underline-offset-4"); // the quiet link variant
      expect(manage.className).not.toContain("bg-primary");
      expect(card).toHaveTextContent("(done)"); // for assistive tech, like the Get started step
    });

    it("several ready agents read as a count", () => {
      renderCards({ providers: THREE, health: { "claude-native": READY, glm: READY, "codex-native": SIGNED_OUT } });
      expect(screen.getByTestId("first-run-providers-done")).toHaveTextContent("2 agents ready");
    });

    it("Manage runs the same Settings -> Providers navigation the to-do card's button runs", () => {
      const seen: unknown[] = [];
      const listener = (e: Event) => seen.push((e as CustomEvent).detail);
      window.addEventListener("smind:open-settings", listener);
      try {
        renderCards({ health: { "claude-native": READY } });
        fireEvent.click(screen.getByRole("button", { name: "Manage" }));
        expect(seen).toEqual([{ sectionId: "providers" }]);
      } finally {
        window.removeEventListener("smind:open-settings", listener);
      }
    });

    it("the signed-out line is a short summary, never one name per provider", () => {
      renderCards({ health: { "claude-native": SIGNED_OUT, glm: SIGNED_OUT } });
      const text = screen.getByTestId("first-run-providers-status").textContent ?? "";
      expect(text).not.toContain("Claude Code");
      expect(text).not.toContain("GLM");
      expect(text.length).toBeLessThan(60);
    });

    it("done: no stretching spacer, Manage sits in the same row as the name, the grid does not stretch the card", () => {
      renderCards({ health: { "claude-native": READY } });
      const card = screen.getByTestId("first-run-card-providers");
      expect(card.innerHTML).not.toContain("flex-1");
      expect(screen.getByTestId("first-run-providers-done").parentElement).toContainElement(screen.getByTestId("first-run-open-providers"));
      expect(screen.getByTestId("first-run-cards").className).toContain("items-start");
    });

    it("done leaves Add a project as the one primary action", () => {
      renderCards({ health: { "claude-native": READY } });
      expect(screen.getByTestId("first-run-add-project").className).toContain("bg-primary");
    });

    it.each([
      ["signed out", { "claude-native": SIGNED_OUT, glm: SIGNED_OUT }, "No agent is signed in · 2 need sign-in"],
      ["not installed", { "claude-native": MISSING, glm: MISSING }, "No agent is signed in · 2 not installed"],
      ["mixed", { "claude-native": SIGNED_OUT, glm: MISSING }, "No agent is signed in · 1 needs sign-in · 1 not installed"],
    ])("%s keeps the Set up providers card with its sentence and its outline button, and no primary Add a project", (_name, health, sentence) => {
      renderCards({ health });
      const card = screen.getByTestId("first-run-card-providers");
      expect(card).toHaveAttribute("data-state", "todo");
      expect(card).toHaveTextContent("Set up providers");
      expect(screen.getByTestId("first-run-providers-status").textContent).toBe(sentence);
      const open = within(card).getByRole("button", { name: "Open Providers" });
      expect(open.className).not.toContain("bg-primary");
      expect(screen.queryByTestId("first-run-providers-done")).not.toBeInTheDocument();
      expect(screen.getByTestId("first-run-add-project").className).not.toContain("bg-primary");
    });

    it("while the agents are still being checked the card stays to-do (never done early)", () => {
      renderCards({ health: {} });
      expect(screen.getByTestId("first-run-card-providers")).toHaveAttribute("data-state", "todo");
      expect(screen.getByTestId("first-run-providers-status")).toHaveTextContent(/Checking|Checking/);
    });
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
