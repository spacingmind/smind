import { UsageMeter } from "@/components/account-usage/usage-meter";
import { budgetState, type BudgetLevel } from "@/lib/learning-budget";
import type { LearningWorkspaceBudget } from "@/lib/learning-types";
import type { UsageTone } from "@/lib/account-usage/types";

const TONE: Record<BudgetLevel, UsageTone> = { ok: "default", warn: "warning", exhausted: "warning" };

function Meter({ label, value, fraction, tone, testId }: { label: string; value: string; fraction: number; tone: UsageTone; testId: string }) {
  return (
    <div className="flex flex-col gap-1">
      <div className="flex items-center justify-between gap-2 text-ui-sm">
        <span className="text-muted-foreground">{label}</span>
        <span data-testid={testId} className="font-medium tabular-nums text-foreground">
          {value}
        </span>
      </div>
      <div role="progressbar" aria-label={label} aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round(fraction * 100)}>
        <UsageMeter percent={fraction * 100} tone={tone} />
      </div>
    </div>
  );
}

/**
 * What reflection has spent in the daemon's rolling window against its daily caps (ADR-0026): a
 * cost bar and a token bar on the shared usage meter. At 80% either bar turns to the warning
 * tone; at a cap the daemon starts no new reflection job, and the row says so, calmly.
 */
export function LearningBudgetRow({ budget }: { budget: LearningWorkspaceBudget }) {
  const state = budgetState(budget);
  const tone = TONE[state.level];
  return (
    <div data-testid="learning-budget" data-level={state.level} className="flex flex-col gap-2">
      <h3 className="text-ui-base font-medium text-foreground">Learning budget (last {budget.windowHours} h)</h3>
      <div className="grid gap-3 sm:grid-cols-2">
        <Meter label="Cost" value={state.costLabel} fraction={state.costFraction} tone={tone} testId="learning-budget-cost" />
        <Meter label="Tokens" value={state.tokensLabel} fraction={state.tokenFraction} tone={tone} testId="learning-budget-tokens" />
      </div>
      {state.level === "exhausted" && (
        <p data-testid="learning-budget-paused" className="text-ui-sm text-muted-foreground">
          Reflection is paused until usage drops below the daily limit; nothing is lost, windows are kept for the next run.
        </p>
      )}
      {budget.unreportedRuns > 0 && (
        <p data-testid="learning-budget-note" className="text-ui-sm text-foreground-subtle">
          {budget.unreportedRuns} {budget.unreportedRuns === 1 ? "run" : "runs"} without reported usage {budget.unreportedRuns === 1 ? "is" : "are"} estimated
        </p>
      )}
    </div>
  );
}
