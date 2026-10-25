import { useEffect, useId, useState } from "react";
import { useQuery } from "@tanstack/react-query";

import { Button } from "@/components/ui/button";
import { FormError } from "@/components/ui/form-error";
import { Input } from "@/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { useSetTaskBudget } from "@/hooks/use-set-task-budget";
import { Icons } from "@/lib/icons";
import { queryKeys } from "@/lib/query-keys";
import { budgetState, formatUsd, parseBudgetInput, type BudgetKind } from "@/lib/task-budget";
import type { Task } from "@/lib/types";
import type { WsClient } from "@/lib/ws-client";
import { cn } from "@/lib/utils";

const METER_FILL: Record<BudgetKind, string> = {
  none: "bg-transparent",
  under: "bg-foreground-muted",
  near: "bg-warning",
  over: "bg-warning",
};

/** The thin spent/cap meter; full width at or past the cap. */
function BudgetMeter({ kind, ratio, className }: { kind: BudgetKind; ratio: number; className?: string }) {
  const pct = Math.min(100, Math.max(0, ratio * 100));
  return (
    <div
      role="meter"
      aria-label="Budget used"
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={Math.round(pct)}
      data-testid="task-budget-meter"
      className={cn("h-1 overflow-hidden rounded-full bg-border", className)}
    >
      <div className={cn("h-full rounded-full", METER_FILL[kind])} style={{ width: `${pct}%` }} />
    </div>
  );
}

/**
 * The root task header's "Budget" chip (ADR-0041 A5): the tree's reported spend against the opt-in USD cap, and a
 * popover to set or clear it. A human-only control (agents have no setter); renders nothing for a task with a parent,
 * since the cap lives on the tree root. The spend comes from a shared `task.get` entry that refreshes when a run ends.
 */
export function TaskBudgetControl({ task, client }: { task: Task; client: WsClient | null }) {
  const isRoot = !task.ParentTaskID;
  const { data } = useQuery<Task>({
    queryKey: queryKeys.taskBudget(task.ID),
    queryFn: ({ signal }) => client!.call<Task>("task.get", { id: task.ID }, { signal }),
    enabled: isRoot && client !== null,
    placeholderData: task,
  });
  const [open, setOpen] = useState(false);
  const [text, setText] = useState("");
  const [serverError, setServerError] = useState<string | null>(null);
  // The popover body as it was when a save/clear was submitted: the saved figures land before the popover finishes
  // closing, and re-rendering them mid fade-out makes its content jump.
  const [frozen, setFrozen] = useState<{ current: Task; state: ReturnType<typeof budgetState> } | null>(null);
  const setBudget = useSetTaskBudget();
  const inputId = useId();

  const current = data && data.ID === task.ID ? data : task;
  const state = budgetState(current);
  const parsed = parseBudgetInput(text);
  const showInvalid = text.trim() !== "" && !parsed.ok;

  // Each open starts from the stored cap and a fresh spend figure.
  useEffect(() => {
    if (!open) return;
    setText(current.budgetUsd != null ? current.budgetUsd.toFixed(2) : "");
    setServerError(null);
    setFrozen(null);
    // eslint-disable-next-line react-hooks/exhaustive-deps -- seed once per open, not on every spend refresh
  }, [open]);

  if (!isRoot) return null;

  const submit = (budgetUsd: number | null) => {
    setServerError(null);
    setFrozen({ current, state });
    setBudget.mutate(
      { taskId: task.ID, budgetUsd },
      {
        onSuccess: () => setOpen(false),
        onError: (err) => {
          setFrozen(null);
          setServerError(err.message || "Couldn't update the budget");
        },
      },
    );
  };

  const hasCap = state.kind !== "none";
  const shown = frozen ?? { current, state };
  const shownHasCap = shown.state.kind !== "none";
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        type="button"
        data-testid="task-budget-chip"
        data-state-kind={state.kind}
        aria-label={hasCap ? `Budget: ${state.label}` : "Set budget"}
        className={cn(
          "flex min-w-0 shrink items-center gap-1.5 rounded-full border px-2 py-0.5 text-ui-sm transition-colors hover:bg-hover hover:text-foreground",
          state.kind === "none" ? "border-dashed text-foreground-subtle" : "text-foreground-muted",
          state.kind === "near" && "text-foreground",
          state.kind === "over" && "border-warning/50 text-warning",
        )}
      >
        <Icons.usage aria-hidden className="size-3 shrink-0" />
        <span className="truncate">{state.label}</span>
        {hasCap && state.kind !== "over" && <BudgetMeter kind={state.kind} ratio={state.ratio} className="hidden w-8 shrink-0 sm:block" />}
        {state.partialLabel && (
          <span data-testid="task-budget-chip-partial" className="hidden shrink-0 rounded border px-1 text-ui-xs sm:inline">
            partial
          </span>
        )}
      </PopoverTrigger>
      <PopoverContent data-testid="task-budget-popover" align="start" className="flex w-72 max-w-[calc(100vw-2rem)] flex-col gap-2.5 p-3">
        <p className="text-ui-sm font-medium text-foreground">Tree budget</p>
        {shownHasCap ? (
          <div className="flex flex-col gap-1.5">
            <p data-testid="task-budget-summary" className="text-ui-base text-foreground">
              {formatUsd(shown.current.budgetSpentUsd ?? 0)}
              {shown.state.partialLabel ? "+" : ""} <span className="text-foreground-muted">of {formatUsd(shown.current.budgetUsd!)}</span>
              {shown.state.kind === "over" && <span className="ml-2 text-ui-sm text-warning">Over budget</span>}
            </p>
            <BudgetMeter kind={shown.state.kind} ratio={shown.state.ratio} />
            {shown.state.partialLabel && (
              <p data-testid="task-budget-partial" className="flex items-start gap-1.5 text-ui-sm text-foreground-muted">
                <span className="mt-px shrink-0 rounded border px-1 text-ui-xs">partial</span>
                <span>{shown.state.partialLabel}</span>
              </p>
            )}
          </div>
        ) : (
          <p className="text-ui-sm text-foreground-muted">No cap on this task and its sub-tasks.</p>
        )}
        <p className="text-ui-sm text-foreground-muted">
          At the cap, agents can't start new runs or tasks in this tree; you always can, and running runs aren't stopped. The cap bounds
          reported cost only: runs that report no cost aren't counted.
        </p>
        <form
          className="flex flex-col gap-1.5"
          onSubmit={(e) => {
            e.preventDefault();
            if (parsed.ok) submit(parsed.value);
          }}
        >
          <label htmlFor={inputId} className="text-ui-sm font-medium text-foreground">
            Cap (USD)
          </label>
          <Input
            id={inputId}
            data-testid="task-budget-input"
            inputMode="decimal"
            autoComplete="off"
            placeholder="e.g. 10.00"
            value={text}
            aria-invalid={showInvalid || undefined}
            aria-describedby={showInvalid ? `${inputId}-error` : undefined}
            onChange={(e) => setText(e.target.value)}
          />
          <FormError
            message={showInvalid && !parsed.ok ? parsed.error : null}
            testId="task-budget-error"
            size="sm"
            silent
          />
          <FormError message={serverError} testId="task-budget-server-error" size="sm" />
          <div className="flex items-center justify-end gap-2 pt-0.5">
            {shownHasCap && (
              <Button type="button" variant="ghost" size="sm" data-testid="task-budget-clear" disabled={setBudget.isPending} onClick={() => submit(null)}>
                Clear
              </Button>
            )}
            <Button type="submit" size="sm" data-testid="task-budget-save" disabled={!parsed.ok || setBudget.isPending}>
              Save
            </Button>
          </div>
        </form>
      </PopoverContent>
    </Popover>
  );
}
