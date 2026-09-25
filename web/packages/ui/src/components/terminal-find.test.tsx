import { act } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { FakeWsClient } from "@/test/fake-ws-client";
import { TerminalPane, type TerminalFindResult, type TerminalHandle } from "@/components/terminal-pane";
import { KeyboardProvider } from "@/keyboard/keyboard-provider";
import { resetTerminalSessions } from "@/lib/terminal-sessions";
import type { Task } from "@/lib/types";

/**
 * End-to-end coverage for terminal Find (AC3): `Mod+F` claimed only while
 * the terminal pane has focus, and the shared bar driving
 * `@xterm/addon-search` through `TerminalHandle.find` -- mocked here (per
 * the plan's own "mock the addon" test-scenario note) rather than a real
 * `SearchAddon`, which needs xterm's own canvas renderer jsdom doesn't
 * implement (see terminal-pane.test.tsx's `FakeTerminalHandle` for the
 * same reasoning applied to the rest of the terminal wiring).
 */

const TASK: Task = {
  ID: 1,
  WorkspaceID: 1,
  SpaceID: null,
  Title: "Task A",
  Status: "active",
  WorktreePath: "/tmp/a",
  Branch: "task-a",
  CreatedAt: "2024-01-01T00:00:00Z",
  UpdatedAt: "2024-01-01T00:00:00Z",
  ArchivedAt: null,
};

/** A TerminalHandle whose `find` capability is a full mock, so tests can assert exactly how TerminalPane drives the search addon. */
class FindableTerminalHandle implements TerminalHandle {
  opened: HTMLElement | null = null;
  searchCalls: Array<{ term: string; direction?: "next" | "previous" }> = [];
  clearCalls = 0;
  private resultsCallback: ((result: TerminalFindResult) => void) | null = null;

  open(container: HTMLElement): void {
    this.opened = container;
    // A real xterm.js mounts its own helper <textarea> into the container;
    // closeFind() looks for one to return focus to it.
    const textarea = document.createElement("textarea");
    container.appendChild(textarea);
  }
  onData(): { dispose(): void } {
    return { dispose: () => {} };
  }
  onResize(): { dispose(): void } {
    return { dispose: () => {} };
  }
  write(): void {}
  fit(): void {}
  dispose(): void {}

  find = {
    search: (term: string, direction?: "next" | "previous") => {
      this.searchCalls.push({ term, direction });
    },
    clear: () => {
      this.clearCalls++;
    },
    onDidChangeResults: (callback: (result: TerminalFindResult) => void) => {
      this.resultsCallback = callback;
      return { dispose: () => (this.resultsCallback = null) };
    },
  };

  /** Test-only: simulates the addon reporting a new result set. */
  emitResults(result: TerminalFindResult): void {
    this.resultsCallback?.(result);
  }
}

function pressModF(): void {
  fireEvent.keyDown(document, { key: "f", code: "KeyF", ctrlKey: true });
}

function focusTerminalPane(): void {
  fireEvent.focus(screen.getByTestId("terminal-container"));
}

function renderTerminal(fake: TerminalHandle) {
  const client = new FakeWsClient();
  render(
    <KeyboardProvider>
      <TerminalPane client={client} task={TASK} createTerminal={() => fake} />
    </KeyboardProvider>,
  );
  return client;
}

// The tab<->session binding in lib/terminal-sessions.ts lives outside
// React and otherwise leaks between this file's tests (same reasoning as
// terminal-pane.test.tsx's own afterEach).
afterEach(() => {
  resetTerminalSessions();
});

describe("terminal Find", () => {
  it("Mod+F opens the bar only once the terminal pane is focused", () => {
    const fake = new FindableTerminalHandle();
    renderTerminal(fake);

    pressModF();
    expect(screen.queryByTestId("find-bar")).not.toBeInTheDocument();

    focusTerminalPane();
    pressModF();
    expect(screen.getByTestId("find-bar")).toBeInTheDocument();
  });

  it("never shows Find for a handle with no search capability", () => {
    const fake: TerminalHandle = {
      open: () => {},
      onData: () => ({ dispose: () => {} }),
      onResize: () => ({ dispose: () => {} }),
      write: () => {},
      fit: () => {},
      dispose: () => {},
    };
    renderTerminal(fake);

    focusTerminalPane();
    pressModF();
    expect(screen.queryByTestId("find-bar")).not.toBeInTheDocument();
  });

  it("typing calls the search addon, and prev/next pass a direction", () => {
    const fake = new FindableTerminalHandle();
    renderTerminal(fake);
    focusTerminalPane();
    pressModF();

    fireEvent.change(screen.getByTestId("find-input"), { target: { value: "error" } });
    expect(fake.searchCalls.at(-1)).toEqual({ term: "error", direction: undefined });
    // The addon reports a result count asynchronously (from TerminalPane's
    // perspective); prev/next are disabled until there's something to
    // navigate to, mirroring the shared FindBar's `canNavigate` contract.
    act(() => fake.emitResults({ resultIndex: 0, resultCount: 3 }));

    fireEvent.click(screen.getByRole("button", { name: "Next match" }));
    expect(fake.searchCalls.at(-1)).toEqual({ term: "error", direction: "next" });

    fireEvent.click(screen.getByRole("button", { name: "Previous match" }));
    expect(fake.searchCalls.at(-1)).toEqual({ term: "error", direction: "previous" });
  });

  it("reflects the addon's own reported result index/count", () => {
    const fake = new FindableTerminalHandle();
    renderTerminal(fake);
    focusTerminalPane();
    pressModF();

    fireEvent.change(screen.getByTestId("find-input"), { target: { value: "error" } });
    act(() => fake.emitResults({ resultIndex: 2, resultCount: 5 }));

    expect(screen.getByTestId("find-status")).toHaveTextContent("3/5");
  });

  it("Escape (or Close) clears the addon's decorations", () => {
    const fake = new FindableTerminalHandle();
    renderTerminal(fake);
    focusTerminalPane();
    pressModF();
    fireEvent.change(screen.getByTestId("find-input"), { target: { value: "error" } });

    fireEvent.keyDown(screen.getByTestId("find-input"), { key: "Escape" });

    expect(fake.clearCalls).toBe(1);
    expect(screen.queryByTestId("find-bar")).not.toBeInTheDocument();
  });
});
