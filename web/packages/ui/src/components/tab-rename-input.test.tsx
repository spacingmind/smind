import { act } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { TabRenameInput } from "@/components/tab-rename-input";

/** Waits for the input's own requestAnimationFrame-deferred focus to run. */
async function flushRaf(): Promise<void> {
  await act(async () => {
    await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()));
  });
}

describe("TabRenameInput", () => {
  it("starts with the tab's current title, selected", () => {
    render(<TabRenameInput title="Terminal" onCommit={() => {}} onCancel={() => {}} />);
    expect(screen.getByTestId("workspace-tab-rename-input")).toHaveValue("Terminal");
  });

  it("focuses itself once mounted, even though something else grabs focus synchronously first", async () => {
    // Regression test for the real bug this component was extracted to
    // fix: App.tsx's ContextMenu (Radix) runs its own dismiss-time focus
    // restoration in the same commit this component mounts in, moving
    // focus to whatever it considers "the trigger" right after Rename is
    // clicked -- an `autoFocus` input reliably lost that race in a real
    // browser (verified live), landing focus on a sibling control
    // instead and, via this input's own onBlur, silently committing an
    // unedited rename before the user could type anything.
    //
    // jsdom can't reproduce Radix's real portal/dismiss timing, so this
    // simulates the race directly: a sibling button steals focus
    // synchronously, in the same tick this component mounts (exactly
    // what a menu's own onCloseAutoFocus would do if not prevented) --
    // the input must still end up focused once its own deferred effect
    // runs.
    function Harness() {
      return (
        <div>
          <button data-testid="menu-trigger-stand-in">trigger</button>
          <TabRenameInput title="Terminal" onCommit={() => {}} onCancel={() => {}} />
        </div>
      );
    }
    render(<Harness />);

    // The competing focus grab -- synchronous, same tick as mount.
    screen.getByTestId("menu-trigger-stand-in").focus();
    expect(document.activeElement).toHaveAttribute("data-testid", "menu-trigger-stand-in");

    await flushRaf();

    expect(document.activeElement).toBe(screen.getByTestId("workspace-tab-rename-input"));
  });

  it("selects the current text once focused, so typing replaces rather than appends", async () => {
    render(<TabRenameInput title="Terminal" onCommit={() => {}} onCancel={() => {}} />);
    await flushRaf();

    const input = screen.getByTestId("workspace-tab-rename-input") as HTMLInputElement;
    expect(input.selectionStart).toBe(0);
    expect(input.selectionEnd).toBe("Terminal".length);
  });

  it("commits the new value on blur", async () => {
    const commits: string[] = [];
    render(<TabRenameInput title="Terminal" onCommit={(v) => commits.push(v)} onCancel={() => {}} />);
    await flushRaf();

    const input = screen.getByTestId("workspace-tab-rename-input");
    fireEvent.change(input, { target: { value: "build" } });
    fireEvent.blur(input);

    expect(commits).toEqual(["build"]);
  });

  it("commits on Enter (which blurs the input)", async () => {
    const commits: string[] = [];
    render(<TabRenameInput title="Terminal" onCommit={(v) => commits.push(v)} onCancel={() => {}} />);
    await flushRaf();

    const input = screen.getByTestId("workspace-tab-rename-input");
    fireEvent.change(input, { target: { value: "build" } });
    fireEvent.keyDown(input, { key: "Enter" });

    expect(commits).toEqual(["build"]);
  });

  it("cancels on Escape without committing", async () => {
    const commits: string[] = [];
    const cancels: number[] = [];
    render(<TabRenameInput title="Terminal" onCommit={(v) => commits.push(v)} onCancel={() => cancels.push(1)} />);
    await flushRaf();

    const input = screen.getByTestId("workspace-tab-rename-input");
    fireEvent.change(input, { target: { value: "should not stick" } });
    fireEvent.keyDown(input, { key: "Escape" });
    fireEvent.blur(input);

    expect(cancels).toEqual([1]);
    expect(commits).toEqual([]);
  });
});
