// Sidebar polish (docs/plans/active/sidebar-polish.md): row-level behavior of
// the task sidebar -- scoped actions (S1), keyboard (S2), the single status
// slot (S3), the single-line row (S4), container rows (S5), the task context
// menu (S6), title fade + per-row indentation (S7) and persisted expansion
// (S8). The older app-sidebar*.test.tsx files keep the pre-existing contracts.
import { act } from "react";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { AppSidebar } from "@/components/app-sidebar";
import { SidebarProvider } from "@/components/ui/sidebar";
import { FakeWsClient } from "@/test/fake-ws-client";
import { hoverTask, keyboardFocus, openHoverCard, revealSpaceActions, revealWorkspaceActions, taskItem, taskTrigger, unhoverTask } from "@/test/sidebar-helpers";
import type { AttentionReason, TaskAttention, TaskRunStatus } from "@/hooks/use-task-attention";
import type { Space, Task, Workspace } from "@/lib/types";

const WORKSPACE: Workspace = {
  ID: 1,
  Path: "/tmp/ws",
  Title: "My Workspace",
  RoutingPolicy: "",
  CreatedAt: "2024-01-01T00:00:00Z",
  UpdatedAt: "2024-01-01T00:00:00Z",
};

function makeTask(id: number, title: string, extra: Partial<Task> = {}): Task {
  return {
    ID: id,
    WorkspaceID: 1,
    SpaceID: null,
    Title: title,
    Status: "active",
    WorktreePath: null,
    Branch: null,
    CreatedAt: "2024-01-01T00:00:00Z",
    UpdatedAt: "2024-01-01T00:00:00Z",
    ArchivedAt: null,
    ...extra,
  };
}

// A three-tier tree: root (supervisor) > leader > coder, plus a sibling leader.
const ROOT = makeTask(1, "Root task");
const LEADER = makeTask(2, "Leader task", { ParentTaskID: 1 });
const CODER = makeTask(3, "Coder task", { ParentTaskID: 2 });
const LEADER_B = makeTask(4, "Leader B", { ParentTaskID: 1 });
const TREE = [ROOT, LEADER, CODER, LEADER_B];

const SPACE_A: Space = {
  ID: 10,
  WorkspaceID: 1,
  Title: "Space A",
  EnvData: "",
  CreatedAt: "2024-01-01T00:00:00Z",
  UpdatedAt: "2024-01-01T00:00:00Z",
};

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

async function mountSidebar(
  tasks: Task[],
  props: Partial<Parameters<typeof AppSidebar>[0]> = {},
  spaces: Space[] = [],
) {
  const client = new FakeWsClient();
  const utils = render(
    <SidebarProvider>
      <AppSidebar client={client as never} {...props} />
    </SidebarProvider>,
  );
  client.nth("workspace.list", 0).resolve([WORKSPACE]);
  await flush();
  client.nth("space.list", 0).resolve(spaces);
  client.nth("task.list", 0).resolve(tasks);
  await flush();
  await screen.findAllByTestId("sidebar-task-row");
  return { client, ...utils };
}

function triggerCount(): number {
  return screen.queryAllByTestId("sidebar-task-actions-trigger").length;
}

afterEach(() => {
  window.localStorage.clear();
});

function rowButton(taskId: number): HTMLElement {
  return within(taskItem(taskId)).getByTestId("sidebar-task-row");
}

function toggleOf(taskId: number): HTMLElement {
  return within(taskItem(taskId)).getByTestId("task-collapse-toggle");
}

describe("task row actions (S1)", () => {
  it("keyboard focus within a row mounts its actions, and blur unmounts them", async () => {
    await mountSidebar(TREE);

    keyboardFocus(rowButton(LEADER.ID));
    expect(screen.getAllByTestId("sidebar-task-actions-trigger").map((t) => t.getAttribute("data-task-id"))).toEqual([
      String(LEADER.ID),
    ]);

    act(() => rowButton(LEADER.ID).blur());
    expect(triggerCount()).toBe(0);
  });

  it("hovering a task row mounts only that row's actions", async () => {
    await mountSidebar(TREE);
    expect(triggerCount()).toBe(0);

    hoverTask(LEADER.ID);

    const triggers = screen.getAllByTestId("sidebar-task-actions-trigger");
    expect(triggers).toHaveLength(1);
    expect(triggers[0]).toHaveAttribute("data-task-id", String(LEADER.ID));
  });

  it("hovering a child row does not mount its parent's or siblings' actions", async () => {
    await mountSidebar(TREE);

    hoverTask(CODER.ID);

    const owners = screen.getAllByTestId("sidebar-task-actions-trigger").map((t) => t.getAttribute("data-task-id"));
    // The coder's ancestors (leader, root) and its parent's sibling stay quiet.
    expect(owners).toEqual([String(CODER.ID)]);
  });

  it("hovering a parent row does not mount its descendants' actions", async () => {
    await mountSidebar(TREE);

    hoverTask(ROOT.ID);

    const owners = screen.getAllByTestId("sidebar-task-actions-trigger").map((t) => t.getAttribute("data-task-id"));
    expect(owners).toEqual([String(ROOT.ID)]);
  });

  it("leaving a row unmounts its actions", async () => {
    await mountSidebar(TREE);
    hoverTask(LEADER.ID);
    expect(triggerCount()).toBe(1);

    unhoverTask(LEADER.ID);

    expect(triggerCount()).toBe(0);
  });

  it("an open menu keeps its row's actions mounted after the pointer leaves", async () => {
    await mountSidebar(TREE);
    const trigger = taskTrigger(LEADER.ID);

    fireEvent.pointerDown(trigger, { button: 0, ctrlKey: false });
    await screen.findByTestId("sidebar-task-pin-action");
    unhoverTask(LEADER.ID);

    expect(screen.getByTestId("sidebar-task-actions-trigger")).toBeInTheDocument();
  });
});

describe("task row keyboard (S2)", () => {
  it("a task row is a focusable button, and selecting it invokes onSelectTask", async () => {
    const onSelectTask = vi.fn();
    await mountSidebar(TREE, { onSelectTask });

    const row = rowButton(LEADER.ID);
    // A real <button type=button>: Tab reaches it and Enter/Space activate it
    // natively (the browser turns both into a click).
    expect(row.tagName).toBe("BUTTON");
    expect(row).toHaveAttribute("type", "button");
    expect(row).not.toHaveAttribute("tabindex", "-1");
    act(() => row.focus());
    expect(row).toHaveFocus();

    fireEvent.click(row);
    expect(onSelectTask).toHaveBeenCalledWith(expect.objectContaining({ ID: LEADER.ID }));
  });

  it("ArrowDown and ArrowUp move focus between visible task rows", async () => {
    await mountSidebar(TREE);
    act(() => rowButton(ROOT.ID).focus());

    fireEvent.keyDown(rowButton(ROOT.ID), { key: "ArrowDown" });
    expect(rowButton(LEADER.ID)).toHaveFocus();
    fireEvent.keyDown(rowButton(LEADER.ID), { key: "ArrowDown" });
    expect(rowButton(CODER.ID)).toHaveFocus();
    fireEvent.keyDown(rowButton(CODER.ID), { key: "ArrowUp" });
    expect(rowButton(LEADER.ID)).toHaveFocus();
  });

  it("arrow navigation skips rows hidden under a collapsed parent", async () => {
    await mountSidebar(TREE);
    fireEvent.click(toggleOf(LEADER.ID));
    expect(screen.queryByText("Coder task")).not.toBeInTheDocument();
    act(() => rowButton(LEADER.ID).focus());

    fireEvent.keyDown(rowButton(LEADER.ID), { key: "ArrowDown" });

    expect(rowButton(LEADER_B.ID)).toHaveFocus();
  });

  it("ArrowDown on the last row stays put", async () => {
    await mountSidebar(TREE);
    act(() => rowButton(LEADER_B.ID).focus());

    fireEvent.keyDown(rowButton(LEADER_B.ID), { key: "ArrowDown" });

    expect(rowButton(LEADER_B.ID)).toHaveFocus();
  });

  it("ArrowLeft collapses an expanded parent and ArrowRight expands it again", async () => {
    await mountSidebar(TREE);
    expect(toggleOf(LEADER.ID)).toHaveAttribute("aria-expanded", "true");

    fireEvent.keyDown(rowButton(LEADER.ID), { key: "ArrowLeft" });
    expect(screen.queryByText("Coder task")).not.toBeInTheDocument();
    expect(toggleOf(LEADER.ID)).toHaveAttribute("aria-expanded", "false");

    fireEvent.keyDown(rowButton(LEADER.ID), { key: "ArrowRight" });
    expect(screen.getByText("Coder task")).toBeInTheDocument();
  });

  it("ArrowRight on an expanded parent steps into its first child", async () => {
    await mountSidebar(TREE);
    act(() => rowButton(LEADER.ID).focus());

    fireEvent.keyDown(rowButton(LEADER.ID), { key: "ArrowRight" });

    expect(rowButton(CODER.ID)).toHaveFocus();
  });

  it("ArrowLeft on a childless row moves focus to its parent", async () => {
    await mountSidebar(TREE);
    act(() => rowButton(CODER.ID).focus());

    fireEvent.keyDown(rowButton(CODER.ID), { key: "ArrowLeft" });

    expect(rowButton(LEADER.ID)).toHaveFocus();
  });

  it("modified arrow keys are left to the browser", async () => {
    await mountSidebar(TREE);
    act(() => rowButton(ROOT.ID).focus());

    fireEvent.keyDown(rowButton(ROOT.ID), { key: "ArrowDown", altKey: true });

    expect(rowButton(ROOT.ID)).toHaveFocus();
  });

  it("keeps the nested list structure and aria-expanded on the collapse toggle", async () => {
    await mountSidebar(TREE);

    const rootLi = taskItem(ROOT.ID).closest("li")!;
    const childList = Array.from(rootLi.children).find((c) => c.tagName === "UL");
    expect(childList).toBeTruthy();
    expect(childList!.contains(taskItem(LEADER.ID))).toBe(true);
    expect(toggleOf(ROOT.ID)).toHaveAttribute("aria-expanded", "true");
  });
});

describe("task row status slot (S3)", () => {
  const SIGNAL_TASK = makeTask(7, "Signal task");

  function slotOf(taskId: number): HTMLElement {
    return within(taskItem(taskId)).getByTestId("task-status-slot");
  }

  function signals(opts: { reasons?: AttentionReason[]; run?: string; unread?: boolean }) {
    const attention: TaskAttention = new Map(
      opts.reasons ? [[SIGNAL_TASK.ID, new Set<AttentionReason>(opts.reasons)]] : [],
    );
    const runStatus: TaskRunStatus = new Map(opts.run ? [[SIGNAL_TASK.ID, opts.run as never]] : []);
    return { attention, runStatus, unread: new Set(opts.unread ? [SIGNAL_TASK.ID] : []) };
  }

  it("renders exactly one status slot per row, at the same width with or without a signal", async () => {
    const { rerender, client } = await mountSidebar([SIGNAL_TASK], signals({}));
    expect(within(taskItem(SIGNAL_TASK.ID)).getAllByTestId("task-status-slot")).toHaveLength(1);
    const idleClass = slotOf(SIGNAL_TASK.ID).className;
    expect(slotOf(SIGNAL_TASK.ID).children).toHaveLength(0);

    rerender(
      <SidebarProvider>
        <AppSidebar client={client as never} {...signals({ reasons: ["error"] })} />
      </SidebarProvider>,
    );

    expect(slotOf(SIGNAL_TASK.ID).className).toBe(idleClass);
    expect(slotOf(SIGNAL_TASK.ID).children).toHaveLength(1);
  });

  it.each([
    // [name, signals, expected winning kind, expected inner testid]
    ["permission beats everything", { reasons: ["permission", "finished"], run: "running", unread: true }, "attention", "task-attention"],
    ["error beats a running run and unread", { reasons: ["error"], run: "running", unread: true }, "attention", "task-attention"],
    ["a run that errored beats running and unread", { run: "error", unread: true }, "run", "task-run-status"],
    ["running beats finished and unread", { reasons: ["finished"], run: "running", unread: true }, "run", "task-run-status"],
    ["finished beats unread", { reasons: ["finished"], unread: true }, "attention", "task-attention"],
    ["unread alone", { unread: true }, "unread", "task-unread-marker"],
    ["a run that is merely done is not a signal", { run: "done" }, "none", null],
    ["no signal", {}, "none", null],
  ] as const)("priority: %s", async (_name, sig, kind, testId) => {
    await mountSidebar([SIGNAL_TASK], signals(sig as never));

    const slot = slotOf(SIGNAL_TASK.ID);
    expect(slot).toHaveAttribute("data-slot-signal", kind);
    // Exactly one indicator is drawn, and it is the winner.
    if (testId === null) {
      expect(slot.children).toHaveLength(0);
    } else {
      expect(slot.children).toHaveLength(1);
      expect(within(slot).getByTestId(testId)).toBeInTheDocument();
    }
  });

  it("exposes data-attention-reason, data-run-status and data-unread on the slot", async () => {
    await mountSidebar([SIGNAL_TASK], signals({ reasons: ["permission"], run: "running", unread: true }));

    const slot = slotOf(SIGNAL_TASK.ID);
    expect(slot).toHaveAttribute("data-attention-reason", "permission");
    expect(slot).toHaveAttribute("data-run-status", "running");
    expect(slot).toHaveAttribute("data-unread", "true");
  });

  it("an idle row's slot carries no signal attributes", async () => {
    await mountSidebar([SIGNAL_TASK], signals({}));

    const slot = slotOf(SIGNAL_TASK.ID);
    expect(slot).not.toHaveAttribute("data-attention-reason");
    expect(slot).not.toHaveAttribute("data-run-status");
    expect(slot).not.toHaveAttribute("data-unread");
  });
});

describe("task row, single line (S4)", () => {
  const NOW = new Date("2024-06-01T12:00:00Z").getTime();

  it("is one 32px line: no meta row, no second line of text", async () => {
    await mountSidebar([makeTask(1, "Solo", { Branch: "smind/task-solo-abc" })]);

    expect(screen.queryByTestId("sidebar-task-meta")).not.toBeInTheDocument();
    const item = taskItem(1);
    expect(item.className).toContain("h-8");
    // Title, then the right cluster -- the branch is not rendered on the row.
    expect(item.textContent).not.toContain("smind/task-solo-abc");
  });

  it("shows a compact relative time from UpdatedAt in the right cluster", async () => {
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(NOW);
    try {
      await mountSidebar([
        makeTask(1, "Recent", { UpdatedAt: "2024-06-01T11:11:00Z" }),
        makeTask(2, "Older", { UpdatedAt: "2024-05-29T12:00:00Z" }),
      ]);

      expect(within(taskItem(1)).getByTestId("task-row-time")).toHaveTextContent("49m");
      expect(within(taskItem(2)).getByTestId("task-row-time")).toHaveTextContent("3d");
    } finally {
      vi.useRealTimers();
    }
  });

  it("shows +n -n only when files changed", async () => {
    const { client } = await mountSidebar([makeTask(1, "Dirty"), makeTask(2, "Clean")]);
    client.nth("task.stats", 0).resolve({
      stats: [
        { taskId: 1, branch: "b1", filesChanged: 2, insertions: 7, deletions: 3 },
        { taskId: 2, branch: "b2", filesChanged: 0, insertions: 0, deletions: 0 },
      ],
    });
    await flush();

    expect(within(taskItem(1)).getByTestId("sidebar-task-diffstat")).toHaveTextContent("+7 -3");
    expect(within(taskItem(2)).queryByTestId("sidebar-task-diffstat")).not.toBeInTheDocument();
  });

  it("the actions replace the time in the same slot on hover, without reflowing the row", async () => {
    await mountSidebar([makeTask(1, "Hover me")]);
    const time = within(taskItem(1)).getByTestId("task-row-time");
    const widthClass = time.className.replace("invisible", "").replace(/\s+/g, " ").trim();
    expect(time.className).not.toContain("invisible");

    hoverTask(1);

    // The time keeps its box (invisible, not removed) and the trigger is overlaid.
    const after = within(taskItem(1)).getByTestId("task-row-time");
    expect(after.className).toContain("invisible");
    expect(after.className.replace("invisible", "").replace(/\s+/g, " ").trim()).toBe(widthClass);
    expect(within(taskItem(1)).getByTestId("sidebar-task-actions-trigger")).toBeInTheDocument();
  });

  it("keeps the branch and tier in the hover card and the accessible name, not on the row", async () => {
    await mountSidebar(TREE.map((t) => ({ ...t, Branch: `br-${t.ID}` })));

    const leader = rowButton(LEADER.ID);
    expect(leader).toHaveAccessibleName("Leader task, leader with 1 sub-task, branch br-2");
    expect(leader.textContent).not.toContain("br-2");
    const card = await openHoverCard(LEADER.ID);
    expect(within(card).getByTestId("sidebar-task-branch")).toHaveTextContent("br-2");
    expect(within(card).getByTestId("task-tier")).toHaveTextContent("leader · 1 sub-task");
  });

  it("parents keep the child-count chip and the collapsed rollup", async () => {
    const attention: TaskAttention = new Map([[CODER.ID, new Set<AttentionReason>(["permission"])]]);
    await mountSidebar(TREE, { attention });

    expect(within(taskItem(ROOT.ID)).getByTestId("task-child-count")).toHaveTextContent("2");
    expect(within(taskItem(CODER.ID)).queryByTestId("task-child-count")).not.toBeInTheDocument();
    expect(within(taskItem(LEADER.ID)).queryByTestId("task-rollup")).not.toBeInTheDocument();

    fireEvent.click(toggleOf(LEADER.ID));

    const rollup = within(taskItem(LEADER.ID)).getByTestId("task-rollup");
    expect(rollup).toHaveAttribute("data-rollup-reason", "permission");
    expect(within(taskItem(LEADER.ID)).getByTestId("task-child-count")).toHaveTextContent("1");
  });
});

describe("container rows (S5)", () => {
  const IN_SPACE = makeTask(5, "Spaced task", { SpaceID: SPACE_A.ID });

  it("workspace and space actions are not mounted at idle and mount on hover", async () => {
    await mountSidebar([IN_SPACE], {}, [SPACE_A]);
    expect(screen.queryByTestId("sidebar-workspace-actions-trigger")).not.toBeInTheDocument();
    expect(screen.queryByTestId("sidebar-space-actions-trigger")).not.toBeInTheDocument();

    await revealSpaceActions();
    // Hovering the space header mounts the space's trigger only -- not the workspace's.
    expect(screen.getByTestId("sidebar-space-actions-trigger")).toBeInTheDocument();
    expect(screen.queryByTestId("sidebar-workspace-actions-trigger")).not.toBeInTheDocument();

    fireEvent.mouseLeave(screen.getByTestId("sidebar-space-row").parentElement!);
    await revealWorkspaceActions();
    expect(screen.getByTestId("sidebar-workspace-actions-trigger")).toBeInTheDocument();
    expect(screen.queryByTestId("sidebar-space-actions-trigger")).not.toBeInTheDocument();
  });

  it("keyboard focus within a workspace or space header mounts its actions", async () => {
    await mountSidebar([IN_SPACE], {}, [SPACE_A]);

    keyboardFocus(screen.getByTestId("sidebar-space-row"));
    expect(screen.getByTestId("sidebar-space-actions-trigger")).toBeInTheDocument();
    act(() => screen.getByTestId("sidebar-space-row").blur());
    expect(screen.queryByTestId("sidebar-space-actions-trigger")).not.toBeInTheDocument();

    keyboardFocus(screen.getByTestId("sidebar-workspace-row"));
    expect(screen.getByTestId("sidebar-workspace-actions-trigger")).toBeInTheDocument();
  });

  it("an open space menu keeps its trigger mounted after the pointer leaves", async () => {
    await mountSidebar([IN_SPACE], {}, [SPACE_A]);
    fireEvent.pointerDown(await revealSpaceActions(), { button: 0, ctrlKey: false });
    await screen.findByText("Delete space");

    fireEvent.mouseLeave(screen.getByTestId("sidebar-space-row").parentElement!);

    expect(screen.getByTestId("sidebar-space-actions-trigger")).toBeInTheDocument();
  });

  it("the space ⋯ is outside the header button, which reserves its own right padding, so it can never sit on the chevron", async () => {
    await mountSidebar([IN_SPACE], {}, [SPACE_A]);
    const trigger = await revealSpaceActions();
    const header = screen.getByTestId("sidebar-space-row");

    expect(header.contains(trigger)).toBe(false);
    expect(header.className).toContain("pr-8");
    // The chevron sits right after the title inside the button, not at the far right.
    const chevron = within(header).getByTestId("row-chevron");
    expect(chevron.previousElementSibling).toBe(within(header).getByText("Space A"));
    expect(chevron.className).not.toContain("ml-auto");
  });

  it("workspace, space, Pinned and status-group headers all use the same chevron", async () => {
    window.localStorage.setItem("smind:pinned-tasks", JSON.stringify([IN_SPACE.ID]));
    await mountSidebar([IN_SPACE], {}, [SPACE_A]);

    const chevronClass = (header: HTMLElement) =>
      within(header).getByTestId("row-chevron").getAttribute("class")!.replace("rotate-90", "").replace(/\s+/g, " ").trim();
    const workspace = chevronClass(screen.getByTestId("sidebar-workspace-row"));
    expect(chevronClass(screen.getByTestId("sidebar-space-row"))).toBe(workspace);
    expect(chevronClass(screen.getByTestId("sidebar-pinned-section-header"))).toBe(workspace);

    fireEvent.click(screen.getByTestId("sidebar-group-mode-toggle"));
    const groupHeader = (await screen.findAllByTestId("sidebar-status-group-header"))[0]!;
    expect(chevronClass(groupHeader)).toBe(workspace);
    // Muted, small, token-only.
    expect(workspace).toContain("size-3.5");
    expect(workspace).toContain("text-foreground-subtlest");
  });

  it("a status group's count is a pill", async () => {
    await mountSidebar([makeTask(5, "A"), makeTask(6, "B")]);

    fireEvent.click(screen.getByTestId("sidebar-group-mode-toggle"));

    const pill = (await screen.findAllByTestId("status-group-count"))[0]!;
    expect(pill.className).toContain("rounded-full");
    expect(pill).toHaveTextContent("2");
  });
});

describe("task context menu (S6)", () => {
  const IN_A = makeTask(5, "In A", { SpaceID: SPACE_A.ID });
  const SPACE_B: Space = { ...SPACE_A, ID: 11, Title: "Space B" };

  async function menuLabels(menu: HTMLElement): Promise<string[]> {
    return within(menu)
      .getAllByRole("menuitem")
      .map((el) => el.textContent?.trim() ?? "");
  }

  it("right-click offers the same items as the ⋯ menu", async () => {
    await mountSidebar([IN_A], {}, [SPACE_A, SPACE_B]);

    fireEvent.pointerDown(taskTrigger(IN_A.ID), { button: 0, ctrlKey: false });
    const dropdown = await menuLabels(await screen.findByRole("menu"));
    fireEvent.keyDown(document.activeElement ?? document.body, { key: "Escape" });
    await flush();

    fireEvent.contextMenu(rowButton(IN_A.ID));
    const context = await menuLabels(await screen.findByTestId("sidebar-task-context-menu"));

    expect(context).toEqual(dropdown);
    expect(context).toEqual(["Move to space", "Pin", "Mark unread", "Archive task"]);
  });

  it("context menu Pin, Mark unread and Archive call the same handlers as the ⋯ menu", async () => {
    const onMarkUnread = vi.fn();
    await mountSidebar([IN_A], { onMarkUnread }, [SPACE_A]);

    fireEvent.contextMenu(rowButton(IN_A.ID));
    fireEvent.click(await screen.findByTestId("sidebar-task-mark-unread-action"));
    expect(onMarkUnread).toHaveBeenCalledWith(IN_A.ID);

    fireEvent.contextMenu(rowButton(IN_A.ID));
    fireEvent.click(await screen.findByTestId("sidebar-task-pin-action"));
    expect(await screen.findByTestId("sidebar-pinned-section")).toBeInTheDocument();

    fireEvent.contextMenu(rowButton(IN_A.ID));
    fireEvent.click(await screen.findByTestId("sidebar-task-archive-action"));
    // The archive confirm dialog is the ⋯ menu's too.
    expect(await screen.findByText("Archive task?")).toBeInTheDocument();
  });

  it("context menu Move to space lists the other spaces and moves with task.move", async () => {
    const { client } = await mountSidebar([IN_A], {}, [SPACE_A, SPACE_B]);

    fireEvent.contextMenu(rowButton(IN_A.ID));
    const trigger = await screen.findByTestId("sidebar-task-move-action");
    fireEvent.pointerMove(trigger, { pointerType: "mouse" });
    fireEvent.keyDown(trigger, { key: "ArrowRight" });
    await flush();
    const targets = await screen.findAllByTestId("sidebar-task-move-target");
    expect(targets.map((t) => t.textContent)).toEqual(["Ungrouped", "Space B"]);

    fireEvent.click(targets[1]!);
    const move = await waitFor(() => client.nth("task.move"));
    expect(move.params).toEqual({ id: IN_A.ID, spaceId: SPACE_B.ID });
  });

  it("an open context menu keeps its row's actions mounted", async () => {
    await mountSidebar(TREE);

    fireEvent.contextMenu(rowButton(LEADER.ID));
    await screen.findByTestId("sidebar-task-context-menu");

    expect(screen.getByTestId("sidebar-task-actions-trigger")).toHaveAttribute("data-task-id", String(LEADER.ID));
  });
});

describe("title overflow and indentation (S7)", () => {
  function title(taskId: number): HTMLElement {
    return within(taskItem(taskId)).getByTestId("task-title");
  }

  function stubOverflow(scrollWidth: number, clientWidth: number) {
    const sw = vi.spyOn(HTMLElement.prototype, "scrollWidth", "get").mockReturnValue(scrollWidth);
    const cw = vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockReturnValue(clientWidth);
    return () => {
      sw.mockRestore();
      cw.mockRestore();
    };
  }

  it("the title fade mask applies only when the title overflows", async () => {
    const restore = stubOverflow(300, 120);
    try {
      await mountSidebar([makeTask(1, "An extremely long task title that cannot possibly fit")]);
      expect(title(1)).toHaveAttribute("data-overflowing", "true");
      expect(title(1).className).toContain("mask-image");
      // A fade, not a hard ellipsis.
      expect(title(1).className).not.toContain("truncate");
      expect(title(1).className).not.toContain("text-ellipsis");
    } finally {
      restore();
    }
  });

  it("a title that fits has no mask", async () => {
    const restore = stubOverflow(80, 120);
    try {
      await mountSidebar([makeTask(1, "Short")]);
      expect(title(1)).not.toHaveAttribute("data-overflowing");
      expect(title(1).className).not.toContain("mask-image");
    } finally {
      restore();
    }
  });

  it("indentation is per-row padding by depth: 12px per level, one gutter more where the root list has parents", async () => {
    await mountSidebar(TREE);

    // The root list contains a parent, so roots start 12px past the 8px inset
    // (the gutter for the chevron); each level below adds 12px.
    expect(taskItem(ROOT.ID).style.paddingLeft).toBe("20px");
    expect(taskItem(LEADER.ID).style.paddingLeft).toBe("32px");
    expect(taskItem(CODER.ID).style.paddingLeft).toBe("44px");
    // Siblings at one depth share one left edge.
    expect(taskItem(LEADER_B.ID).style.paddingLeft).toBe("32px");
  });

  it("a root list without parents keeps the plain 8px inset", async () => {
    await mountSidebar([makeTask(1, "A"), makeTask(2, "B")]);

    expect(taskItem(1).style.paddingLeft).toBe("8px");
    expect(taskItem(2).style.paddingLeft).toBe("8px");
  });

  it("a parent's chevron sits in the gutter directly left of its own status slot", async () => {
    await mountSidebar(TREE);

    for (const id of [ROOT.ID, LEADER.ID]) {
      const pad = parseInt(taskItem(id).style.paddingLeft, 10);
      expect(toggleOf(id).style.left).toBe(`${pad - 12}px`);
    }
  });

  it("nested lists add no margin, padding or guide line, so every highlight spans the sidebar", async () => {
    await mountSidebar(TREE, {}, [SPACE_A]);

    const lists = Array.from(document.querySelectorAll<HTMLElement>('ul[data-sidebar="menu-sub"]'));
    expect(lists.length).toBeGreaterThan(1);
    for (const list of lists) {
      expect(list.className, "a nested list must not indent its rows").toContain("mx-0");
      expect(list.className).toContain("px-0");
      expect(list.className).toContain("border-l-0");
    }
  });

  it("a row's own highlight is the whole line: the wrapper carries the selected background", async () => {
    await mountSidebar(TREE, { selectedTaskId: CODER.ID });

    expect(taskItem(CODER.ID).className).toContain("bg-selected");
    expect(taskItem(LEADER.ID).className).not.toContain("bg-selected");
  });

  it("the nested ul/li structure is intact: a child's li is inside its parent's li", async () => {
    await mountSidebar(TREE);

    const leaderLi = taskItem(LEADER.ID).closest("li")!;
    const rootLi = taskItem(ROOT.ID).closest("li")!;
    expect(rootLi.contains(leaderLi)).toBe(true);
    expect(leaderLi.contains(taskItem(CODER.ID))).toBe(true);
    expect(leaderLi.contains(taskItem(LEADER_B.ID))).toBe(false);
  });
});
