import { useState } from "react";

import { useLastDefined } from "@/hooks/use-last-defined";

import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { EmptyState } from "@/components/ui/empty-state";
import { LoadingRows } from "@/components/ui/loading-rows";
import { StatusBadge, type StatusBadgeStatus } from "@/components/ui/status-badge";
import { useAutomationActions, useAutomationAudit, useAutomationMemory } from "@/hooks/use-automations";
import { useConfirmAction } from "@/hooks/use-confirm-action";
import { useRunLookup } from "@/hooks/use-learning";
import { formatUtc } from "@/lib/automation-format";
import type { AutomationAuditRow, AutomationView } from "@/lib/automation-types";
import { formatRoute } from "@/lib/route";

const DECISIONS = ["fire", "deduped", "capped", "paused", "missed", "failed"] as const;

/** fire is the one success; a held or recorded-only decision is a warning; failed is a danger; deduped / missed are quiet. */
const DECISION_STATUS: Record<string, StatusBadgeStatus> = {
  fire: "success",
  failed: "danger",
  capped: "warning",
  paused: "warning",
  deduped: "neutral",
  missed: "neutral",
};

const SELECT_CLASS =
  "h-8 rounded-lg border border-input-border bg-input px-2 text-ui-base outline-none hover:border-input-border-hover focus-visible:border-input-border-focused focus-visible:bg-input-focused";

/** A task an automation started: opens it through the app's hash route (Settings covers the shell, so it closes first). */
function TaskLink({ row, workspaceId, closeSettings }: { row: AutomationAuditRow; workspaceId: number; closeSettings?: () => void }) {
  const lookup = useRunLookup();
  if (row.taskId === null) return null;
  const run = row.runId ? lookup(row.runId) : undefined;
  const href = formatRoute({ workspaceId, taskId: row.taskId, tab: run ? { kind: "chat", chatId: run.ChatID } : { kind: "files" } });
  return (
    <a
      data-testid={`automation-audit-task-${row.id}`}
      href={href}
      className="text-ui-sm text-foreground underline underline-offset-2 hover:text-foreground-muted"
      onClick={(e) => {
        e.preventDefault();
        window.location.hash = href;
        closeSettings?.();
      }}
    >
      Task #{row.taskId}
    </a>
  );
}

function AuditRow({ row, workspaceId, closeSettings }: { row: AutomationAuditRow; workspaceId: number; closeSettings?: () => void }) {
  return (
    <li
      data-testid={`automation-audit-${row.id}`}
      data-decision={row.decision}
      className="flex flex-col gap-1 border-b py-2 last:border-b-0 md:grid md:grid-cols-[9.5rem_auto_1fr] md:items-start md:gap-x-3"
    >
      <span className="text-ui-sm text-muted-foreground" title={row.createdAt}>
        {formatUtc(row.createdAt)}
      </span>
      <span className="flex flex-wrap items-center gap-1">
        <StatusBadge status={DECISION_STATUS[row.decision] ?? "neutral"} data-testid={`automation-audit-decision-${row.id}`}>
          {row.decision}
        </StatusBadge>
        {row.manual && (
          <StatusBadge status="neutral" data-testid={`automation-audit-manual-${row.id}`}>
            manual
          </StatusBadge>
        )}
        {row.catchup && (
          <StatusBadge status="neutral" data-testid={`automation-audit-catchup-${row.id}`}>
            catch-up
          </StatusBadge>
        )}
      </span>
      <span className="flex min-w-0 flex-col gap-0.5">
        {row.reason && <span className="break-words text-ui-sm text-foreground-muted">{row.reason}</span>}
        {row.outcome && (
          <span className="break-words text-ui-sm text-foreground-muted" data-testid={`automation-audit-outcome-${row.id}`}>
            Outcome: {row.outcome}
            {row.note ? ` · ${row.note}` : ""}
          </span>
        )}
        <TaskLink row={row} workspaceId={workspaceId} closeSettings={closeSettings} />
      </span>
    </li>
  );
}

function MemoryPanel({ automation, canManage, onForbidden }: { automation: AutomationView; canManage: boolean; onForbidden: (err: unknown) => void }) {
  const memory = useAutomationMemory(automation.id, true);
  const actions = useAutomationActions();
  const [confirming, setConfirming] = useState(false);
  const clear = useConfirmAction(confirming);
  const text = memory.data ?? "";
  return (
    <section className="flex flex-col gap-1.5 border-t pt-3" data-testid="automation-memory-panel">
      <div className="flex items-center justify-between gap-2">
        <h4 className="text-ui-sm font-medium text-foreground">Memory</h4>
        <Button
          type="button"
          variant="outline"
          size="sm"
          data-testid="automation-memory-clear"
          disabled={!canManage || text === ""}
          onClick={() => {
            clear.reset();
            setConfirming(true);
          }}
        >
          Clear
        </Button>
      </div>
      {memory.isError ? (
        <Alert variant="error" description={memory.error instanceof Error ? memory.error.message : String(memory.error)} />
      ) : (
        <pre
          data-testid="automation-memory-text"
          className="max-h-48 overflow-auto whitespace-pre-wrap break-words rounded-lg border bg-card p-2 font-mono text-ui-sm text-foreground-muted"
        >
          {memory.isPending ? "Loading…" : text === "" ? "Empty. The agent writes notes here for its next run." : text}
        </pre>
      )}
      <ConfirmDialog
        open={confirming}
        onOpenChange={(open) => {
          if (open) return;
          setConfirming(false);
          clear.reset();
        }}
        title="Clear this memory?"
        description="The agent loses what it noted for its next runs. This cannot be undone."
        confirmLabel="Clear memory"
        pendingLabel="Clearing…"
        pending={clear.pending}
        error={clear.error}
        testId="automation-memory-clear-dialog"
        onConfirm={() => {
          void clear
            .run(async () => {
              try {
                await actions.memoryClear(automation.id);
              } catch (err) {
                onForbidden(err);
                throw err;
              }
            })
            .then((done) => {
              if (done) setConfirming(false);
            });
        }}
      />
    </section>
  );
}

/**
 * The run history of one automation (ADR-0044 section 10): every signal the engine decided on, newest first, with the
 * decision, the reason, the outcome and the task it started; "Load older" pages with `before` = the last id; the
 * decision filter narrows the loaded rows. The memory panel shows only when memory is enabled.
 */
export function AutomationHistoryDialog({
  automation,
  canManage,
  onClose,
  onForbidden,
  closeSettings,
}: {
  automation: AutomationView | null;
  canManage: boolean;
  onClose: () => void;
  onForbidden: (err: unknown) => void;
  closeSettings?: () => void;
}) {
  // Cleared on close while the exit animation plays: keep showing the last automation so the dialog fades out whole.
  const shown = useLastDefined(automation);
  return (
    <Dialog open={automation !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent data-testid="automation-history-dialog" className="max-h-[90vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader className="text-left">
          <DialogTitle>{shown ? `History: ${shown.name}` : "History"}</DialogTitle>
          <DialogDescription>Every signal this automation decided on, newest first.</DialogDescription>
        </DialogHeader>
        {shown && <HistoryBody key={shown.id} automation={shown} canManage={canManage} onForbidden={onForbidden} closeSettings={closeSettings} />}
      </DialogContent>
    </Dialog>
  );
}

function HistoryBody({ automation, canManage, onForbidden, closeSettings }: { automation: AutomationView; canManage: boolean; onForbidden: (err: unknown) => void; closeSettings?: () => void }) {
  const [decision, setDecision] = useState("");
  const audit = useAutomationAudit(automation.id, { decision });
  const rows = audit.rows;
  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center gap-2">
        <label htmlFor="automation-audit-filter" className="text-ui-sm text-foreground-muted">
          Decision
        </label>
        <select id="automation-audit-filter" data-testid="automation-audit-filter" className={SELECT_CLASS} value={decision} onChange={(e) => setDecision(e.target.value)}>
          <option value="">All</option>
          {DECISIONS.map((d) => (
            <option key={d} value={d}>
              {d}
            </option>
          ))}
        </select>
        {audit.isFetching && !audit.isPending && <span className="text-ui-sm text-foreground-subtlest">Updating…</span>}
      </div>

      {rows === undefined ? (
        audit.isError ? (
          <Alert variant="error" testId="automation-audit-error" description={audit.error instanceof Error ? audit.error.message : String(audit.error)} />
        ) : (
          <LoadingRows rows={7} />
        )
      ) : rows.length === 0 ? (
        <EmptyState
          testId="automation-audit-empty"
          title={audit.loadedCount === 0 ? "No runs yet" : `No ${decision} rows loaded`}
          description={audit.loadedCount === 0 ? "Signals appear here once the automation is enabled and fires." : "Load older rows to look further back."}
        />
      ) : (
        <ul className="flex flex-col" data-testid="automation-audit-list">
          {rows.map((row) => (
            <AuditRow key={row.id} row={row} workspaceId={automation.workspaceId} closeSettings={closeSettings} />
          ))}
        </ul>
      )}
      {audit.hasNextPage && (
        <div>
          <Button type="button" variant="outline" size="sm" data-testid="automation-audit-more" disabled={audit.isFetchingNextPage} onClick={() => void audit.fetchNextPage()}>
            {audit.isFetchingNextPage ? "Loading…" : "Load older"}
          </Button>
        </div>
      )}
      {decision !== "" && audit.loadedCount > 0 && (
        <p className="text-ui-sm text-foreground-subtlest">
          Showing {rows?.length ?? 0} of {audit.loadedCount} loaded rows.
        </p>
      )}

      {automation.memoryEnabled && <MemoryPanel automation={automation} canManage={canManage} onForbidden={onForbidden} />}
    </div>
  );
}
