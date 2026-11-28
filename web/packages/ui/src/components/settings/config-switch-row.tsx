import { useState, type ReactNode } from "react";

import { SettingsRow } from "@/components/settings/settings-layout";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { Switch } from "@/components/ui/switch";
import { useConfigKey, useConfigSet } from "@/hooks/use-config";
import { CONFIG_CONFIRM, RESUME_CONFIRM, isWritableConfigKey, type WritableConfigKey } from "@/lib/config-keys";
import { cn } from "@/lib/utils";
import { daemonSentence } from "@/lib/daemon-sentence";
import { Icons } from "@/lib/icons";
import { isDesktop } from "@/lib/platform";

const RESTART_NOTE = "Takes effect after the daemon restarts.";

function messageOf(error: unknown): string {
  return daemonSentence(error instanceof Error ? error.message : String(error));
}

/**
 * "Open Daemon settings", styled as a link. The Daemon section (and its restart) exists only in the desktop
 * app, so the button renders there and nowhere else.
 */
export function OpenDaemonSettingsButton({ testId }: { testId: string }) {
  if (!isDesktop) return null;
  return (
    <Button
      type="button"
      variant="link"
      size="sm"
      data-testid={testId}
      onClick={() => window.dispatchEvent(new CustomEvent("smind:open-settings", { detail: { sectionId: "daemon" } }))}
    >
      Open Daemon settings
      <Icons.arrowRight aria-hidden />
    </Button>
  );
}

/**
 * One switch row for a global config key (ADR-0062; docs/plans/active/config-switches-ui.md). Everything shown
 * is the daemon's own answer: `writable` decides whether a switch exists, the value is always the re-read
 * one (no optimistic state), and every refusal is the daemon's sentence. Turning a key on, or resuming the
 * automations engine, asks first with the daemon's confirm sentence; turning off or pausing (the brake) does not.
 */
export function ConfigSwitchRow({
  configKey,
  label,
  description,
  testId,
  flush,
  noRestartNotes,
}: {
  configKey: WritableConfigKey;
  label: string;
  description: ReactNode;
  testId: string;
  /** No side padding: for a row placed inside a card that already pads its content. */
  flush?: boolean;
  /** The page already says what a restart changes (Hosts has its own state line): no restart line or link here. */
  noRestartNotes?: boolean;
}) {
  if (!isWritableConfigKey(configKey)) return null;
  return <Row configKey={configKey} label={label} description={description} testId={testId} flush={flush} noRestartNotes={noRestartNotes} />;
}

function Row({ configKey, label, description, testId, flush, noRestartNotes }: { configKey: WritableConfigKey; label: string; description: ReactNode; testId: string; flush?: boolean; noRestartNotes?: boolean }) {
  const px = flush ? "px-0" : undefined;
  const { view, isPending, error } = useConfigKey(configKey);
  const set = useConfigSet();
  const [confirming, setConfirming] = useState(false);
  const [saved, setSaved] = useState(false);

  const isPause = configKey === "automations.paused";
  const onWord = isPause ? "Paused" : "On";
  const offWord = isPause ? "Not paused" : "Off";

  if (isPending && !view) {
    return (
      <SettingsRow
        label={label}
        description={description}
        testId={testId}
        className={px}
        control={<div aria-hidden data-testid={`${testId}-loading`} className="h-5 w-9 animate-pulse rounded-full bg-muted" />}
      />
    );
  }
  if (error && !view) {
    return (
      <SettingsRow label={label} description={description} testId={testId} className={px}>
        <p data-testid={`${testId}-load-error`} className="text-ui-sm text-foreground-muted">
          {`Could not read this setting: ${messageOf(error)}`}
        </p>
      </SettingsRow>
    );
  }
  if (!view) return null;

  const on = view.value === "true";
  const busy = set.isPending;
  const envLocked = view.source === "env" && Boolean(view.env);
  // A change in the "risky" direction asks first: on, or resume. The brake (off, pause) goes straight through.
  const needsConfirm = (next: boolean) => (isPause ? !next : next);

  async function send(next: boolean): Promise<boolean> {
    setSaved(false);
    try {
      await set.mutateAsync({ key: configKey, value: next });
      setSaved(true);
      return true;
    } catch {
      return false; // the mutation's error is shown below; the query re-read puts the real value back
    }
  }

  function onToggle(next: boolean) {
    set.reset();
    if (needsConfirm(next)) {
      setConfirming(true);
      return;
    }
    void send(next);
  }

  const showRestart = view.restart && !noRestartNotes;
  const restartLine = showRestart ? (
    <p data-testid={`${testId}-restart`} className="mt-1 text-ui-sm text-foreground-muted">
      {saved ? `Saved. ${RESTART_NOTE}` : RESTART_NOTE}
    </p>
  ) : null;
  // The Daemon section (and its restart) exists only in the desktop app, so the button does too.
  const openDaemon = saved && showRestart ? <OpenDaemonSettingsButton testId={`${testId}-open-daemon`} /> : null;

  const notes = (
    <>
      {restartLine}
      {openDaemon}
    </>
  );

  if (!view.writable && !envLocked) {
    return (
      <div data-testid={`${testId}-wrap`}>
        <SettingsRow
          label={label}
          description={description}
          testId={testId}
          className={px}
          control={
            <span data-testid={`${testId}-state`} className="text-ui-sm text-foreground-muted">
              {on ? onWord : offWord}
            </span>
          }
        />
        <div className={cn("flex flex-col gap-1 px-4 pb-3", flush && "px-0")}>
          <p data-testid={`${testId}-read-only`} className="text-ui-sm text-foreground-muted">
            Change this on the daemon&apos;s machine.
          </p>
          {restartLine}
        </div>
      </div>
    );
  }

  const hasNotes = Boolean(restartLine || openDaemon || (set.error && !confirming));
  return (
    <div data-testid={`${testId}-wrap`}>
      <SettingsRow
        label={label}
        description={description}
        testId={testId}
        className={px}
        control={
          <div className="flex items-center gap-2">
            {envLocked && (
              <span data-testid={`${testId}-env`} className="text-ui-sm text-foreground-muted">
                Overridden by {view.env}
              </span>
            )}
            <Switch
              data-testid={`${testId}-switch`}
              aria-label={label}
              checked={on}
              disabled={busy || envLocked}
              aria-busy={busy}
              onCheckedChange={onToggle}
            />
          </div>
        }
      />
      {hasNotes && (
        <div className={cn("flex flex-col items-start gap-1 px-4 pb-3", flush && "px-0")}>
          {notes}
          {set.error && !confirming && <Alert variant="error" testId={`${testId}-error`} description={messageOf(set.error)} />}
        </div>
      )}
      <ConfirmDialog
        open={confirming}
        onOpenChange={(open) => {
          setConfirming(open);
          if (!open) set.reset();
        }}
        title={isPause ? "Resume automations" : label}
        description={isPause ? RESUME_CONFIRM : CONFIG_CONFIRM[configKey]}
        confirmLabel={isPause ? "Resume" : "Turn on"}
        pending={busy}
        error={set.error ? messageOf(set.error) : null}
        destructive={false}
        testId={`${testId}-dialog`}
        onConfirm={() => {
          void send(!isPause).then((ok) => {
            if (ok) setConfirming(false);
          });
        }}
      />
    </div>
  );
}
