import { act } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { AppSidebar } from "@/components/app-sidebar";
import { SidebarProvider } from "@/components/ui/sidebar";
import { FakeWsClient } from "@/test/fake-ws-client";
import type { AttentionReason } from "@/hooks/use-task-attention";
import type { Task, Workspace } from "@/lib/types";

// TaskRow calls useRowActive exactly once per render, so counting its calls
// counts TaskRow renders (its only other callers, a workspace row and the
// space rows, are a handful) without any seam in production code.
const rowRenders = vi.hoisted(() => ({ n: 0 }));
vi.mock("@/hooks/use-row-active", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/hooks/use-row-active")>();
  return {
    ...actual,
    useRowActive: () => {
      rowRenders.n += 1;
      return actual.useRowActive();
    },
  };
});

const WORKSPACE: Workspace = { ID: 1, Path: "/tmp/ws", Title: "W", RoutingPolicy: "", CreatedAt: "2024-01-01T00:00:00Z", UpdatedAt: "2024-01-01T00:00:00Z" };

function task(id: number, parent: number | null): Task {
  return {
    ID: id, WorkspaceID: 1, SpaceID: null, Title: `Task ${id}`, Status: "active", WorktreePath: null, Branch: null,
    CreatedAt: "2024-01-01T00:00:00Z", UpdatedAt: "2024-01-01T00:00:00Z", ArchivedAt: null, ...(parent ? { ParentTaskID: parent } : {}),
  };
}

async function flush() {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

// task-hierarchy-tiers PR-U1's memo guard (U1-AC3): TaskRow is memoized so a
// re-render of AppSidebar that changes nothing a row reads leaves every row
// alone. It was silently defeated for a while: the archive handler was an
// inline arrow, a new prop on each render, so opening any dialog re-rendered
// all of a 300-task tree. The tree below has a parent with children, so it
// also covers PR-H11's TreeActionsContext (read by every row, and once built
// from a fresh object on each render).
describe("AppSidebar memo guard (PR-U1)", () => {
  it("a 300-task wide tree renders without re-rendering unrelated rows", async () => {
    const tasks = [task(1, null), ...Array.from({ length: 299 }, (_, i) => task(i + 2, 1))];
    const client = new FakeWsClient();
    const tree = (attention?: Map<number, Set<AttentionReason>>) => (
      <SidebarProvider>
        <AppSidebar client={client as never} attention={attention} />
      </SidebarProvider>
    );
    const { rerender } = render(tree());
    client.nth("workspace.list", 0).resolve([WORKSPACE]);
    await flush();
    client.nth("space.list", 0).resolve([]);
    client.nth("task.list", 0).resolve(tasks);
    await flush();
    expect(await screen.findAllByTestId("sidebar-task-row")).toHaveLength(300);
    // The counter is live: mounting rendered every row at least once.
    expect(rowRenders.n).toBeGreaterThanOrEqual(300);

    // Opening the "New workspace" dialog changes AppSidebar's own state and
    // nothing a task row reads: no task row may re-render for it.
    const beforeDialog = rowRenders.n;
    fireEvent.click(screen.getByTestId("sidebar-new-workspace-button"));
    await flush();
    expect(rowRenders.n - beforeDialog).toBeLessThan(20); // only the handful of workspace/space rows, never 300

    // Control: a change a row does read (one task's attention) does re-render
    // rows, so the counter above would have noticed a leak.
    const beforeSignal = rowRenders.n;
    rerender(tree(new Map([[5, new Set<AttentionReason>(["permission"])]])));
    await flush();
    expect(rowRenders.n - beforeSignal).toBeGreaterThan(0);
  });
});
