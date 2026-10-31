import { act } from "react";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { InboxBadge } from "@/components/inbox/inbox-badge";
import { InboxScreen } from "@/components/inbox/inbox-screen";
import { hostDownItem, hostPairItem, hostRefusedItem, InboxDaemon, permissionItem, REQUEST_EVENTS, stallItem, verdictItem } from "@/test/inbox-fixtures";
import { createQueryClient } from "@/lib/query/query-client";
import { queryKeys } from "@/lib/query-keys";
import { fleetHost } from "@/test/fleet-fixtures";
import { renderWithDaemon } from "@/test/render-with-daemon";
import type { WorkspaceWithTree } from "@/lib/workspace-tree";

async function flush(): Promise<void> {
  await act(async () => {
    for (let i = 0; i < 10; i++) await Promise.resolve();
  });
}

async function click(el: HTMLElement): Promise<void> {
  fireEvent.click(el);
  await flush();
}

async function key(el: HTMLElement, k: string): Promise<void> {
  fireEvent.keyDown(el, { key: k });
  await flush();
}

async function renderInbox(daemon: InboxDaemon, props: { onOpenTask?: (id: number) => void; onNavigateBack?: () => void; onOpenSettings?: (id: string) => void } = {}) {
  const view = renderWithDaemon(<InboxScreen onNavigateBack={props.onNavigateBack ?? (() => {})} onOpenTask={props.onOpenTask ?? (() => {})} onOpenSettings={props.onOpenSettings} />, daemon);
  await flush();
  return view;
}

function daemonWith(...items: ReturnType<typeof permissionItem>[]): InboxDaemon {
  const d = new InboxDaemon();
  d.items = items;
  d.logs["run-1"] = REQUEST_EVENTS;
  return d;
}

describe("InboxScreen", () => {
  it("InboxPage_GroupsInOrder: needs you now, validator verdicts, stalled", async () => {
    await renderInbox(daemonWith(stallItem(), verdictItem(), permissionItem()));
    const headings = screen.getAllByRole("heading", { level: 2 }).map((h) => h.textContent);
    expect(headings).toEqual(["Needs you now", "Validator verdicts", "Stalled"]);
    expect(screen.getByTestId("inbox-row-perm:q1")).toHaveAttribute("data-kind", "permission");
    expect(screen.getByTestId("inbox-summary-verdict:7")).toHaveTextContent("ready for your merge decision");
    expect(screen.getByTestId("inbox-kind-verdict:7")).toHaveTextContent("Passed");
  });

  it("TestInboxRowShowsHostChip", async () => {
    const d = daemonWith(hostPairItem(), permissionItem());
    const holder = d.client as unknown as { call: (m: string, p?: unknown) => Promise<unknown> };
    const inner = holder.call;
    holder.call = async (method, params) => (method === "fleet.host.list" ? { hosts: [fleetHost({ hostId: "h1", alias: "mac" })] } : inner.call(d.client, method, params));
    await renderInbox(d);
    expect(screen.getByTestId("inbox-host-chip-host_pair:h1")).toHaveTextContent("mac");
    expect(screen.getAllByTestId(/^inbox-host-chip-/)).toHaveLength(1);
  });

  it("TestInboxScreen_HostKinds", async () => {
    const d = daemonWith(permissionItem(), hostPairItem(), hostRefusedItem(), hostDownItem(), verdictItem());
    const onOpenSettings = vi.fn();
    await renderInbox(d, { onOpenSettings });
    const order = ["permission", "host_pair", "host_refused", "host_down", "verdict"].map((k) => screen.getByTestId(`inbox-group-${k}`));
    for (let i = 1; i < order.length; i++) {
      expect(order[i - 1].compareDocumentPosition(order[i]) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    }
    expect(screen.getByTestId("inbox-kind-host_pair:h1")).toHaveTextContent("Pairing");
    expect(screen.getByTestId("inbox-kind-host_refused:h3:fleet_version_unsupported")).toHaveTextContent("Refused");
    expect(screen.getByTestId("inbox-kind-host_down:h2:1700000000")).toHaveTextContent("Unreachable");

    await click(screen.getByTestId("inbox-hosts-host_pair:h1"));
    expect(onOpenSettings).toHaveBeenCalledWith("hosts");

    for (const k of ["host_pair:h1", "host_refused:h3:fleet_version_unsupported", "host_down:h2:1700000000"]) {
      const row = screen.getByTestId(`inbox-row-${k}`);
      expect(within(row).queryByText("Open task")).toBeNull();
      expect(within(row).getByText("Snooze")).toBeInTheDocument();
      expect(within(row).getByText("Resolve")).toBeInTheDocument();
    }
    await click(screen.getByTestId("inbox-resolve-host_down:h2:1700000000"));
    expect(d.callsOf("inbox.resolve")[0].params).toEqual({ keys: ["host_down:h2:1700000000"] });
  });

  it("InboxPage_EmptyState", async () => {
    await renderInbox(daemonWith());
    expect(screen.getByTestId("inbox-empty")).toHaveTextContent("Nothing needs you");
  });

  it("NoSnoozeOrResolveOnPermissionRow: a permission row offers only Review request", async () => {
    await renderInbox(daemonWith(permissionItem()));
    const row = screen.getByTestId("inbox-row-perm:q1");
    expect(within(row).getByTestId("inbox-review-perm:q1")).toBeInTheDocument();
    expect(within(row).queryByText("Snooze")).toBeNull();
    expect(within(row).queryByText("Resolve")).toBeNull();
  });

  it("PermissionRowOpenTask: a permission row offers Open task, a host_pair row does not", async () => {
    const d = daemonWith(permissionItem(), hostPairItem());
    const onOpenTask = vi.fn();
    await renderInbox(d, { onOpenTask });
    const row = screen.getByTestId("inbox-row-perm:q1");
    expect(within(row).getByText("Open task")).toBeInTheDocument();
    expect(within(screen.getByTestId("inbox-row-host_pair:h1")).queryByText("Open task")).toBeNull();
    await click(within(row).getByTestId("inbox-open-perm:q1"));
    expect(onOpenTask).toHaveBeenCalledWith(5);
  });

  it("NoApproveAllOrMergeControl: nothing on the page approves everything, merges or always-allows", async () => {
    await renderInbox(daemonWith(permissionItem(), verdictItem(), stallItem({ state: "new" })));
    const text = document.body.textContent ?? "";
    expect(text).not.toMatch(/approve all|approve everything|always allow|allow all/i);
    expect(screen.queryByRole("button", { name: /merge/i })).toBeNull();
  });

  it("Keyboard: j/k move between rows, e resolves and s snoozes a verdict, never a permission", async () => {
    const d = daemonWith(permissionItem(), verdictItem(), stallItem());
    await renderInbox(d);
    const perm = screen.getByTestId("inbox-row-perm:q1");
    const verdict = screen.getByTestId("inbox-row-verdict:7");
    perm.focus();
    await key(perm, "j");
    expect(document.activeElement).toBe(verdict);
    await key(verdict, "k");
    expect(document.activeElement).toBe(perm);

    // e and s on a permission row do nothing at all
    await key(perm, "e");
    await key(perm, "s");
    expect(d.callsOf("inbox.resolve")).toHaveLength(0);
    expect(d.callsOf("inbox.snooze")).toHaveLength(0);

    // s snoozes the verdict row for under 24 hours
    await key(verdict, "s");
    const snooze = d.callsOf("inbox.snooze");
    expect(snooze).toHaveLength(1);
    const p = snooze[0].params as { keys: string[]; until: string };
    expect(p.keys).toEqual(["verdict:7"]);
    const ms = new Date(p.until).getTime() - Date.now();
    expect(ms).toBeGreaterThan(0);
    expect(ms).toBeLessThan(24 * 3_600_000);
    await waitFor(() => expect(screen.queryByTestId("inbox-row-verdict:7")).toBeNull());

    // e resolves the stall row
    await key(screen.getByTestId("inbox-row-stall:8:1000"), "e");
    expect(d.callsOf("inbox.resolve")[0].params).toEqual({ keys: ["stall:8:1000"] });
  });

  it("Keyboard: a key typed inside a control of the row does not act on the row", async () => {
    const d = daemonWith(verdictItem());
    await renderInbox(d);
    await key(screen.getByTestId("inbox-open-verdict:7"), "e");
    expect(d.callsOf("inbox.resolve")).toHaveLength(0);
  });

  it("OpenTask: Enter and the button open the task and mark the row seen", async () => {
    const d = daemonWith(verdictItem());
    const onOpenTask = vi.fn();
    await renderInbox(d, { onOpenTask });
    await key(screen.getByTestId("inbox-row-verdict:7"), "Enter");
    expect(onOpenTask).toHaveBeenCalledWith(6);
    expect(d.callsOf("inbox.seen")[0].params).toEqual({ keys: ["verdict:7"] });
  });

  it("MarkSeen: the row loses its New marker once seen", async () => {
    const d = daemonWith(verdictItem());
    await renderInbox(d);
    expect(screen.getByTestId("inbox-new-verdict:7")).toBeInTheDocument();
    await click(screen.getByTestId("inbox-seen-verdict:7"));
    await waitFor(() => expect(screen.queryByTestId("inbox-new-verdict:7")).toBeNull());
  });

  it("SnoozeMenu: a choice sends one snooze capped under 24 hours", async () => {
    const d = daemonWith(stallItem({ state: "new" }));
    await renderInbox(d);
    fireEvent.pointerDown(screen.getByTestId("inbox-snooze-stall:8:1000"), { button: 0, ctrlKey: false });
    await flush();
    await click(await screen.findByTestId("inbox-snooze-tomorrow-stall:8:1000"));
    const until = (d.callsOf("inbox.snooze")[0].params as { until: string }).until;
    expect(new Date(until).getTime() - Date.now()).toBeLessThan(24 * 3_600_000);
  });

  it("PermissionCard: Review request shows the task page's card, and answering uses run.respondPermission", async () => {
    const d = daemonWith(permissionItem());
    await renderInbox(d);
    await click(screen.getByTestId("inbox-review-perm:q1"));
    const card = await screen.findByTestId("permission-card");
    expect(within(card).getByText("Allow")).toBeInTheDocument();
    expect(within(card).getByText("Deny")).toBeInTheDocument();
    expect(d.callsOf("run.logs")[0].params).toEqual({ runId: "run-1" });
    await click(within(card).getByText("Allow"));
    expect(d.callsOf("run.respondPermission")[0].params).toEqual({ runId: "run-1", requestId: "q1", optionId: "allow-1" });
    await waitFor(() => expect(screen.queryByTestId("inbox-row-perm:q1")).toBeNull());
  });

  it("PermissionCard: an already answered request says so", async () => {
    const d = daemonWith(permissionItem());
    d.logs["run-1"] = [...REQUEST_EVENTS, { type: "permission_resolved", requestId: "q1", optionId: "allow-1" }];
    await renderInbox(d);
    await click(screen.getByTestId("inbox-review-perm:q1"));
    expect(await screen.findByTestId("inbox-request-gone")).toBeInTheDocument();
  });

  it("LiveRegion: polite, and it speaks only when the count changes after the first load", async () => {
    const d = daemonWith(verdictItem());
    await renderInbox(d);
    const live = screen.getByTestId("inbox-live");
    expect(live).toHaveAttribute("aria-live", "polite");
    expect(live).toHaveAttribute("role", "status");
    expect(live).toHaveTextContent("");
    d.items = [...d.items, stallItem({ state: "new" })];
    await act(async () => d.emit("inbox.changed", {}));
    await flush();
    await waitFor(() => expect(live).toHaveTextContent("2 items need you"));
  });

  it("InboxChanged: an event refetches the list and the count", async () => {
    const d = daemonWith(verdictItem());
    await renderInbox(d);
    const before = d.callsOf("inbox.list").length;
    d.items = [verdictItem(), permissionItem()];
    await act(async () => d.emit("inbox.changed", {}));
    await flush();
    expect(d.callsOf("inbox.list").length).toBe(before + 1);
    expect(await screen.findByTestId("inbox-row-perm:q1")).toBeInTheDocument();
  });

  it("Escape goes back", async () => {
    const onNavigateBack = vi.fn();
    await renderInbox(daemonWith(), { onNavigateBack });
    fireEvent.keyDown(document, { key: "Escape" });
    expect(onNavigateBack).toHaveBeenCalled();
  });
});

describe("InboxBadge", () => {
  it("BadgeCountFollowsInboxChanged: shows the unseen count, updates on the event and disappears at zero", async () => {
    const d = daemonWith(permissionItem(), verdictItem());
    renderWithDaemon(<InboxBadge />, d);
    await flush();
    expect(screen.getByTestId("inbox-badge")).toHaveTextContent("2");
    expect(screen.getByTestId("inbox-badge")).toHaveAttribute("aria-label", "2 unseen inbox items");
    d.items = [verdictItem({ state: "seen" })];
    await act(async () => d.emit("inbox.changed", {}));
    await flush();
    await waitFor(() => expect(screen.queryByTestId("inbox-badge")).toBeNull());
  });

  it("a seen permission still counts", async () => {
    const d = daemonWith(permissionItem({ state: "seen" }));
    renderWithDaemon(<InboxBadge />, d);
    await flush();
    expect(screen.getByTestId("inbox-badge")).toHaveTextContent("1");
  });
});

describe("InboxScreen automation chip", () => {
  it("marks a row whose task an automation started, from the cached tree, without a fetch", async () => {
    const queryClient = createQueryClient();
    const task = (ID: number, extra: object = {}) => ({ ID, WorkspaceID: 1, SpaceID: null, Title: "t", Status: "active", WorktreePath: null, Branch: null, CreatedAt: "", UpdatedAt: "", ArchivedAt: null, ...extra });
    queryClient.setQueryData(queryKeys.workspaceTree(), [
      { ID: 1, spaces: [], ungroupedTasks: [task(8, { automationId: 2, automationName: "Nightly" })] },
    ] as unknown as WorkspaceWithTree[]);
    const d = daemonWith(stallItem(), permissionItem());
    const view = renderWithDaemon(<InboxScreen onNavigateBack={() => {}} onOpenTask={() => {}} />, d, { queryClient });
    await flush();
    expect(within(view.getByTestId("inbox-row-stall:8:1000")).getByTestId("automation-chip")).toBeInTheDocument();
    expect(within(view.getByTestId("inbox-row-perm:q1")).queryByTestId("automation-chip")).not.toBeInTheDocument();
  });
});
