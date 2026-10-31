import { act, cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { HomeLanding } from "@/components/home/home-landing";
import { GREETINGS, greetingBucket } from "@/lib/greeting";
import { renderWithClientProp as render } from "@/test/render-with-daemon";
import type { ProviderInfo, Task, Workspace } from "@/lib/types";
import type { WsClientLike } from "@/lib/ws-client";
import { useHomeProjectStore } from "@/stores/home-project-store";
import { useHomeRunConfigStore } from "@/stores/home-run-config-store";
import { OFFICE_SUGGESTED_PROMPTS } from "@/lib/suggested-prompts";
import { useInterfaceModeStore } from "@/stores/interface-mode-store";
import { usePendingSendStore } from "@/stores/pending-send-store";

function ws(ID: number, Title: string): Workspace {
  return { ID, Title, Path: `/repos/${Title.toLowerCase()}`, RoutingPolicy: "", CreatedAt: "", UpdatedAt: "" };
}

const THREE = [ws(1, "Alpha"), ws(2, "Beta"), ws(3, "Gamma")];
const SEVEN = [ws(1, "Alpha"), ws(2, "Beta"), ws(3, "Gamma"), ws(4, "Delta"), ws(5, "Epsilon"), ws(6, "Zeta"), ws(7, "Eta")];

const PROVIDERS: ProviderInfo[] = [
  {
    id: "claude-native",
    label: "Claude Code",
    defaultMode: "acceptEdits",
    liveModeSwitch: true,
    modes: [
      { id: "acceptEdits", label: "Accept File Edits" },
      { id: "default", label: "Always Ask" },
      { id: "plan", label: "Plan Mode" },
    ],
    models: [{ id: "claude-opus-5-5", label: "Opus 5.5" }],
  },
];

function fakeClient() {
  const task = { ID: 42, WorkspaceID: 2, Title: "Fix the bug" } as Task;
  const call = vi.fn(async (method: string, _params?: unknown) => {
    if (method === "provider.list") return { providers: PROVIDERS };
    if (method === "profile.list" || method === "run.list") return [];
    return task;
  });
  return { client: { call, callStream: vi.fn() } as unknown as WsClientLike, call, task };
}

function renderHome(props: Partial<React.ComponentProps<typeof HomeLanding>> = {}) {
  const { client, call, task } = fakeClient();
  const onCreated = vi.fn();
  const utils = render(<HomeLanding client={client} connected workspaces={THREE} onCreated={onCreated} {...props} />);
  return { ...utils, call, task, onCreated };
}

async function flush(): Promise<void> {
  await act(async () => {
    for (let i = 0; i < 5; i++) await Promise.resolve();
  });
}

function createCalls(call: { mock: { calls: unknown[][] } }) {
  return call.mock.calls.filter(([m]) => m === "task.create");
}

function type(text: string) {
  fireEvent.change(screen.getByLabelText("Prompt"), { target: { value: text } });
}

function pick(id: number) {
  fireEvent.click(screen.getByTestId("home-project-chip"));
  fireEvent.click(screen.getByTestId(`home-project-option-${id}`));
}

describe("HomeLanding", () => {
  beforeEach(() => {
    window.localStorage.clear();
    useHomeProjectStore.getState().reload();
    useHomeRunConfigStore.getState().reload();
    usePendingSendStore.setState({ entry: null });
  });
  afterEach(() => {
    cleanup();
    window.localStorage.clear();
    useHomeProjectStore.getState().reload();
    useHomeRunConfigStore.getState().reload();
    usePendingSendStore.setState({ entry: null });
  });

  it("renders mark, greeting, project chip and composer", () => {
    renderHome();
    expect(screen.getByTestId("home-illustration")).toBeInTheDocument();
    expect(GREETINGS[greetingBucket(new Date().getHours())]).toContain(screen.getByRole("heading", { level: 1 }).textContent);
    expect(screen.getByTestId("home-project-chip")).toHaveTextContent("Alpha");
    expect(screen.getByTestId("home-composer-card")).toContainElement(screen.getByLabelText("Prompt"));
  });

  it("submit creates a task in the chosen project and selects it", async () => {
    const { call, task, onCreated } = renderHome();
    pick(2);
    type("Fix the bug");
    fireEvent.click(screen.getByTestId("home-send-button"));
    await waitFor(() => expect(onCreated).toHaveBeenCalledWith(task));
    expect(call).toHaveBeenCalledWith("task.create", { workspaceId: 2, title: "Fix the bug" });
  });

  it("submit seeds the composer draft with the prompt", async () => {
    const { onCreated } = renderHome();
    const prompt = "Fix the bug\n\nIt crashes on start";
    type(prompt);
    fireEvent.click(screen.getByTestId("home-send-button"));
    await waitFor(() => expect(onCreated).toHaveBeenCalled());
    expect(window.localStorage.getItem("smind:composer-draft:42")).toBe(prompt);
  });

  it("title is the first line truncated to 60 chars", async () => {
    const { call, onCreated } = renderHome();
    type(`${"a".repeat(100)}\nsecond`);
    fireEvent.click(screen.getByTestId("home-send-button"));
    await waitFor(() => expect(onCreated).toHaveBeenCalledTimes(1));
    const title = (createCalls(call)[0][1] as { title: string }).title;
    expect(title).toHaveLength(60);
    expect(title.endsWith("…")).toBe(true);

    type("\n  Second   line  ");
    fireEvent.click(screen.getByTestId("home-send-button"));
    await waitFor(() => expect(onCreated).toHaveBeenCalledTimes(2));
    expect((createCalls(call)[1][1] as { title: string }).title).toBe("Second line");
  });

  it("send is disabled when empty, offline or without projects", () => {
    const empty = renderHome();
    expect(screen.getByTestId("home-send-button")).toBeDisabled();
    empty.unmount();

    const offline = renderHome({ connected: false });
    expect(screen.getByLabelText("Prompt")).toBeDisabled();
    expect(screen.getByTestId("home-send-button")).toBeDisabled();
    offline.unmount();

    renderHome({ workspaces: [] });
    expect(screen.getByLabelText("Prompt")).toBeDisabled();
    expect(screen.getByPlaceholderText("Add a project to start a task")).toBeInTheDocument();
    expect(screen.getByTestId("home-send-button")).toBeDisabled();
  });

  it("project menu filters by search and picks a project", async () => {
    const small = renderHome();
    fireEvent.click(screen.getByTestId("home-project-chip"));
    expect(screen.queryByTestId("home-project-search")).not.toBeInTheDocument();
    small.unmount();

    const { call, onCreated } = renderHome({ workspaces: SEVEN });
    fireEvent.click(screen.getByTestId("home-project-chip"));
    fireEvent.change(screen.getByTestId("home-project-search"), { target: { value: "ta" } });
    const options = within(screen.getByRole("listbox")).getAllByRole("option");
    expect(options.map((o) => o.textContent)).toEqual([
      expect.stringContaining("Beta"),
      expect.stringContaining("Delta"),
      expect.stringContaining("Zeta"),
      expect.stringContaining("Eta"),
    ]);
    fireEvent.click(screen.getByTestId("home-project-option-4"));
    type("Ship it");
    fireEvent.click(screen.getByTestId("home-send-button"));
    await waitFor(() => expect(onCreated).toHaveBeenCalled());
    expect(call).toHaveBeenCalledWith("task.create", { workspaceId: 4, title: "Ship it" });
  });

  it("project choice is remembered", () => {
    const first = renderHome();
    pick(3);
    expect(screen.getByTestId("home-project-chip")).toHaveTextContent("Gamma");
    first.unmount();

    renderHome();
    expect(screen.getByTestId("home-project-chip")).toHaveTextContent("Gamma");
  });

  it("Add project calls the handler", () => {
    const onAddProject = vi.fn();
    renderHome({ onAddProject });
    fireEvent.click(screen.getByTestId("home-project-chip"));
    fireEvent.click(screen.getByTestId("home-project-add"));
    expect(onAddProject).toHaveBeenCalledTimes(1);
  });

  it("landing submit creates and sends once", async () => {
    const { call, onCreated } = renderHome();
    await flush();
    type("Fix the bug");
    const send = screen.getByTestId("home-send-button");
    fireEvent.click(send);
    fireEvent.click(send);
    fireEvent.submit(screen.getByTestId("home-composer"));
    await waitFor(() => expect(onCreated).toHaveBeenCalledTimes(1));
    expect(call.mock.calls.filter(([m]) => m === "task.create")).toHaveLength(1);
    expect(call.mock.calls.some(([m]) => m === "run.start")).toBe(false);
    const entry = usePendingSendStore.getState().entry;
    expect(entry?.taskId).toBe(42);
    expect(entry?.config.provider).toBe("claude-native");
    expect(window.localStorage.getItem("smind:composer-draft:42")).toBe("Fix the bug");
  });

  it("send failure keeps the draft", async () => {
    const { call, onCreated } = renderHome();
    await flush();
    call.mockImplementation(async (method: string) => {
      if (method === "task.create") throw new Error("boom");
      return method === "provider.list" ? { providers: PROVIDERS } : [];
    });
    type("Fix the bug");
    fireEvent.click(screen.getByTestId("home-send-button"));
    expect(await screen.findByRole("alert")).toHaveTextContent("boom");
    expect(screen.getByLabelText("Prompt")).toHaveValue("Fix the bug");
    expect(usePendingSendStore.getState().entry).toBeNull();
    expect(onCreated).not.toHaveBeenCalled();
    expect(window.localStorage.getItem("smind:composer-draft:42")).toBeNull();
  });

  it("landing chips apply to the created chat", async () => {
    const { onCreated } = renderHome();
    await flush();
    fireEvent.click(screen.getByLabelText("Permission mode"));
    const options = await screen.findAllByRole("option");
    fireEvent.click(options.find((o) => (o.getAttribute("aria-label") ?? o.textContent) === "Plan Mode")!);
    await flush();
    type("Plan it");
    fireEvent.click(screen.getByTestId("home-send-button"));
    await waitFor(() => expect(onCreated).toHaveBeenCalled());
    const entry = usePendingSendStore.getState().entry;
    expect(entry?.config.permissionMode).toBe("plan");
    expect(JSON.stringify(entry)).not.toContain("chatId");
  });

  it("chips default to the last used", async () => {
    useHomeRunConfigStore.getState().set({
      baseAgentId: null,
      custom: true,
      provider: "claude-native",
      permissionMode: "plan",
      autoAccept: false,
      thinkingLevel: "",
      model: "",
    });
    const { onCreated } = renderHome();
    await flush();
    expect(screen.getByLabelText("Permission mode")).toHaveTextContent("Plan Mode");
    type("Again");
    fireEvent.click(screen.getByTestId("home-send-button"));
    await waitFor(() => expect(onCreated).toHaveBeenCalled());
    expect(usePendingSendStore.getState().entry?.config).toMatchObject({ provider: "claude-native", permissionMode: "plan" });
  });
});

describe("HomeLanding office suggestions", () => {
  beforeEach(() => {
    window.localStorage.clear();
    useInterfaceModeStore.getState().reload();
    useHomeProjectStore.getState().reload();
    useHomeRunConfigStore.getState().reload();
    usePendingSendStore.setState({ entry: null });
  });
  afterEach(() => {
    cleanup();
    window.localStorage.clear();
    useInterfaceModeStore.getState().reload();
  });

  it("coding mode shows no suggestion chips", () => {
    renderHome();
    expect(screen.queryByTestId("home-suggestions")).toBeNull();
  });

  it("four_chips_fill_never_send", async () => {
    useInterfaceModeStore.getState().set("office");
    const { call } = renderHome();
    await flush();
    const chips = screen.getAllByTestId("home-suggestion");
    expect(chips.map((c) => c.textContent)).toEqual([...OFFICE_SUGGESTED_PROMPTS]);
    expect(chips).toHaveLength(4);
    fireEvent.click(chips[1]);
    await flush();
    expect((screen.getByLabelText("Prompt") as HTMLTextAreaElement).value).toBe(OFFICE_SUGGESTED_PROMPTS[1]);
    expect(createCalls(call)).toHaveLength(0);
    expect(call.mock.calls.map(([m]) => m)).not.toContain("run.start");
  });
});
