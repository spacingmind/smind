import { act } from "react";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { Composer } from "@/components/composer/composer";
import { TaskDetailPane } from "@/components/task-detail";
import type { DaemonEvents } from "@/hooks/use-daemon-events";
import type { ChatQueueItem, RunSummary, Task } from "@/lib/types";
import { autoGrow, isSubmitKey, MAX_COMPOSER_HEIGHT } from "@/components/composer/prompt-textarea";
import { draftStorageKey } from "@/components/composer/use-composer-draft";
import { FakeWsClient } from "@/test/fake-ws-client";
import type { Provider, ThinkingLevel } from "@/lib/types";

interface Submission {
  provider: Provider;
  prompt: string;
  permissionMode: string;
  autoAccept: boolean;
  thinkingLevel?: ThinkingLevel;
}

/** provider.list with served mode catalogs (ADR-0019), GLM's being agent-discovered. */
const SERVED_PROVIDERS = {
  providers: [
    {
      id: "claude-native",
      label: "Claude Code",
      defaultMode: "acceptEdits",
      liveModeSwitch: true,
      modes: [
        { id: "acceptEdits", label: "Accept File Edits", description: "Automatically approves edit-focused tools without prompting" },
        { id: "default", label: "Always Ask" },
        { id: "plan", label: "Plan Mode" },
        { id: "auto", label: "Auto mode", autoApproves: true },
        { id: "bypassPermissions", label: "Bypass", description: "Skip all permission prompts (use with caution)", autoApproves: true },
      ],
    },
    {
      id: "glm",
      label: "GLM",
      defaultMode: "default",
      modesDiscovered: true,
      supportsAutoAccept: true,
      liveModeSwitch: true,
      modes: [
        { id: "default", label: "Default" },
        { id: "accept_edits", label: "Accept Edits" },
        { id: "bypass_permissions", label: "Bypass Permissions", autoApproves: true },
      ],
    },
  ],
};

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
    chatId: 10,
    isDefaultChat: true,
    boundProvider: null,
    connected: true,
    runningRunId: null,
    queuedItems: [],
    onCancelQueued: async () => {},
    onSubmit: async (provider, prompt, permission, thinkingLevel) => {
      submissions.push({ provider, prompt, ...permission, thinkingLevel });
      return { kind: "started", runId: "run-test" };
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

/** A minimal DaemonEvents fake: topic -> listeners, plus an emit helper. */
function fakeEvents(): DaemonEvents & { emit(topic: string, payload: unknown): void } {
  const listeners = new Map<string, Set<(payload: unknown) => void>>();
  return {
    subscribe(topic, listener) {
      let set = listeners.get(topic);
      if (!set) {
        set = new Set();
        listeners.set(topic, set);
      }
      set.add(listener);
      return () => set!.delete(listener);
    },
    emit(topic, payload) {
      for (const fn of listeners.get(topic) ?? []) fn(payload);
    },
  };
}

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

function queueItem(partial: Partial<ChatQueueItem> & { ID: number }): ChatQueueItem {
  return {
    ChatID: 10,
    Prompt: `prompt ${partial.ID}`,
    RunConfig: "{}",
    Source: "human",
    FromTaskID: null,
    FromChatID: null,
    Priority: 0,
    Status: "queued",
    RunID: null,
    CancelReason: "",
    CancelKind: "",
    CreatedAt: "2026-10-11T00:00:00Z",
    UpdatedAt: "2026-10-11T00:00:00Z",
    ...partial,
  };
}

function runningSummary(): RunSummary {
  return {
    ID: "run-live",
    TaskID: TASK.ID,
    ChatID: 10,
    Provider: "claude-native",
    Prompt: "first turn",
    Status: "running",
    StartedAt: "2024-01-01T00:00:00Z",
    FinishedAt: null,
    StopReason: "",
    Err: "",
    PermissionMode: "acceptEdits",
    AutoAccept: false,
    ThinkingLevel: "",
  };
}

/** Renders the whole pane (the real useChatQueue + composer wiring) against a fake client -- the integration harness for the server-queue tests. */
function renderPane(client: FakeWsClient, events: DaemonEvents, chatId = 10) {
  const chat = {
    ID: chatId,
    TaskID: TASK.ID,
    Title: "Chat",
    Provider: null,
    AgentSession: null,
    CreatedAt: "2024-01-01T00:00:00Z",
    ArchivedAt: null,
  };
  const view = render(<TaskDetailPane client={client} task={TASK} chat={chat} isDefaultChat events={events} />);
  return {
    rerender: (nextChatId: number) =>
      view.rerender(<TaskDetailPane client={client} task={TASK} chat={{ ...chat, ID: nextChatId }} isDefaultChat events={events} />),
    unmount: view.unmount,
  };
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
    expect(submissions).toEqual([{ provider: "claude-native", prompt: "line one", permissionMode: "", autoAccept: false }]);
  });

  it("Cmd+Enter is not a plain submit key", () => {
    // The alternate-send shortcut (ADR-0021 §9) owns modifier-held
    // Enters -- the textarea must not fire the plain submit underneath
    // the composer's own Cmd/Ctrl+Enter handler.
    const key = (over: object) => ({ shiftKey: false, metaKey: false, ctrlKey: false, nativeEvent: {}, ...over }) as never;
    expect(isSubmitKey(key({ key: "Enter", metaKey: true }))).toBe(false);
    expect(isSubmitKey(key({ key: "Enter", ctrlKey: true }))).toBe(false);
    expect(isSubmitKey(key({ key: "Enter" }))).toBe(true);
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
    // Item 5: honest placeholder -- no @file/command claims.
    expect(textarea()).toHaveAttribute("placeholder", "Message the agent…");
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

    // Item 5: the visible <label> text moved out of the card's toolbar --
    // the accessible name now comes from aria-label on the label-less
    // trigger, which is still what getByLabelText resolved through.
    expect(screen.queryByText("Provider")).not.toBeInTheDocument();
    expect(screen.queryByText("Permission mode")).not.toBeInTheDocument();
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

    expect(submissions).toEqual([{ provider: "glm", prompt: "do the thing", permissionMode: "", autoAccept: false }]);
  });

  it("the permission-mode dropdown lists the provider's own modes, in its own words, and submits the pick (W1)", async () => {
    const client = new FakeWsClient();
    const { submissions } = renderComposer({ client });
    client.nth("provider.list", 0).resolve(SERVED_PROVIDERS);
    await flush();

    fireEvent.click(screen.getByLabelText("Permission mode"));
    const options = await screen.findAllByRole("option");
    expect(options.map((o) => o.textContent)).toEqual(["Accept File Edits", "Always Ask", "Plan Mode", "Auto mode", "Bypass"]);
    const bypass = options[4];
    expect(bypass).toHaveAttribute("title", expect.stringContaining("Skip all permission prompts"));

    fireEvent.click(bypass);
    await flush();

    fireEvent.change(textarea(), { target: { value: "go wild" } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    await flush();

    expect(submissions).toEqual([{ provider: "claude-native", prompt: "go wild", permissionMode: "bypassPermissions", autoAccept: false }]);
  });

  it("before provider.list answers, the static catalog keeps the mode dropdown usable", async () => {
    renderComposer();
    fireEvent.click(screen.getByLabelText("Permission mode"));
    const options = await screen.findAllByRole("option");
    expect(options.map((o) => o.textContent)).toEqual(["Accept File Edits", "Always Ask", "Plan Mode", "Bypass"]);
  });

  it("Shift+Tab in the composer cycles the provider's modes, wrapping, and submits the cycled-to value (W3)", async () => {
    const { submissions } = renderComposer();

    const trigger = () => screen.getByLabelText("Permission mode");
    expect(trigger()).toHaveTextContent("Accept File Edits");

    fireEvent.keyDown(textarea(), { key: "Tab", shiftKey: true });
    expect(trigger()).toHaveTextContent("Always Ask");

    fireEvent.keyDown(textarea(), { key: "Tab", shiftKey: true });
    fireEvent.keyDown(textarea(), { key: "Tab", shiftKey: true });
    expect(trigger()).toHaveTextContent("Bypass");

    // Wraps past the last mode back to the first.
    fireEvent.keyDown(textarea(), { key: "Tab", shiftKey: true });
    expect(trigger()).toHaveTextContent("Accept File Edits");

    fireEvent.keyDown(textarea(), { key: "Tab", shiftKey: true });
    fireEvent.change(textarea(), { target: { value: "ask me" } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    await flush();

    expect(submissions).toEqual([{ provider: "claude-native", prompt: "ask me", permissionMode: "default", autoAccept: false }]);
  });

  it("a plain Tab (no Shift) in the composer does not touch the permission mode", async () => {
    renderComposer();
    const trigger = () => screen.getByLabelText("Permission mode");

    fireEvent.keyDown(textarea(), { key: "Tab" });

    expect(trigger()).toHaveTextContent("Accept File Edits");
  });

  it("switching provider resets to that provider's own default mode; an ACP provider adds the Auto-accept toggle (W1)", async () => {
    const client = new FakeWsClient();
    const { submissions } = renderComposer({ client });
    client.nth("provider.list", 0).resolve(SERVED_PROVIDERS);
    await flush();

    fireEvent.click(screen.getByLabelText("Permission mode"));
    fireEvent.click(await screen.findByRole("option", { name: "Plan Mode" }));
    await flush();
    expect(screen.queryByTestId("composer-auto-accept")).not.toBeInTheDocument();

    fireEvent.click(screen.getByLabelText("Provider"));
    fireEvent.click(await screen.findByRole("option", { name: "GLM" }));
    await flush();

    expect(screen.getByLabelText("Permission mode")).toHaveTextContent("Default");
    fireEvent.click(screen.getByLabelText("Permission mode"));
    const options = await screen.findAllByRole("option");
    expect(options.map((o) => o.textContent)).toEqual(["Default", "Accept Edits", "Bypass Permissions"]);
    fireEvent.keyDown(options[0], { key: "Escape" });
    await flush();

    const toggle = screen.getByTestId("composer-auto-accept");
    expect(toggle).toHaveAttribute("aria-pressed", "false");
    fireEvent.click(toggle);
    await flush();
    expect(toggle).toHaveAttribute("aria-pressed", "true");

    fireEvent.change(textarea(), { target: { value: "unattended" } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    await flush();
    expect(submissions).toEqual([{ provider: "glm", prompt: "unattended", permissionMode: "", autoAccept: true }]);
  });

  describe("model picker (ADR-0025)", () => {
    const MODEL_PROVIDERS = {
      providers: [
        {
          id: "claude-native",
          label: "Claude Code",
          models: [
            { id: "sonnet", label: "Sonnet", description: "Balanced" },
            { id: "opus", label: "Opus" },
          ],
        },
        { id: "glm", label: "GLM" },
      ],
    };

    function renderWithModels() {
      const models: (string | undefined)[] = [];
      const client = new FakeWsClient();
      renderComposer({
        client,
        onSubmit: async (_p, _prompt, _perm, _t, _w, model) => {
          models.push(model);
          return { kind: "started", runId: "run-test" };
        },
      });
      client.nth("provider.list", 0).resolve(MODEL_PROVIDERS);
      return { client, models };
    }

    async function send(text: string) {
      fireEvent.change(textarea(), { target: { value: text } });
      fireEvent.click(screen.getByRole("button", { name: "Send" }));
      await flush();
    }

    it("composer sends model on run.start", async () => {
      const { models } = renderWithModels();
      await flush();

      fireEvent.click(screen.getByLabelText("Model"));
      const options = await screen.findAllByRole("option");
      expect(options.map((o) => o.textContent)).toEqual(["Default", "Sonnet", "Opus"]);
      fireEvent.click(options[2]);
      await flush();

      await send("hello");
      expect(models).toEqual(["opus"]);
    });

    it("composer omits model when default is selected", async () => {
      const { models } = renderWithModels();
      await flush();

      expect(screen.getByLabelText("Model")).toHaveTextContent("Default");
      await send("hello");
      expect(models).toEqual([undefined]);

      // Picking a model and then going back to Default omits it again.
      fireEvent.click(screen.getByLabelText("Model"));
      fireEvent.click((await screen.findAllByRole("option"))[1]);
      await flush();
      fireEvent.click(screen.getByLabelText("Model"));
      fireEvent.click((await screen.findAllByRole("option"))[0]);
      await flush();
      await send("again");
      expect(models).toEqual([undefined, undefined]);
    });

    it("composer hides the model picker when the provider lists no models", async () => {
      renderWithModels();
      await flush();
      expect(screen.getByLabelText("Model")).toBeInTheDocument();

      fireEvent.click(screen.getByLabelText("Provider"));
      fireEvent.click(await screen.findByRole("option", { name: "GLM" }));
      await flush();
      expect(screen.queryByLabelText("Model")).not.toBeInTheDocument();
    });

    it("switching provider resets an unavailable model", async () => {
      const { models } = renderWithModels();
      await flush();

      fireEvent.click(screen.getByLabelText("Model"));
      fireEvent.click((await screen.findAllByRole("option"))[1]);
      await flush();

      fireEvent.click(screen.getByLabelText("Provider"));
      fireEvent.click(await screen.findByRole("option", { name: "GLM" }));
      await flush();
      await send("on glm");
      expect(models).toEqual([undefined]);

      // Back on Claude the model starts from Default, not the stale pick.
      fireEvent.click(screen.getByLabelText("Provider"));
      fireEvent.click(await screen.findByRole("option", { name: "Claude Code" }));
      await flush();
      expect(screen.getByLabelText("Model")).toHaveTextContent("Default");
    });
  });

  it("shows a thinking-level selector only for claude-native, and omits the field entirely for every other provider", async () => {
    const client = new FakeWsClient();
    renderComposer({ client });
    client.nth("provider.list", 0).resolve({
      providers: [
        { id: "claude-native", label: "Claude Code" },
        { id: "glm", label: "GLM" },
      ],
    });
    await flush();

    // Default provider is claude-native -- the control is present.
    expect(screen.getByLabelText("Thinking level")).toBeInTheDocument();

    fireEvent.click(screen.getByLabelText("Provider"));
    fireEvent.click(await screen.findByRole("option", { name: "GLM" }));
    await flush();

    // GLM has no pre-run thinking-level control at all (its own lives in
    // the live chat view, once a session exists) -- not merely disabled.
    expect(screen.queryByLabelText("Thinking level")).not.toBeInTheDocument();
  });

  it("thinking level defaults to Standard visually but omits the field until the user actually picks one, same as the permission mode's own default", async () => {
    const { submissions } = renderComposer();

    // Untouched: the control shows "Standard" but the field is left off
    // run.start's payload entirely -- an unmodified Claude submission
    // sends exactly what it did before this selector existed.
    expect(screen.getByLabelText("Thinking level")).toHaveTextContent("Standard");

    fireEvent.change(textarea(), { target: { value: "think about it" } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    await flush();

    expect(submissions).toEqual([
      { provider: "claude-native", prompt: "think about it", permissionMode: "", autoAccept: false },
    ]);
    expect(submissions[0].thinkingLevel).toBeUndefined();
  });

  it("submits the picked thinking level once the selector is actually touched", async () => {
    const { submissions } = renderComposer();

    fireEvent.click(screen.getByLabelText("Thinking level"));
    const options = await screen.findAllByRole("option");
    expect(options.map((o) => o.textContent)).toEqual(["Off", "Standard", "Extended"]);
    fireEvent.click(options[2]);
    await flush();

    fireEvent.change(textarea(), { target: { value: "reason hard" } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    await flush();

    expect(submissions).toEqual([
      { provider: "claude-native", prompt: "reason hard", permissionMode: "", autoAccept: false, thinkingLevel: "extended" },
    ]);
  });

  it("keeps the permission-mode help tooltip and disables both selects while the composer is inactive", () => {
    const { rerender } = renderComposer();

    // acceptEdits is Claude's default, so the trigger's tooltip is that
    // mode's own description (the provider's words).
    const policy = screen.getByLabelText("Permission mode");
    expect(policy).toHaveAttribute("title", expect.stringContaining("edit"));
    expect(policy).not.toBeDisabled();
    expect(screen.getByLabelText("Provider")).not.toBeDisabled();

    rerender({ connected: false });
    expect(screen.getByLabelText("Provider")).toBeDisabled();
    expect(screen.getByLabelText("Permission mode")).toBeDisabled();
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
    for (const label of ["Provider", "Permission mode"]) {
      const trigger = screen.getByLabelText(label);
      expect(trigger.className).toContain("h-11");
      expect(trigger.className).toContain("md:h-7");
    }
  });

  // visual-identity-console Item 4: the Run/Stop control is the "execute"
  // call site -- Send is "execute" while it would start a run, but reverts
  // to the generic "default" once a run is live (the same button now reads
  // "Queue", which isn't a run-control action). Stop is always "execute".
  it("Send is the execute variant when idle, default when it reads Queue; Stop is always execute", () => {
    const { rerender } = renderComposer({ runningRunId: null });
    expect(screen.getByTestId("chat-send-button")).toHaveAttribute("data-variant", "execute");

    rerender({ runningRunId: "run-7" });
    expect(screen.getByTestId("chat-send-button")).toHaveAttribute("data-variant", "default");
    expect(screen.getByTestId("chat-stop-button")).toHaveAttribute("data-variant", "execute");
  });
});

describe("Composer server queue (ADR-0021 §9)", () => {
  beforeEach(() => {
    window.localStorage.clear();
  });

  it("busy send calls run.start with whenBusy queue and renders the queued item from the server", async () => {
    const client = new FakeWsClient();
    const events = fakeEvents();
    renderPane(client, events);

    // A live run plus an initially empty queue.
    client.nth("run.list", 0).resolve([runningSummary()]);
    client.nth("chat.queueList", 0).resolve({ items: [] });
    await flush();

    fireEvent.change(textarea(), { target: { value: "then do this" } });
    fireEvent.click(screen.getByRole("button", { name: "Queue" }));

    const start = client.calls.find((c) => c.method === "run.start")!;
    expect(start.params).toMatchObject({ whenBusy: "queue", prompt: "then do this" });
    act(() => {
      start.resolve({ queued: true, queueItemId: 5 });
    });
    await flush();

    // The onQueued refresh's queueList answers with the server's row.
    client.nth("chat.queueList", 1).resolve({ items: [queueItem({ ID: 5, Prompt: "then do this" })] });
    await flush();

    expect(screen.getByTestId("composer-queue")).toBeInTheDocument();
    expect(screen.getByTestId("composer-queue")).toHaveTextContent("then do this");
    // No run entry was appended for a queued prompt: the timeline still
    // holds exactly the original live run.
    expect(within(screen.getByTestId("run-log-column")).getAllByRole("listitem").length).toBe(1);
  });

  it("idle send also passes whenBusy queue and starts the run", async () => {
    const whenBusy: (string | undefined)[] = [];
    renderComposer({
      onSubmit: async (_provider, _prompt, _permission, _thinkingLevel, busy) => {
        whenBusy.push(busy);
        return { kind: "started", runId: "run-1" };
      },
    });

    fireEvent.change(textarea(), { target: { value: "go" } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    await flush();

    expect(whenBusy).toEqual(["queue"]);
  });

  it("busy send restores the draft when run.start fails", async () => {
    renderComposer({
      runningRunId: "run-1",
      onSubmit: async () => {
        throw new Error("daemon said no");
      },
    });

    fireEvent.change(textarea(), { target: { value: "queued text" } });
    fireEvent.click(screen.getByRole("button", { name: "Queue" }));
    await flush();

    expect(screen.getByText("daemon said no")).toBeInTheDocument();
    expect(textarea()).toHaveValue("queued text");
  });

  it("Cmd+Enter while running sends with whenBusy interrupt", async () => {
    const whenBusy: (string | undefined)[] = [];
    renderComposer({
      runningRunId: "run-1",
      onSubmit: async (_provider, _prompt, _permission, _thinkingLevel, busy) => {
        whenBusy.push(busy);
        return { kind: "started", runId: "run-2" };
      },
    });

    fireEvent.change(textarea(), { target: { value: "right now" } });
    fireEvent.keyDown(textarea(), { key: "Enter", metaKey: true });
    await flush();

    expect(whenBusy).toEqual(["interrupt"]);
  });

  it("Send now button is absent when idle", () => {
    const { rerender } = renderComposer({ runningRunId: null });
    expect(screen.queryByTestId("chat-send-now-button")).not.toBeInTheDocument();

    rerender({ runningRunId: "run-1" });
    expect(screen.getByTestId("chat-send-now-button")).toBeInTheDocument();
  });

  it("cancel button calls chat.queueCancel with the item id", async () => {
    const client = new FakeWsClient();
    const events = fakeEvents();
    renderPane(client, events);

    client.nth("run.list", 0).resolve([]);
    client.nth("chat.queueList", 0).resolve({ items: [queueItem({ ID: 5, Prompt: "waiting" })] });
    await flush();

    fireEvent.click(screen.getByRole("button", { name: "Cancel queued prompt: waiting" }));

    expect(client.calls.some((c) => c.method === "chat.queueCancel")).toBe(true);
    expect(client.calls.find((c) => c.method === "chat.queueCancel")!.params).toEqual({ itemId: 5 });
  });

  it("cancel button is keyboard operable and labelled with the prompt", async () => {
    const cancelled: number[] = [];
    renderComposer({
      queuedItems: [queueItem({ ID: 7, Prompt: "keyboard reachable prompt" })],
      onCancelQueued: (itemId) => {
        cancelled.push(itemId);
      },
    });

    // A real <button> with an accessible name is what makes Enter/Space
    // click it from the keyboard -- asserted structurally, plus the click.
    const button = screen.getByRole("button", { name: "Cancel queued prompt: keyboard reachable prompt" });
    expect(button.tagName).toBe("BUTTON");
    expect(button).not.toBeDisabled();
    expect(button).not.toHaveAttribute("tabindex", "-1");

    fireEvent.click(button);
    expect(cancelled).toEqual([7]);
  });

  it("queued items from the server do not fire client-side on run end", async () => {
    const client = new FakeWsClient();
    const events = fakeEvents();
    renderPane(client, events);

    client.nth("run.list", 0).resolve([runningSummary()]);
    client.nth("chat.queueList", 0).resolve({ items: [queueItem({ ID: 5, Prompt: "waiting" })] });
    await flush();

    // The live run ends (its attach reaches the terminal result).
    act(() => {
      client.nth("run.attach", 0).resolve({ runId: "run-live", stopReason: "end_turn" });
    });
    await flush();

    // A queued row exists and the run is over -- nothing client-side may
    // fire it; that would be a second run.start (there was never a first).
    expect(screen.getByTestId("composer-queue")).toHaveTextContent("waiting");
    expect(client.calls.filter((c) => c.method === "run.start")).toHaveLength(0);

    // The row leaves only when the server's snapshot says so.
    act(() => {
      events.emit("chat.queueUpdated", { chatId: 10, items: [] });
    });
    expect(screen.queryByTestId("composer-queue")).not.toBeInTheDocument();
  });

  it("a second submit while one is in flight sends once", async () => {
    const sends: string[] = [];
    renderComposer({
      onSubmit: async (_provider, prompt) => {
        sends.push(prompt);
        return new Promise(() => {});
      },
    });

    fireEvent.change(textarea(), { target: { value: "one at a time" } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    await flush();

    // The submit is still unresolved (textarea disabled); a second Enter
    // and a second click must both be no-ops.
    fireEvent.keyDown(textarea(), { key: "Enter" });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    await flush();

    expect(sends).toEqual(["one at a time"]);
  });

  it("shows only the open chat's queue after a chat switch", async () => {
    const client = new FakeWsClient();
    const events = fakeEvents();
    const { rerender } = renderPane(client, events, 10);

    client.nth("run.list", 0).resolve([]);
    client.nth("chat.queueList", 0).resolve({ items: [queueItem({ ID: 5, ChatID: 10, Prompt: "chat ten's item" })] });
    await flush();
    expect(screen.getByTestId("composer-queue")).toHaveTextContent("chat ten's item");

    rerender(20);
    await flush();
    client.nth("chat.queueList", 1).resolve({ items: [] });
    await flush();
    expect(screen.queryByTestId("composer-queue")).not.toBeInTheDocument();

    // A late snapshot for the old chat must not resurrect its row.
    act(() => {
      events.emit("chat.queueUpdated", { chatId: 10, items: [queueItem({ ID: 5, ChatID: 10 })] });
    });
    expect(screen.queryByTestId("composer-queue")).not.toBeInTheDocument();
  });

  it("renders an auto-cancelled item's reason", () => {
    renderComposer({
      queuedItems: [queueItem({ ID: 5, Status: "cancelled", CancelKind: "auto", CancelReason: "delivery failed: queue is full" })],
    });

    expect(screen.getByText("Not sent")).toBeInTheDocument();
    expect(screen.getByText("delivery failed: queue is full")).toBeInTheDocument();
  });
});

describe("Composer diff-stat pill (Item 5)", () => {
  it("renders the pill when the task has changes, and clicking it opens the diff tab", () => {
    const onOpenDiff = vi.fn();
    renderComposer({
      diffStat: { files: 2, additions: 12, deletions: 3 },
      onOpenDiff,
    });

    const pill = screen.getByTestId("composer-diff-stat");
    expect(pill).toHaveTextContent("+12");
    expect(pill).toHaveTextContent("\u22123"); // minus sign, not a hyphen

    fireEvent.click(pill);
    expect(onOpenDiff).toHaveBeenCalledTimes(1);
  });

  it("omits the pill when the task has no changes, no stat, or no diff tab to open", () => {
    const { rerender } = renderComposer({ diffStat: { files: 0, additions: 0, deletions: 0 }, onOpenDiff: vi.fn() });
    expect(screen.queryByTestId("composer-diff-stat")).not.toBeInTheDocument();

    // Changes exist but there is no Diff tab to jump to (no onOpenDiff).
    rerender({ diffStat: { files: 1, additions: 5, deletions: 0 }, onOpenDiff: undefined });
    expect(screen.queryByTestId("composer-diff-stat")).not.toBeInTheDocument();

    rerender({ diffStat: null, onOpenDiff: vi.fn() });
    expect(screen.queryByTestId("composer-diff-stat")).not.toBeInTheDocument();
  });

  it("wraps the textarea and toolbar in one input card with no placeholder + button", () => {
    renderComposer({ diffStat: { files: 1, additions: 4, deletions: 1 }, onOpenDiff: vi.fn() });

    const card = screen.getByTestId("composer-card");
    expect(card).toContainElement(screen.getByLabelText("Prompt"));
    expect(card).toContainElement(screen.getByLabelText("Provider"));
    expect(card).toContainElement(screen.getByTestId("chat-send-button"));
    // No dead affordances: Stop only exists while a run is live, and
    // there is no attachment "+" until attachments exist.
    expect(screen.queryByTestId("chat-stop-button")).not.toBeInTheDocument();
    expect(card.querySelector("[aria-label~=attachment]")).not.toBeInTheDocument();
  });

  // ADR-0014's Profiles picker: client-side seed of provider/permissionMode/
  // thinkingLevel from a saved profile, per the ADR's "How a profile is
  // applied" section.
  describe("Profiles picker (ADR-0014)", () => {
    it("does not render when profile.list resolves empty (AC15 regression)", async () => {
      const client = new FakeWsClient();
      renderComposer({ client });

      client.nth("profile.list", 0).resolve([]);
      await flush();

      expect(screen.queryByLabelText("Agents")).not.toBeInTheDocument();
    });

    it("does not render while profile.list is still pending, and every other control works as before", async () => {
      const client = new FakeWsClient();
      const { submissions } = renderComposer({ client });

      // profile.list is left unresolved -- mirrors a daemon predating this
      // feature just as well as one that's merely slow to answer.
      expect(screen.queryByLabelText("Agents")).not.toBeInTheDocument();

      fireEvent.change(textarea(), { target: { value: "still works" } });
      fireEvent.click(screen.getByRole("button", { name: "Send" }));
      await flush();

      expect(submissions).toEqual([{ provider: "claude-native", prompt: "still works", permissionMode: "", autoAccept: false }]);
    });

    it("selecting a profile seeds provider/permissionMode/autoAccept/thinkingLevel in one click", async () => {
      const client = new FakeWsClient();
      renderComposer({ client });

      client.nth("provider.list", 0).resolve(SERVED_PROVIDERS);
      client.nth("profile.list", 0).resolve([
        {
          ID: 1,
          Name: "UI work",
          Provider: "glm",
          PermissionMode: "accept_edits",
          AutoAccept: true,
          ThinkingLevel: "",
          Notes: "",
          CreatedAt: "2026-09-25T00:00:00Z",
          UpdatedAt: "2026-09-25T00:00:00Z",
        },
      ]);
      await flush();

      fireEvent.pointerDown(screen.getByLabelText("Agents"), { button: 0 });
      fireEvent.click(await screen.findByTestId("agent-menu-item-1"));
      await flush();

      expect(screen.getByLabelText("Provider")).toHaveTextContent("GLM");
      expect(screen.getByLabelText("Permission mode")).toHaveTextContent("Accept Edits");
      expect(screen.getByTestId("composer-auto-accept")).toHaveAttribute("aria-pressed", "true");
      // The trigger now shows the applied agent's own name (run-config IA:
      // the toolbar tracks which agent is active, not just a one-time seed).
      expect(screen.getByLabelText("Agents")).toHaveTextContent("UI work");
    });

    it("hand-editing a field after applying a profile flips the trigger to Custom · from <name>, and ↺ restores the agent's values", async () => {
      const client = new FakeWsClient();
      const { submissions } = renderComposer({ client });

      client.nth("profile.list", 0).resolve([
        {
          ID: 1,
          Name: "UI work",
          Provider: "claude-native",
          PermissionMode: "plan",
          AutoAccept: false,
          ThinkingLevel: "",
          Notes: "",
          CreatedAt: "2026-09-25T00:00:00Z",
          UpdatedAt: "2026-09-25T00:00:00Z",
        },
      ]);
      await flush();

      fireEvent.pointerDown(screen.getByLabelText("Agents"), { button: 0 });
      fireEvent.click(await screen.findByTestId("agent-menu-item-1"));
      await flush();
      expect(screen.getByLabelText("Permission mode")).toHaveTextContent("Plan Mode");
      expect(screen.getByLabelText("Agents")).toHaveTextContent("UI work");
      expect(screen.queryByTestId("composer-agent-reset")).not.toBeInTheDocument();

      fireEvent.click(screen.getByLabelText("Permission mode"));
      fireEvent.click(await screen.findByRole("option", { name: "Accept File Edits" }));
      await flush();
      expect(screen.getByLabelText("Permission mode")).toHaveTextContent("Accept File Edits");
      // Hand-editing flips the Agent trigger to a "Custom · from <name>"
      // hint -- the change is a per-run override, not written back to the
      // stored profile (nothing here calls profile.update).
      expect(screen.getByLabelText("Agents")).toHaveTextContent("Custom · from UI work");
      expect(client.calls.some((c) => c.method === "profile.update")).toBe(false);

      // Sending still submits the hand-edited value -- the toolbar never
      // silently reverts an override the user made.
      fireEvent.change(textarea(), { target: { value: "go" } });
      fireEvent.click(screen.getByRole("button", { name: "Send" }));
      await flush();
      expect(submissions).toEqual([{ provider: "claude-native", prompt: "go", permissionMode: "acceptEdits", autoAccept: false }]);

      // The ↺ reset restores the picked agent's own stored values.
      fireEvent.click(screen.getByTestId("composer-agent-reset"));
      await flush();
      expect(screen.getByLabelText("Permission mode")).toHaveTextContent("Plan Mode");
      expect(screen.getByLabelText("Agents")).toHaveTextContent("UI work");
      expect(screen.queryByTestId("composer-agent-reset")).not.toBeInTheDocument();
    });

    it('the agent menu offers "No agent" and "Manage agents…", the latter opening Settings -> Agents', async () => {
      const client = new FakeWsClient();
      renderComposer({ client });

      client.nth("profile.list", 0).resolve([
        {
          ID: 1,
          Name: "UI work",
          Provider: "claude-native",
          PermissionMode: "",
          AutoAccept: false,
          ThinkingLevel: "",
          Notes: "",
          CreatedAt: "2026-09-25T00:00:00Z",
          UpdatedAt: "2026-09-25T00:00:00Z",
        },
      ]);
      await flush();

      fireEvent.pointerDown(screen.getByLabelText("Agents"), { button: 0 });
      fireEvent.click(await screen.findByTestId("agent-menu-item-1"));
      await flush();
      expect(screen.getByLabelText("Agents")).toHaveTextContent("UI work");

      fireEvent.pointerDown(screen.getByLabelText("Agents"), { button: 0 });
      fireEvent.click(await screen.findByTestId("agent-menu-no-agent"));
      await flush();
      expect(screen.getByLabelText("Agents")).toHaveTextContent("No agent");

      const onOpenSettings = vi.fn();
      window.addEventListener("smind:open-settings", onOpenSettings);
      fireEvent.pointerDown(screen.getByLabelText("Agents"), { button: 0 });
      fireEvent.click(await screen.findByTestId("agent-menu-manage"));
      window.removeEventListener("smind:open-settings", onOpenSettings);

      expect(onOpenSettings).toHaveBeenCalledTimes(1);
      expect((onOpenSettings.mock.calls[0][0] as CustomEvent).detail).toEqual({ sectionId: "agents" });
    });
  });

  // run-config IA: the toolbar's state is persisted per task
  // (docs/design.md §9), and a task that's never had one persisted starts
  // from the ★ default agent (Settings -> Agents).
  describe("run-config persistence and the ★ default agent (run-config IA)", () => {
    const PROFILE = {
      ID: 1,
      Name: "UI work",
      Provider: "glm",
      PermissionMode: "",
      AutoAccept: true,
      ThinkingLevel: "",
      Notes: "",
      CreatedAt: "2026-09-25T00:00:00Z",
      UpdatedAt: "2026-09-25T00:00:00Z",
    };

    it("keeps a per-task run-config across a task switch, persisted to storage", async () => {
      const client = new FakeWsClient();
      const { rerender } = renderComposer({ client, taskId: 1 });
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
      expect(window.localStorage.getItem("smind:run-config:1:10")).toContain('"provider":"glm"');

      // Task 2 has never had a run-config persisted -- starts from the
      // plain EMPTY_STATE default, not task 1's GLM pick.
      rerender({ taskId: 2 });
      await flush();
      expect(screen.getByLabelText("Provider")).toHaveTextContent("Claude Code");

      // Switching back to task 1 reads its own persisted config back.
      rerender({ taskId: 1 });
      await flush();
      expect(screen.getByLabelText("Provider")).toHaveTextContent("GLM");
    });

    it("a task with no persisted run-config starts from the ★ default agent", async () => {
      window.localStorage.setItem("smind:settings:defaultAgentId", "1");
      const client = new FakeWsClient();
      renderComposer({ client, taskId: 5 });

      client.nth("provider.list", 0).resolve({
        providers: [
          { id: "claude-native", label: "Claude Code" },
          { id: "glm", label: "GLM" },
        ],
      });
      client.nth("profile.list", 0).resolve([PROFILE]);
      await flush();

      expect(screen.getByLabelText("Provider")).toHaveTextContent("GLM");
      expect(screen.getByTestId("composer-auto-accept")).toHaveAttribute("aria-pressed", "true");
      expect(screen.getByLabelText("Agents")).toHaveTextContent("UI work");
    });

    it("a task that already has a persisted run-config is not overridden by the ★ default agent", async () => {
      window.localStorage.setItem("smind:settings:defaultAgentId", "1");
      window.localStorage.setItem(
        "smind:run-config:5:10",
        JSON.stringify({ baseAgentId: null, custom: false, provider: "claude-native", permissionMode: "", autoAccept: false, thinkingLevel: "" }),
      );
      const client = new FakeWsClient();
      renderComposer({ client, taskId: 5 });

      client.nth("provider.list", 0).resolve({
        providers: [
          { id: "claude-native", label: "Claude Code" },
          { id: "glm", label: "GLM" },
        ],
      });
      client.nth("profile.list", 0).resolve([PROFILE]);
      await flush();

      expect(screen.getByLabelText("Provider")).toHaveTextContent("Claude Code");
      expect(screen.getByLabelText("Agents")).toHaveTextContent("No agent");
    });

    it("a pre-ADR-0019 persisted run-config loads with the provider's default mode, never a mapped legacy policy (W2)", async () => {
      window.localStorage.setItem(
        "smind:run-config:5:10",
        JSON.stringify({ baseAgentId: null, custom: false, provider: "claude-native", approvalPolicy: "full-access", thinkingLevel: "extended" }),
      );
      const { submissions } = renderComposer({ taskId: 5 });

      expect(screen.getByLabelText("Permission mode")).toHaveTextContent("Accept File Edits");
      expect(screen.getByLabelText("Thinking level")).toHaveTextContent("Extended");

      fireEvent.change(textarea(), { target: { value: "hi" } });
      fireEvent.click(screen.getByRole("button", { name: "Send" }));
      await flush();
      expect(submissions).toEqual([{ provider: "claude-native", prompt: "hi", permissionMode: "", autoAccept: false, thinkingLevel: "extended" }]);
      expect(window.localStorage.getItem("smind:run-config:5:10")).not.toContain("approvalPolicy");
    });
  });

  describe("bound provider (ADR-0016 P3: a chat's provider is immutable after its first run)", () => {
    it("forces the Provider selector to the chat's bound provider, read-only, and keeps it there", async () => {
      const client = new FakeWsClient();
      renderComposer({ client, boundProvider: "glm" });

      client.nth("provider.list", 0).resolve({
        providers: [
          { id: "claude-native", label: "Claude Code" },
          { id: "glm", label: "GLM" },
        ],
      });
      await flush();

      const select = screen.getByLabelText("Provider");
      expect(select).toHaveTextContent("GLM");
      expect(select).toHaveAttribute("data-disabled");
      expect(select).toHaveAttribute("title", expect.stringContaining("bound to GLM"));

      // Radix's Select fires a spurious onValueChange("") of its own once
      // a disabled select's value settles (not from any user click) --
      // regression coverage for the real bug that surfaced this: it must
      // never blank out the selector.
      await flush();
      expect(select).toHaveTextContent("GLM");
    });
  });
});
