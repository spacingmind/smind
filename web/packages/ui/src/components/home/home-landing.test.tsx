import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { HomeLanding } from "@/components/home/home-landing";
import { GREETINGS, greetingBucket } from "@/lib/greeting";
import { renderWithClientProp as render } from "@/test/render-with-daemon";
import type { Task, Workspace } from "@/lib/types";
import type { WsClientLike } from "@/lib/ws-client";
import { useHomeProjectStore } from "@/stores/home-project-store";

function ws(ID: number, Title: string): Workspace {
  return { ID, Title, Path: `/repos/${Title.toLowerCase()}`, RoutingPolicy: "", CreatedAt: "", UpdatedAt: "" };
}

const THREE = [ws(1, "Alpha"), ws(2, "Beta"), ws(3, "Gamma")];
const SEVEN = [ws(1, "Alpha"), ws(2, "Beta"), ws(3, "Gamma"), ws(4, "Delta"), ws(5, "Epsilon"), ws(6, "Zeta"), ws(7, "Eta")];

function fakeClient() {
  const task = { ID: 42, WorkspaceID: 2, Title: "Fix the bug" } as Task;
  const call = vi.fn().mockResolvedValue(task);
  return { client: { call, callStream: vi.fn() } as unknown as WsClientLike, call, task };
}

function renderHome(props: Partial<React.ComponentProps<typeof HomeLanding>> = {}) {
  const { client, call, task } = fakeClient();
  const onCreated = vi.fn();
  const utils = render(<HomeLanding client={client} connected workspaces={THREE} onCreated={onCreated} {...props} />);
  return { ...utils, call, task, onCreated };
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
  });
  afterEach(() => {
    cleanup();
    window.localStorage.clear();
    useHomeProjectStore.getState().reload();
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
    const title = (call.mock.calls[0][1] as { title: string }).title;
    expect(title).toHaveLength(60);
    expect(title.endsWith("…")).toBe(true);

    type("\n  Second   line  ");
    fireEvent.click(screen.getByTestId("home-send-button"));
    await waitFor(() => expect(onCreated).toHaveBeenCalledTimes(2));
    expect((call.mock.calls[1][1] as { title: string }).title).toBe("Second line");
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
});
