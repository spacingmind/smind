import { act } from "react";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { App } from "@/App";
import { WsClient } from "@/lib/ws-client";
import { FakeSocket } from "@/test/fake-socket";
import "@/test/query-scheduler";
import { resetTerminalSessions } from "@/lib/terminal-sessions";
import type { Chat, Task, Workspace } from "@/lib/types";

// Mutable so one test can make the app believe it runs in a task window.
const windowState = vi.hoisted(() => ({ taskWindow: false, desktop: false }));
// nav.back / nav.forward are bound on the desktop build only: flip the platform flag for the one test that needs them.
vi.mock("@/lib/platform", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/platform")>();
  const mocked = Object.create(null) as typeof actual;
  for (const key of Object.keys(actual) as Array<keyof typeof actual>) {
    Object.defineProperty(mocked, key, { enumerable: true, get: () => (key === "isDesktop" ? windowState.desktop : actual[key]) });
  }
  return mocked;
});
vi.mock("@/lib/task-window", () => ({
  get isTaskWindow() {
    return windowState.taskWindow;
  },
  isTaskWindowLabel: (label: unknown) => typeof label === "string" && /^task-[1-8]$/.test(label),
}));

vi.setConfig({ testTimeout: 30_000 });

const WORKSPACE: Workspace = {
  ID: 1,
  Path: "/tmp/ws",
  Title: "My Workspace",
  RoutingPolicy: "",
  CreatedAt: "2024-01-01T00:00:00Z",
  UpdatedAt: "2024-01-01T00:00:00Z",
};

const TASK_A: Task = {
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
const TASK_B: Task = { ...TASK_A, ID: 2, Title: "Task B", Branch: "task-b" };

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

function respond(socket: FakeSocket, method: string, result: unknown, index = 0): void {
  const env = socket.sent.filter((e) => e.method === method)[index];
  if (!env?.id) throw new Error(`no ${method} request #${index} sent yet`);
  socket.emit({ id: env.id, result });
}

function respondAll(socket: FakeSocket, method: string, result: unknown): void {
  for (const env of socket.sent.filter((e) => e.method === method)) {
    if (env.id) socket.emit({ id: env.id, result });
  }
}

function defaultChatFor(task: Task): Chat {
  return {
    ID: task.ID * 100 + 1,
    TaskID: task.ID,
    Title: "Chat",
    Provider: null,
    AgentSession: null,
    CreatedAt: "2024-01-01T00:00:00Z",
    ArchivedAt: null,
  };
}

function respondChatList(socket: FakeSocket, task: Task): void {
  respondAll(socket, "chat.list", [defaultChatFor(task)]);
}

async function resolveSidebar(socket: FakeSocket, tasks: Task[] = [TASK_A, TASK_B]): Promise<void> {
  await flush();
  respond(socket, "workspace.list", [WORKSPACE]);
  await flush();
  respond(socket, "space.list", []);
  respond(socket, "task.list", tasks);
  respond(socket, "run.list", []);
  await flush();
}

function clickTaskRow(task: Task): void {
  fireEvent.click(screen.getAllByText(task.Title)[0]!);
}

async function mountApp(tasks: Task[] = [TASK_A, TASK_B]): Promise<{ socket: FakeSocket; unmount: () => void }> {
  const socket = new FakeSocket();
  const connect = vi.fn().mockResolvedValue(new WsClient(socket));
  const view = render(<App connect={connect} />);
  await resolveSidebar(socket, tasks);
  return { socket, unmount: view.unmount };
}

async function selectTask(socket: FakeSocket, task: Task): Promise<void> {
  clickTaskRow(task);
  await flush();
  respondChatList(socket, task);
  await flush();
  respondAll(socket, "run.list", []);
  await flush();
}

async function openSettingsEvent(detail?: { sectionId?: string }): Promise<void> {
  await act(async () => {
    window.dispatchEvent(new CustomEvent("smind:open-settings", detail ? { detail } : undefined));
  });
  await flush();
}

async function pressCtrl(key: string, code: string, extra: Record<string, unknown> = {}): Promise<void> {
  await act(async () => {
    fireEvent.keyDown(document, { key, code, ctrlKey: true, ...extra });
  });
  await flush();
}

function expectSettingsOpen(): void {
  const dialog = screen.getByTestId("settings-dialog");
  expect(dialog).toBeInTheDocument();
  expect(within(dialog).getByTestId("settings-screen")).toBeInTheDocument();
}

beforeEach(() => {
  vi.useFakeTimers();
  windowState.taskWindow = false;
  windowState.desktop = false;
});

afterEach(() => {
  vi.useRealTimers();
  window.location.hash = "";
  window.localStorage.clear();
  resetTerminalSessions();
});

describe("App Settings modal (UI cleanup B3)", () => {
  it("ui_cleanup_b3_triggers_open_the_modal", async () => {
    const triggers: Array<[string, (socket: FakeSocket) => Promise<void>]> = [
      ["event, no section", () => openSettingsEvent()],
      ["event, providers", () => openSettingsEvent({ sectionId: "providers" })],
      [
        "sidebar gear",
        async () => {
          fireEvent.click(screen.getByTestId("sidebar-settings-button"));
          await flush();
        },
      ],
      ["Ctrl+,", () => pressCtrl(",", "Comma")],
      [
        "palette row",
        async () => {
          await pressCtrl("k", "KeyK");
          const input = screen.getByTestId("command-palette-input");
          fireEvent.change(input, { target: { value: "open settings" } });
          await flush();
          await act(async () => {
            fireEvent.keyDown(input, { key: "Enter" });
          });
          await flush();
        },
      ],
    ];
    for (const [name, trigger] of triggers) {
      const { socket, unmount } = await mountApp();
      expect(screen.queryByTestId("settings-dialog"), name).not.toBeInTheDocument();
      await trigger(socket);
      expectSettingsOpen();
      unmount();
    }
  });

  it("ui_cleanup_b3_deep_link_lands_on_the_section", async () => {
    await mountApp();
    await openSettingsEvent({ sectionId: "shortcuts" });
    expect(screen.getByTestId("settings-section-shortcuts")).toBeInTheDocument();
    await openSettingsEvent({ sectionId: "general" });
    expect(screen.getByTestId("settings-section-general")).toBeInTheDocument();
    expect(screen.queryByTestId("settings-section-shortcuts")).not.toBeInTheDocument();
  });

  it("ui_cleanup_b3_cmd_comma_toggles_closed", async () => {
    await mountApp();
    await pressCtrl(",", "Comma");
    expectSettingsOpen();
    await pressCtrl(",", "Comma");
    expect(screen.queryByTestId("settings-dialog")).not.toBeInTheDocument();
  });

  it("ui_cleanup_b3_workspace_stays_mounted_and_draft_survives", async () => {
    const { socket } = await mountApp();
    await selectTask(socket, TASK_A);
    const composer = screen.getByLabelText("Prompt");
    fireEvent.change(composer, { target: { value: "half a thought" } });
    expect(screen.getByLabelText("Prompt")).toHaveValue("half a thought");

    await openSettingsEvent();
    expectSettingsOpen();
    const region = screen.getByTestId("content-region");
    expect(region).not.toHaveClass("invisible");
    expect(region).not.toHaveAttribute("inert");
    expect(composer.isConnected).toBe(true);
    expect(composer).toHaveValue("half a thought");

    fireEvent.click(screen.getByTestId("settings-close-button"));
    await flush();
    expect(screen.queryByTestId("settings-dialog")).not.toBeInTheDocument();
    expect(screen.getByLabelText("Prompt")).toHaveValue("half a thought");
  });

  it("ui_cleanup_b3_restriction_active_while_open_and_released_on_close", async () => {
    const { socket } = await mountApp();
    await selectTask(socket, TASK_A);
    const title = () => screen.getByTestId("app-header-title");
    expect(title()).toHaveTextContent(TASK_A.Title);

    await openSettingsEvent();
    // task.next is restricted away while Settings is open.
    await pressCtrl("]", "BracketRight");
    expect(title()).toHaveTextContent(TASK_A.Title);
    // palette.open is a safe action: it still opens over Settings.
    await pressCtrl("k", "KeyK");
    expect(screen.getByTestId("command-palette")).toBeInTheDocument();
    await act(async () => {
      fireEvent.keyDown(screen.getByTestId("command-palette-input"), { key: "Escape" });
    });
    await flush();

    fireEvent.click(screen.getByTestId("settings-close-button"));
    await flush();
    expect(screen.queryByTestId("settings-dialog")).not.toBeInTheDocument();

    await pressCtrl("]", "BracketRight");
    respondChatList(socket, TASK_B);
    await flush();
    expect(title()).toHaveTextContent(TASK_B.Title);
  });

  it("ui_cleanup_b3_task_window_refuses_settings", async () => {
    windowState.taskWindow = true;
    await mountApp();
    await openSettingsEvent();
    expect(screen.queryByTestId("settings-dialog")).not.toBeInTheDocument();
  });

  it("ui_cleanup_b3_overlay_closes_before_settings_opens", async () => {
    await mountApp();
    await act(async () => {
      fireEvent.keyDown(document, { key: "?", code: "Slash", shiftKey: true });
    });
    await flush();
    expect(screen.getByRole("dialog", { name: "Keyboard shortcuts" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Customize in Settings" }));
    await flush();
    expect(screen.queryByRole("dialog", { name: "Keyboard shortcuts" })).not.toBeInTheDocument();
    expect(within(screen.getByTestId("settings-dialog")).getByTestId("settings-section-shortcuts")).toBeInTheDocument();
  });

  it("ui_cleanup_b3_settings_is_not_a_history_entry", async () => {
    windowState.desktop = true;
    const { socket } = await mountApp();
    await selectTask(socket, TASK_A);
    await selectTask(socket, TASK_B);
    const title = () => screen.getByTestId("app-header-title");
    expect(title()).toHaveTextContent(TASK_B.Title);

    await openSettingsEvent();
    fireEvent.click(screen.getByTestId("settings-close-button"));
    await flush();
    expect(screen.queryByTestId("settings-dialog")).not.toBeInTheDocument();

    await act(async () => {
      fireEvent.keyDown(document, { key: "ArrowLeft", code: "ArrowLeft", altKey: true });
    });
    await flush();
    respondChatList(socket, TASK_A);
    await flush();
    expect(title()).toHaveTextContent(TASK_A.Title);
    expect(screen.queryByTestId("settings-dialog")).not.toBeInTheDocument();
  });

  it("ui_cleanup_b3_palette_task_pick_closes_settings", async () => {
    const { socket } = await mountApp();
    await selectTask(socket, TASK_A);
    await openSettingsEvent();
    expectSettingsOpen();

    await pressCtrl("k", "KeyK");
    const input = screen.getByTestId("command-palette-input");
    fireEvent.change(input, { target: { value: TASK_B.Title } });
    await act(async () => {
      fireEvent.keyDown(input, { key: "Enter" });
    });
    await flush();
    respondChatList(socket, TASK_B);
    await flush();

    expect(screen.queryByTestId("settings-dialog")).not.toBeInTheDocument();
    expect(screen.getByTestId("app-header-title")).toHaveTextContent(TASK_B.Title);
  });

  it("ui_cleanup_b3_unmount_while_open_releases_the_page_lock", async () => {
    const { unmount } = await mountApp();
    await openSettingsEvent();
    expectSettingsOpen();
    unmount();
    await flush();
    expect(document.body.style.pointerEvents).not.toBe("none");
    expect(document.body).not.toHaveAttribute("data-scroll-locked");
    expect(document.querySelectorAll("[aria-hidden='true']")).toHaveLength(0);
  });
});
