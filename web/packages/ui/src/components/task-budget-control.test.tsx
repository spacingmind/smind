// ADR-0041 A5 / AC12: the root task header's budget chip + popover.
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { TaskBudgetControl } from "@/components/task-budget-control";
import { FakeWsClient } from "@/test/fake-ws-client";
import { renderWithClientProp as render } from "@/test/render-with-daemon";
import type { Task } from "@/lib/types";
import type { WsClient } from "@/lib/ws-client";

const ROOT: Task = {
  ID: 7,
  WorkspaceID: 1,
  SpaceID: null,
  Title: "Root",
  Status: "active",
  WorktreePath: null,
  Branch: null,
  CreatedAt: "2024-01-01T00:00:00Z",
  UpdatedAt: "2024-01-01T00:00:00Z",
  ArchivedAt: null,
};

function setup(task: Task) {
  const client = new FakeWsClient();
  render(<TaskBudgetControl task={task} client={client as unknown as WsClient} />);
  return client;
}

const chip = () => screen.getByTestId("task-budget-chip");
const open = () => fireEvent.click(chip());

describe("TaskBudgetControl", () => {
  it("none: muted 'Set budget'", () => {
    setup(ROOT);
    expect(chip()).toHaveTextContent("Set budget");
    expect(chip()).toHaveAttribute("data-state-kind", "none");
  });

  it("under: spent / cap with a meter", () => {
    setup({ ...ROOT, budgetUsd: 10, budgetSpentUsd: 4.2 });
    expect(chip()).toHaveTextContent("$4.20 / $10.00");
    expect(chip()).toHaveAttribute("data-state-kind", "under");
    expect(screen.getByTestId("task-budget-meter")).toHaveAttribute("aria-valuenow", "42");
  });

  it("task-detail tree budget chip still renders when a budget is set", () => {
    setup({ ...ROOT, budgetUsd: 10, budgetSpentUsd: 4.2 });
    expect(chip()).toHaveTextContent("$4.20 / $10.00");
  });

  it("near: 80% and up", () => {
    setup({ ...ROOT, budgetUsd: 10, budgetSpentUsd: 8 });
    expect(chip()).toHaveAttribute("data-state-kind", "near");
  });

  it("over: reads 'Over budget'", () => {
    setup({ ...ROOT, budgetUsd: 10, budgetSpentUsd: 10, overBudget: true });
    expect(chip()).toHaveTextContent("Over budget");
    expect(chip()).toHaveAttribute("data-state-kind", "over");
  });

  it("partial: the chip says partial and the popover names the runs and the reported-cost bound", () => {
    setup({ ...ROOT, budgetUsd: 10, budgetSpentUsd: 4.2, budgetPartial: true, budgetUnknownRuns: 3 });
    expect(screen.getByTestId("task-budget-chip-partial")).toHaveTextContent("partial");
    open();
    expect(screen.getByTestId("task-budget-partial")).toHaveTextContent("$4.20+ · partial: 3 runs reported no cost");
    expect(screen.getByTestId("task-budget-popover")).toHaveTextContent("bounds reported cost only");
  });

  it("is hidden for a task with a parent", () => {
    setup({ ...ROOT, ParentTaskID: 3, budgetUsd: 10 });
    expect(screen.queryByTestId("task-budget-chip")).not.toBeInTheDocument();
  });

  it("a bad amount shows an inline error and disables Save", () => {
    setup(ROOT);
    open();
    expect(screen.getByTestId("task-budget-save")).toBeDisabled();
    fireEvent.change(screen.getByTestId("task-budget-input"), { target: { value: "1.234" } });
    expect(screen.getByTestId("task-budget-error")).toHaveTextContent(/2 decimal/);
    expect(screen.getByTestId("task-budget-save")).toBeDisabled();
    expect(screen.getByTestId("task-budget-input")).toHaveAttribute("inputmode", "decimal");
    fireEvent.change(screen.getByTestId("task-budget-input"), { target: { value: "1.23" } });
    expect(screen.queryByTestId("task-budget-error")).not.toBeInTheDocument();
    expect(screen.getByTestId("task-budget-save")).toBeEnabled();
  });

  it("Save calls task.setBudget with the parsed number and updates the chip", async () => {
    const client = setup(ROOT);
    open();
    fireEvent.change(screen.getByTestId("task-budget-input"), { target: { value: "12.5" } });
    fireEvent.click(screen.getByTestId("task-budget-save"));
    await waitFor(() => client.nth("task.setBudget"));
    const call = client.nth("task.setBudget");
    expect(call.params).toEqual({ taskId: 7, budgetUsd: 12.5 });
    call.resolve({ ...ROOT, budgetUsd: 12.5 });
    await waitFor(() => expect(chip()).toHaveTextContent("$0.00 / $12.50"));
    expect(screen.queryByTestId("task-budget-popover")).not.toBeInTheDocument();
  });

  it("Clear calls task.setBudget with null", async () => {
    const client = setup({ ...ROOT, budgetUsd: 10, budgetSpentUsd: 1 });
    open();
    expect(screen.getByTestId("task-budget-input")).toHaveValue("10.00");
    fireEvent.click(screen.getByTestId("task-budget-clear"));
    await waitFor(() => client.nth("task.setBudget"));
    const call = client.nth("task.setBudget");
    expect(call.params).toEqual({ taskId: 7, budgetUsd: null });
    call.resolve({ ...ROOT });
    await waitFor(() => expect(chip()).toHaveTextContent("Set budget"));
  });

  it("a daemon refusal shows inline and keeps the popover open", async () => {
    const client = setup(ROOT);
    open();
    fireEvent.change(screen.getByTestId("task-budget-input"), { target: { value: "5" } });
    fireEvent.click(screen.getByTestId("task-budget-save"));
    await waitFor(() => client.nth("task.setBudget"));
    client.nth("task.setBudget").reject(new Error("task 7 is not a root task"));
    expect(await screen.findByTestId("task-budget-server-error")).toHaveTextContent("not a root task");
    expect(screen.getByTestId("task-budget-popover")).toBeInTheDocument();
  });
});
