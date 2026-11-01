import { useEffect, useState } from "react";

import { AutomationHistoryDialog } from "@/components/settings/automation-history";
import { AutomationFormDialog } from "@/components/settings/automation-form";
import { SettingsEmptyCard } from "@/components/settings/settings-layout";
import { AutomationsOffCard } from "@/components/automations/automations-off-card";
import { DesktopWindowControls } from "@/components/desktop-window-controls";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { FormError } from "@/components/ui/form-error";
import { LoadingRows } from "@/components/ui/loading-rows";
import { StatusBadge } from "@/components/ui/status-badge";
import { useAutomationActions, useAutomations } from "@/hooks/use-automations";
import { useConfirmAction } from "@/hooks/use-confirm-action";
import { useLastDefined } from "@/hooks/use-last-defined";
import { useWindowState } from "@/hooks/use-window-state";
import { automationState, fireOutcomeText, formatLocal, formatUtc, signalSummary } from "@/lib/automation-format";
import type { AutomationAuditRow, AutomationView } from "@/lib/automation-types";
import { Icons } from "@/lib/icons";
import { desktopOS, isDesktop } from "@/lib/platform";
import { formatRelativeTime } from "@/lib/relative-time";
import { cn } from "@/lib/utils";
import { headerLeftInsetPx } from "@/lib/window-chrome";

/** Tailwind `p-2`, which an inline `paddingLeft` replaces (see InboxScreen). */
const BASE_PADDING_PX = 8;

/** The daemon's refusal of a human-local action (a relay session, or a remote login with automations.allowRemoteManage off). */
function isForbidden(err: unknown): boolean {
  return (err as { code?: string } | null)?.code === "forbidden";
}

function messageOf(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

/**
 * The Automations page (ADR-0044, AC20): rules that start a task from a signal (a cron schedule, a failed GitHub
 * workflow run). A full-screen view like Settings and the inbox, not a Settings section. The daemon switches
 * (`automations.enabled` / `paused`) are file edits only: this page shows their state and has no control for them.
 * Every mutation except Disable is human + local only and may answer `forbidden` (a relay session, or a remote login
 * with automations.allowRemoteManage off); the page then goes read-only with the daemon's message, and Disable stays
 * available (a brake, never an accelerator). Live through `automation.fired/paused/updated` (the event bridge
 * refetches the list).
 */
export function AutomationsPage({ onNavigateBack, closeSettings }: { onNavigateBack: () => void; closeSettings: () => void }) {
  const query = useAutomations();
  const actions = useAutomationActions();
  const listing = query.data;
  const automations = listing?.automations ?? null;
  const daemonOn = listing?.enabled ?? false;

  const [readOnlyReason, setReadOnlyReason] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [rowError, setRowError] = useState<{ id: number; message: string } | null>(null);
  const [fireResult, setFireResult] = useState<{ id: number; row: AutomationAuditRow } | null>(null);
  const [busy, setBusy] = useState<number | null>(null);
  /** Edit/create and history targets (the dialogs arrive with the form and the history view). */
  const [editing, setEditing] = useState<number | "new" | null>(null);
  const [historyId, setHistoryId] = useState<number | null>(null);
  const canManage = daemonOn && readOnlyReason === null;
  const lockedReason = readOnlyReason ?? (daemonOn ? null : "Automations are off");

  /** Notes a refusal (the page goes read-only with the daemon's own message) and returns the message. */
  function fail(err: unknown): string {
    if (isForbidden(err)) setReadOnlyReason(messageOf(err));
    return messageOf(err);
  }

  async function quick(a: AutomationView, run: () => Promise<unknown>) {
    setBusy(a.id);
    setRowError(null);
    setNotice(null);
    setFireResult(null);
    try {
      await run();
    } catch (err) {
      setRowError({ id: a.id, message: fail(err) });
    } finally {
      setBusy(null);
    }
  }

  const [deleteId, setDeleteId] = useState<number | null>(null);
  const deleteAction = useConfirmAction(deleteId !== null);
  const deleteTarget = useLastDefined(automations?.find((a) => a.id === deleteId) ?? null);

  const newButton = (
    <Button
      type="button"
      size="sm"
      data-testid="automation-new-button"
      disabled={!canManage}
      title={lockedReason ?? undefined}
      onClick={() => {
        setNotice(null);
        setEditing("new");
      }}
    >
      <Icons.add aria-hidden className="size-3.5" /> New automation
    </Button>
  );

  // Escape closes the screen, unless something on top (a menu, a dialog) already consumed it.
  useEffect(() => {
    function onKeyDown(e: globalThis.KeyboardEvent) {
      if (e.key !== "Escape" || e.defaultPrevented) return;
      const open = document.querySelectorAll('[role="dialog"],[role="alertdialog"],[role="menu"],[role="listbox"]');
      if (Array.from(open).some((el) => !el.closest("[inert]"))) return;
      onNavigateBack();
    }
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [onNavigateBack]);

  const { fullscreen } = useWindowState();
  const leftInset = isDesktop ? headerLeftInsetPx(desktopOS, fullscreen, 0) : 0;
  const headerStyle = leftInset > 0 ? { paddingLeft: BASE_PADDING_PX + leftInset } : undefined;

  return (
    <div className="flex h-full min-h-0 w-full flex-col bg-background" data-testid="automations-screen">
      <header
        className="flex h-12 shrink-0 items-center gap-2 border-b p-2"
        data-testid="automations-header"
        data-tauri-drag-region={isDesktop ? "deep" : undefined}
        style={headerStyle}
      >
        <Button variant="ghost" size="icon" aria-label="Back to app" data-testid="automations-back-button" onClick={onNavigateBack}>
          <Icons.back className="size-4" />
        </Button>
        <h1 className="truncate text-ui-base font-medium">Automations</h1>
        <div className="ml-auto">{newButton}</div>
        <DesktopWindowControls />
      </header>
      <main className="min-h-0 flex-1 overflow-y-auto" data-testid="automations-content">
        <div className="mx-auto flex w-full max-w-3xl flex-col gap-5 p-4">
          {listing && !listing.enabled && <AutomationsOffCard />}
          {listing?.enabled && listing.paused && (
            <Alert
              variant="warning"
              testId="automations-paused"
              title="Automations are paused"
              description={
                <>
                  Signals are recorded but nothing fires. Set <code className="font-mono text-ui-sm">automations.paused: false</code> in config.yaml on the computer to
                  resume.
                </>
              }
            />
          )}
          {readOnlyReason && (
            <Alert variant="info" testId="automations-read-only" title="Automations can't be managed from this connection" description={readOnlyReason} />
          )}
          {notice && <Alert variant="success" testId="automations-notice" description={notice} />}

          {(!listing || listing.enabled) &&
            (automations === null ? (
              query.isError ? (
                <div className="flex items-center gap-2">
                  <Alert variant="error" testId="automations-load-error" description={messageOf(query.error)} />
                  <Button type="button" variant="ghost" size="sm" data-testid="automations-load-retry" onClick={() => void query.refetch()}>
                    Retry
                  </Button>
                </div>
              ) : (
                <LoadingRows />
              )
            ) : automations.length === 0 ? (
              <SettingsEmptyCard
                testId="automations-empty-state"
                title="No automations"
                description="An automation starts a task when a schedule fires or a GitHub workflow fails. Create one, then enable it when you are ready."
              />
            ) : (
              <ul className="flex flex-col gap-1 rounded-xl border bg-card p-3" data-testid="automations-list">
                {automations.map((a) => (
                  <AutomationRow
                    key={a.id}
                    automation={a}
                    canManage={canManage}
                    lockedReason={lockedReason}
                    busy={busy === a.id}
                    error={rowError?.id === a.id ? rowError.message : null}
                    fireResult={fireResult?.id === a.id ? fireResult.row : null}
                    onToggle={() => void quick(a, () => (a.enabled ? actions.disable(a.id) : actions.enable(a.id)))}
                    onFire={() =>
                      void quick(a, async () => {
                        const row = await actions.fire(a.id);
                        setFireResult({ id: a.id, row });
                      })
                    }
                    onResume={() => void quick(a, () => actions.resume(a.id))}
                    onEdit={() => {
                      setNotice(null);
                      setEditing(a.id);
                    }}
                    onHistory={() => setHistoryId(a.id)}
                    onDelete={() => {
                      deleteAction.reset();
                      setDeleteId(a.id);
                    }}
                  />
                ))}
              </ul>
            ))}
        </div>
      </main>

      <AutomationFormDialog
        open={editing !== null}
        editing={typeof editing === "number" ? (automations?.find((a) => a.id === editing) ?? null) : null}
        onClose={() => setEditing(null)}
        onSaved={(message) => {
          setEditing(null);
          setNotice(message);
        }}
        onForbidden={fail}
      />

      <AutomationHistoryDialog
        automation={automations?.find((a) => a.id === historyId) ?? null}
        canManage={canManage}
        onClose={() => setHistoryId(null)}
        onForbidden={fail}
        closeSettings={closeSettings}
      />

      <ConfirmDialog
        open={deleteId !== null}
        onOpenChange={(open) => {
          if (open) return;
          setDeleteId(null);
          deleteAction.reset();
        }}
        title={`Delete ${deleteTarget?.name ?? ""}?`}
        description="Deletes the automation, its run history and its memory. Tasks it already started are kept."
        confirmLabel="Delete automation"
        pendingLabel="Deleting…"
        pending={deleteAction.pending}
        error={deleteAction.error}
        testId="automation-delete-dialog"
        onConfirm={() => {
          if (!deleteTarget) return;
          const id = deleteTarget.id;
          void deleteAction
            .run(async () => {
              try {
                await actions.delete(id);
              } catch (err) {
                fail(err);
                throw err;
              }
            })
            .then((done) => {
              if (done) setDeleteId(null);
            });
        }}
      />
    </div>
  );
}

function StateBadge({ automation: a }: { automation: AutomationView }) {
  const state = automationState(a);
  return (
    <StatusBadge
      status={state === "active" ? "success" : state === "paused" ? "warning" : "neutral"}
      data-testid={`automation-status-${a.id}`}
      data-state={state}
    >
      {state === "active" ? "Active" : state === "paused" ? "Paused" : "Disabled"}
    </StatusBadge>
  );
}

/** A compact on/off switch (role=switch), as the notifications section draws it. */
/** `busy`: a request is in flight: not clickable, but not dimmed (a dim-then-undim flash on every flip). */
function Switch({ checked, label, disabled, busy, title, testId, onClick }: { checked: boolean; label: string; disabled: boolean; busy?: boolean; title?: string; testId: string; onClick: () => void }) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      title={title}
      data-testid={testId}
      disabled={disabled}
      onClick={onClick}
      className={cn(
        "relative h-5 w-9 shrink-0 rounded-full border border-input-border transition-colors disabled:cursor-not-allowed",
        busy ? "disabled:opacity-100" : "disabled:opacity-50",
        checked ? "bg-primary" : "bg-transparent",
      )}
    >
      <span
        aria-hidden
        className={cn("absolute top-0.5 left-0.5 size-3.5 rounded-full transition-transform", checked ? "translate-x-4 bg-primary-foreground" : "translate-x-0 bg-foreground")}
      />
    </button>
  );
}

function AutomationRow({
  automation: a,
  canManage,
  lockedReason,
  busy,
  error,
  fireResult,
  onToggle,
  onFire,
  onResume,
  onEdit,
  onHistory,
  onDelete,
}: {
  automation: AutomationView;
  canManage: boolean;
  lockedReason: string | null;
  busy: boolean;
  error: string | null;
  fireResult: AutomationAuditRow | null;
  onToggle: () => void;
  onFire: () => void;
  onResume: () => void;
  onEdit: () => void;
  onHistory: () => void;
  onDelete: () => void;
}) {
  const state = automationState(a);
  const disabled = !canManage || busy;
  // Disable is a brake: it stays available on a relay session, where nothing else is.
  const switchDisabled = busy || (a.enabled ? false : !canManage);
  const isCron = a.signal.kind === "cron";
  return (
    <li data-testid={`automation-row-${a.id}`} data-state={state} className="flex flex-col gap-2 rounded-lg px-2 py-2 hover:bg-hover">
      <div className="flex items-start gap-3">
        <div className="flex min-w-0 flex-1 flex-col gap-0.5">
          <span className="flex flex-wrap items-center gap-x-2 gap-y-1">
            <span className="truncate text-ui-base font-medium text-foreground" data-testid={`automation-name-${a.id}`}>
              {a.name}
            </span>
            <StateBadge automation={a} />
          </span>
          <span className="break-words text-ui-sm text-muted-foreground" data-testid={`automation-signal-${a.id}`}>
            {signalSummary(a.signal)}
          </span>
          <span className="text-ui-sm text-muted-foreground" data-testid={`automation-times-${a.id}`}>
            {a.lastFiredAt ? `Last fired ${formatRelativeTime(a.lastFiredAt)}` : "Never fired"}
            {isCron && a.enabled && state === "active" && a.nextRunAt && ` · next run ${formatUtc(a.nextRunAt)} (${formatLocal(a.nextRunAt)} your time)`}
          </span>
          {a.budgetBlocked && (
            <span className="text-ui-sm text-warning-text" data-testid={`automation-capped-${a.id}`}>
              capped: last run reached its budget
            </span>
          )}
        </div>
        <Switch
          checked={a.enabled}
          label={`${a.enabled ? "Disable" : "Enable"} ${a.name}`}
          disabled={switchDisabled}
          busy={busy}
          title={a.enabled ? undefined : (lockedReason ?? undefined)}
          testId={`automation-switch-${a.id}`}
          onClick={onToggle}
        />
      </div>

      {state === "paused" && (
        <Alert variant="warning" testId={`automation-paused-${a.id}`} title="Paused until you resume it" description={a.pausedReason || "Paused."}>
          <Button
            type="button"
            variant="outline"
            size="sm"
            data-testid={`automation-resume-${a.id}`}
            disabled={disabled}
            title={lockedReason ?? undefined}
            onClick={onResume}
          >
            Resume
          </Button>
        </Alert>
      )}
      {error && <FormError message={error} size="sm" testId={`automation-row-error-${a.id}`} />}
      {fireResult && (
        <Alert
          variant={fireResult.decision === "fire" ? "success" : "info"}
          testId={`automation-fire-result-${a.id}`}
          description={fireOutcomeText(fireResult)}
        />
      )}

      <div className="flex flex-wrap items-center gap-1">
        <Button type="button" variant="outline" size="sm" data-testid={`automation-fire-${a.id}`} disabled={disabled} title={lockedReason ?? undefined} onClick={onFire}>
          Fire now
        </Button>
        <Button type="button" variant="ghost" size="sm" data-testid={`automation-history-${a.id}`} onClick={onHistory}>
          History
        </Button>
        <Button type="button" variant="ghost" size="sm" data-testid={`automation-edit-${a.id}`} disabled={disabled} title={lockedReason ?? undefined} onClick={onEdit}>
          Edit
        </Button>
        <Button type="button" variant="ghost" size="sm" data-testid={`automation-delete-${a.id}`} disabled={disabled} title={lockedReason ?? undefined} onClick={onDelete}>
          Delete
        </Button>
      </div>
    </li>
  );
}
