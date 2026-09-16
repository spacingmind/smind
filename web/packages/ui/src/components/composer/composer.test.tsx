import { act } from "react";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { Composer } from "@/components/composer/composer";
import { autoGrow, MAX_COMPOSER_HEIGHT } from "@/components/composer/prompt-textarea";
import { draftStorageKey } from "@/components/composer/use-composer-draft";
import { FakeWsClient } from "@/test/fake-ws-client";
import type { ApprovalPolicy, Provider } from "@/lib/types";

interface Submission {
  provider: Provider;
  prompt: string;
  approvalPolicy: ApprovalPolicy;
}

/** Flushes pending microtasks inside `act` so React commits before assertions. */
async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

function renderComposer(
  overrides: Partial<React.ComponentProps<typeof Composer>> = {},
): {
  submissions: Submission[];
  stops: string[];
  rerender: (props: Partial<React.ComponentProps<typeof Composer>>) => void;
  unmount: () => void;
} {
  const submissions: Submission[] = [];
  const stops: string[] = [];
  const props: React.ComponentProps<typeof Composer> = {
    client: new FakeWsClient(),
    taskId: 1,
    connected: true,
    runningRunId: null,
    onSubmit: async (provider, prompt, approvalPolicy) => {
      submissions.push({ provider, prompt, approvalPolicy });
    },
    onStop: async (runId) => {
      stops.push(runId);
    },
    ...overrides,
  };
  const view = render(<Composer {...props} />);
  return {
    submissions,
    stops,
    rerender: (next) => view.rerender(<Composer {...props} {...next} />),
    unmount: view.unmount,
  };
}

function textarea(): HTMLTextAreaElement {
  return screen.getByLabelText("Prompt") as HTMLTextAreaElement;
}

describe("Composer", () => {
  beforeEach(() => {
    window.localStorage.clear();
  });

  it("submits on Enter and inserts a newline on Shift+Enter", async () => {
    const { submissions } = renderComposer();

    fireEvent.change(textarea(), { target: { value: "line one" } });
    fireEvent.keyDown(textarea(), { key: "Enter", shiftKey: true });
    await flush();
    // Shift+Enter is the browser's own default newline insertion -- the
    // composer's job is only to *not* submit.
    expect(submissions).toEqual([]);

    fireEvent.keyDown(textarea(), { key: "Enter" });
    await flush();
    expect(submissions).toEqual([{ provider: "claude-native", prompt: "line one", approvalPolicy: "manual" }]);
  });

  it("does not submit on the Enter that commits an IME composition", async () => {
    const { submissions } = renderComposer();

    fireEvent.change(textarea(), { target: { value: "にほんご" } });

    // The modern signal…
    fireEvent.keyDown(textarea(), { key: "Enter", isComposing: true });
    // …and the legacy one Safari/some Android IMEs send instead.
    fireEvent.keyDown(textarea(), { key: "Enter", keyCode: 229 });
    await flush();
    expect(submissions).toEqual([]);

    fireEvent.keyDown(textarea(), { key: "Enter" });
    await flush();
    expect(submissions).toHaveLength(1);
  });

  it("clears the draft on submit", async () => {
    renderComposer();

    fireEvent.change(textarea(), { target: { value: "do the thing" } });
    expect(window.localStorage.getItem(draftStorageKey(1))).toBe("do the thing");

    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    await flush();

    expect(textarea()).toHaveValue("");
    expect(window.localStorage.getItem(draftStorageKey(1))).toBeNull();
  });

  it("grows the textarea to its cap and then scrolls", () => {
    renderComposer();
    const el = textarea();
    expect(el.style.maxHeight).toBe(`${MAX_COMPOSER_HEIGHT}px`);

    // jsdom has no layout, so drive the sizing rule directly with a
    // stubbed scrollHeight -- see autoGrow's doc comment.
    Object.defineProperty(el, "scrollHeight", { value: 60, configurable: true });
    autoGrow(el);
    expect(el.style.height).toBe("60px");
    expect(el.style.overflowY).toBe("hidden");

    Object.defineProperty(el, "scrollHeight", { value: MAX_COMPOSER_HEIGHT + 400, configurable: true });
    autoGrow(el);
    expect(el.style.height).toBe(`${MAX_COMPOSER_HEIGHT}px`);
    expect(el.style.overflowY).toBe("auto");
  });

  it("keeps a per-task draft across a task switch and a remount", () => {
    const { rerender, unmount } = renderComposer({ taskId: 1 });

    fireEvent.change(textarea(), { target: { value: "task one draft" } });

    rerender({ taskId: 2 });
    expect(textarea()).toHaveValue("");
    fireEvent.change(textarea(), { target: { value: "task two draft" } });

    rerender({ taskId: 1 });
    expect(textarea()).toHaveValue("task one draft");

    // A remount (what App.tsx really does -- the tab strip is keyed by
    // task id) reads the same storage back.
    unmount();
    renderComposer({ taskId: 2 });
    expect(textarea()).toHaveValue("task two draft");
  });

  it("states why it is blocked in the placeholder", () => {
    const { rerender } = renderComposer({ connected: false });
    expect(textarea()).toHaveAttribute("placeholder", expect.stringContaining("Not connected"));
    expect(textarea()).toBeDisabled();

    rerender({ connected: true, taskId: null });
    expect(textarea()).toHaveAttribute("placeholder", "Select a task to send a prompt");
    expect(textarea()).toBeDisabled();

    rerender({ connected: true, taskId: 1, runningRunId: "run-1" });
    // A live run is not a block: it queues, and the placeholder says so.
    expect(textarea()).toHaveAttribute("placeholder", expect.stringContaining("Queue a follow-up"));
    expect(textarea()).not.toBeDisabled();

    rerender({ connected: true, taskId: 1, runningRunId: null });
    expect(textarea()).toHaveAttribute("placeholder", "Send a prompt…");
  });

  it("queues a prompt typed while a run is live and sends it when the run ends", async () => {
    const { submissions, rerender } = renderComposer({ runningRunId: "run-1" });

    fireEvent.change(textarea(), { target: { value: "then do this" } });
    fireEvent.click(screen.getByRole("button", { name: "Queue" }));
    await flush();

    expect(submissions).toEqual([]);
    expect(within(screen.getByTestId("composer-queue")).getByText("then do this")).toBeInTheDocument();
    expect(textarea()).toHaveValue("");

    rerender({ runningRunId: null });
    await flush();

    expect(submissions).toEqual([{ provider: "claude-native", prompt: "then do this", approvalPolicy: "manual" }]);
    expect(screen.queryByTestId("composer-queue")).not.toBeInTheDocument();
  });

  it("drops the queue when the task changes rather than firing it at the next task", async () => {
    const { submissions, rerender } = renderComposer({ taskId: 1, runningRunId: "run-1" });

    fireEvent.change(textarea(), { target: { value: "for task one" } });
    fireEvent.click(screen.getByRole("button", { name: "Queue" }));
    await flush();
    expect(screen.getByTestId("composer-queue")).toBeInTheDocument();

    rerender({ taskId: 2, runningRunId: null });
    await flush();

    expect(submissions).toEqual([]);
    expect(screen.queryByTestId("composer-queue")).not.toBeInTheDocument();
  });

  it("stops the live run from the composer, on click and on Escape", async () => {
    const { stops } = renderComposer({ runningRunId: "run-7" });

    fireEvent.click(screen.getByTestId("chat-stop-button"));
    await flush();
    expect(stops).toEqual(["run-7"]);

    fireEvent.keyDown(textarea(), { key: "Escape" });
    await flush();
    expect(stops).toEqual(["run-7", "run-7"]);
  });

  it("does not lose the text when the submit fails", async () => {
    const onSubmit = vi.fn().mockRejectedValue(new Error("daemon said no"));
    renderComposer({ onSubmit });

    fireEvent.change(textarea(), { target: { value: "do the thing" } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    await flush();

    expect(screen.getByText("daemon said no")).toBeInTheDocument();
    expect(textarea()).toHaveValue("do the thing");
  });

  it("renders the provider dropdown from provider.list behind a visible label", async () => {
    const client = new FakeWsClient();
    renderComposer({ client });

    client.nth("provider.list", 0).resolve({
      providers: [
        { id: "claude-native", label: "Claude Code" },
        { id: "glm", label: "GLM" },
      ],
    });
    await flush();

    // The dropdown is the shadcn/Radix Select, not a native <select>: the
    // trigger is a combobox button and the options live in a portal that
    // only exists while it is open, so they are read after opening it
    // rather than out of a closed element's subtree.
    const trigger = screen.getByLabelText("Provider");
    expect(trigger).toHaveAttribute("role", "combobox");
    expect(trigger.tagName).toBe("BUTTON");

    fireEvent.click(trigger);
    const options = await screen.findAllByRole("option");
    expect(options.map((o) => o.textContent)).toEqual(["Claude Code", "GLM"]);
    // The trigger shows the current selection, so the closed state still
    // says which provider a prompt would go to.
    expect(trigger).toHaveTextContent("Claude Code");

    // "not unlabelled native selects" (Item 10): the label is real markup,
    // not an aria-label, so it is on screen as well as in the a11y tree --
    // and `htmlFor`/`id` still associates it with the Radix trigger, which
    // is what getByLabelText above resolved through.
    expect(screen.getByText("Provider").tagName).toBe("LABEL");
    expect(screen.getByText("Approval policy").tagName).toBe("LABEL");
  });

  it("selecting a provider from the open dropdown is what the next prompt is sent with", async () => {
    const client = new FakeWsClient();
    const { submissions } = renderComposer({ client });

    client.nth("provider.list", 0).resolve({
      providers: [
        { id: "claude-native", label: "Claude Code" },
        { id: "glm", label: "GLM" },
      ],
    });
    await flush();

    fireEvent.click(screen.getByLabelText("Provider"));
    fireEvent.click(await screen.findByRole("option", { name: "GLM" }));
    await flush();

    fireEvent.change(textarea(), { target: { value: "do the thing" } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    await flush();

    expect(submissions).toEqual([{ provider: "glm", prompt: "do the thing", approvalPolicy: "manual" }]);
  });

  it("keeps the approval-policy help tooltip and disables both selects while the composer is inactive", () => {
    const { rerender } = renderComposer();

    const policy = screen.getByLabelText("Approval policy");
    expect(policy).toHaveAttribute("title", expect.stringContaining("Auto-safe"));
    expect(policy).not.toBeDisabled();
    expect(screen.getByLabelText("Provider")).not.toBeDisabled();

    rerender({ connected: false });
    expect(screen.getByLabelText("Provider")).toBeDisabled();
    expect(screen.getByLabelText("Approval policy")).toBeDisabled();
  });

  // Item 21: touch targets need a stated minimum below the compact
  // breakpoint -- jsdom has no layout, so this asserts the class list
  // carries both the 44px compact rule and the `md:`-scoped revert to the
  // original dense size, rather than a computed pixel height.
  it("Send and the provider/policy selects carry the compact 44px touch-target class, reverting to the dense size at md:", () => {
    renderComposer();

    const send = screen.getByTestId("chat-send-button");
    expect(send.className).toContain("h-11");
    expect(send.className).toContain("md:h-7");

    // The trigger element is what the finger lands on, so the rule has to
    // survive on it and not be merged away by SelectTrigger's own h-8.
    for (const label of ["Provider", "Approval policy"]) {
      const trigger = screen.getByLabelText(label);
      expect(trigger.className).toContain("h-11");
      expect(trigger.className).toContain("md:h-7");
      expect(trigger.className).not.toContain("h-8");
    }
  });
});
