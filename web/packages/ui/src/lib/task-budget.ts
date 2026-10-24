import type { Task } from "@/lib/types";

/** ADR-0041 A5: a budget at or past 80% of its cap reads as "near". */
export const NEAR_BUDGET_RATIO = 0.8;

export type BudgetKind = "none" | "under" | "near" | "over";

export interface BudgetState {
  kind: BudgetKind;
  /** spent / cap, 0 when there is no cap; may exceed 1. */
  ratio: number;
  /** The chip text: "Set budget", "$4.20 / $10.00" or "Over budget". */
  label: string;
  /** "$4.20+ · partial: 3 runs reported no cost" when some runs reported no cost, else "". */
  partialLabel: string;
}

type BudgetFields = Pick<Task, "budgetUsd" | "budgetSpentUsd" | "budgetPartial" | "budgetUnknownRuns" | "overBudget">;

export function formatUsd(n: number): string {
  return `$${n.toFixed(2)}`;
}

const cents = (n: number): number => Math.round(n * 100);

/** The chip/popover state of a root task's budget; "none" when it has no cap. */
export function budgetState(task: BudgetFields): BudgetState {
  const cap = task.budgetUsd;
  if (cap == null || !(cap > 0)) return { kind: "none", ratio: 0, label: "Set budget", partialLabel: "" };
  const spent = task.budgetSpentUsd ?? 0;
  const unknown = task.budgetUnknownRuns ?? 0;
  const partial = unknown > 0 || task.budgetPartial === true;
  const ratio = spent / cap;
  const over = cents(spent) >= cents(cap) || task.overBudget === true;
  const kind: BudgetKind = over ? "over" : ratio >= NEAR_BUDGET_RATIO ? "near" : "under";
  const partialLabel = partial
    ? `${formatUsd(spent)}+ · partial: ${unknown > 0 ? `${unknown} ${unknown === 1 ? "run" : "runs"} reported no cost` : "some runs reported no cost"}`
    : "";
  return {
    kind,
    ratio,
    label: kind === "over" ? "Over budget" : `${formatUsd(spent)}${partial ? "+" : ""} / ${formatUsd(cap)}`,
    partialLabel,
  };
}

export type BudgetInput = { ok: true; value: number } | { ok: false; error: string };

/** Validates the popover's text: a positive amount with at most 2 decimals (a leading "$" is tolerated). */
export function parseBudgetInput(text: string): BudgetInput {
  const t = text.trim().replace(/^\$/, "").trim();
  if (t === "") return { ok: false, error: "Enter an amount in USD." };
  if (t.startsWith("-")) return { ok: false, error: "The budget must be greater than 0." };
  if (/^\d*\.\d{3,}$/.test(t)) return { ok: false, error: "Use at most 2 decimal places." };
  if (!/^(\d+\.?\d{0,2}|\.\d{1,2})$/.test(t)) return { ok: false, error: "Enter a number like 5 or 5.00." };
  const value = Number(t);
  if (!Number.isFinite(value) || value <= 0) return { ok: false, error: "The budget must be greater than 0." };
  return { ok: true, value };
}
