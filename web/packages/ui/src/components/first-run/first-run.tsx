import { useEffect, useRef, useState, type ReactNode } from "react";

import { ConnectMethodPanel, useConnectMethod } from "@/components/first-run/connect-methods";
import { describeLocalSituation, type ChoiceId, type LocalSituation } from "@/components/first-run/local-situation";
import { DesktopWindowControls } from "@/components/desktop-window-controls";
import { Button } from "@/components/ui/button";
import { InlineSpinner } from "@/components/ui/inline-spinner";
import { useWindowState } from "@/hooks/use-window-state";
import { Icons } from "@/lib/icons";
import { desktop, desktopOS, isDesktop, type Connection } from "@/lib/platform";
import { cn } from "@/lib/utils";
import { headerLeftInsetPx } from "@/lib/window-chrome";

const BASE_PADDING_PX = 8;

function errorText(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

const sleep = (ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms));

function ChoiceCard({
  id,
  icon,
  title,
  summary,
  selected,
  disabled,
  disabledReason,
  onSelect,
  children,
}: {
  id: ChoiceId;
  icon: ReactNode;
  title: string;
  summary: string;
  selected: boolean;
  disabled?: boolean;
  disabledReason?: string | null;
  onSelect: (id: ChoiceId) => void;
  children?: ReactNode;
}) {
  return (
    <div
      data-testid={`first-run-choice-${id}`}
      data-selected={selected}
      className={cn("rounded-xl border bg-card transition-colors", selected ? "border-foreground/30" : "border-border")}
    >
      <button
        type="button"
        role="radio"
        aria-checked={selected}
        aria-disabled={disabled || undefined}
        disabled={disabled}
        data-testid={`first-run-radio-${id}`}
        onClick={() => onSelect(id)}
        className="flex w-full items-start gap-3 rounded-xl p-3 text-left outline-none focus-visible:ring-3 focus-visible:ring-ring/50 disabled:cursor-not-allowed"
      >
        <span className={cn("mt-0.5 shrink-0 text-muted-foreground", disabled && "opacity-50")}>{icon}</span>
        <span className="flex min-w-0 flex-1 flex-col gap-0.5">
          <span className={cn("text-ui-base font-medium text-foreground", disabled && "opacity-60")}>{title}</span>
          <span className={cn("text-ui-base text-muted-foreground", disabled && "opacity-60")}>{summary}</span>
        </span>
        <span
          aria-hidden
          className={cn(
            "mt-1 flex size-4 shrink-0 items-center justify-center rounded-full border",
            selected ? "border-primary bg-primary text-primary-foreground" : "border-input-border",
            disabled && "opacity-50",
          )}
        >
          {selected && <Icons.check className="size-3" />}
        </span>
      </button>
      {disabled && disabledReason && (
        <p data-testid={`first-run-reason-${id}`} className="px-3 pb-3 pl-10 text-ui-base text-muted-foreground">
          {disabledReason}
        </p>
      )}
      {selected && !disabled && children && <div className="flex flex-col gap-2 px-3 pb-3 pl-10">{children}</div>}
    </div>
  );
}

/**
 * ADR-0031 D7: the desktop first-run screen -- an explicit choice of how this app gets a
 * daemon, shown before the workspace UI. Structure ported from Paseo's welcome screen
 * (method list: direct / paste link / SSH, and it leaves by itself once a choice
 * succeeds), in smind's own look. Nothing is installed or saved until a choice succeeds;
 * "Skip for now" (or Cancel when reopened) changes nothing.
 */
export function FirstRun({
  rerun = false,
  onDone,
  onSkip,
  pollIntervalMs = 500,
  pollTimeoutMs = 30_000,
}: {
  /** Reopened from the Help menu / Settings rather than shown at first launch. */
  rerun?: boolean;
  /** A choice succeeded and `onboardingCompleted` is saved: enter the app. */
  onDone: () => void;
  /** Dismiss for this session only; nothing is saved. */
  onSkip: () => void;
  pollIntervalMs?: number;
  pollTimeoutMs?: number;
}) {
  const [situation, setSituation] = useState<LocalSituation | null>(null);
  const [choice, setChoice] = useState<ChoiceId | null>(null);
  const [installing, setInstalling] = useState(false);
  const [progress, setProgress] = useState<string | null>(null);
  const [localError, setLocalError] = useState<string | null>(null);
  const [relayError, setRelayError] = useState<string | null>(null);
  const connect = useConnectMethod();
  const busy = useRef(false);
  const mounted = useRef(true);

  const { fullscreen } = useWindowState();
  const dragRegion = isDesktop ? "deep" : undefined;
  const leftInset = isDesktop ? headerLeftInsetPx(desktopOS, fullscreen, 0) : 0;

  useEffect(() => {
    mounted.current = true;
    let cancelled = false;
    desktop
      .daemonStatus()
      .then((status) => describeLocalSituation(status), () => describeLocalSituation(null))
      .then((s) => {
        if (cancelled) return;
        setSituation(s);
        setChoice(s.preselect);
      });
    return () => {
      cancelled = true;
      mounted.current = false;
    };
  }, []);

  function select(id: ChoiceId) {
    setChoice(id);
    setLocalError(null);
    setRelayError(null);
    if (id === "url" || id === "ssh" || id === "relay") connect.setMethod(id);
  }

  async function waitForDaemon(first: { reachable: boolean }) {
    if (first.reachable) return;
    const deadline = Date.now() + pollTimeoutMs;
    while (Date.now() < deadline) {
      await sleep(pollIntervalMs);
      if ((await desktop.daemonStatus()).reachable) return;
    }
    throw new Error(`The daemon did not start answering within ${Math.round(pollTimeoutMs / 1000)} seconds. Check the log from Settings → Daemon, then retry.`);
  }

  async function runHere() {
    if (busy.current || !situation) return;
    busy.current = true;
    setInstalling(true);
    setLocalError(null);
    setProgress(null);
    const unlisten = desktop.onDaemonProgress((p) => setProgress(p.message));
    try {
      await desktop.appSettingsSet({ manageLocalDaemon: true });
      // A daemon this app already manages (re-running setup) is kept, not reinstalled.
      const status = situation.alreadyManaged ? await desktop.daemonStatus() : await desktop.daemonInstall();
      await waitForDaemon(status);
      await desktop.selectConnection("local");
      await desktop.appSettingsSet({ onboardingCompleted: true });
      onDone();
    } catch (err) {
      if (mounted.current) setLocalError(errorText(err));
    } finally {
      unlisten();
      busy.current = false;
      if (mounted.current) {
        setInstalling(false);
        setProgress(null);
      }
    }
  }

  async function useExisting() {
    if (busy.current) return;
    busy.current = true;
    setLocalError(null);
    try {
      await desktop.appSettingsSet({ manageLocalDaemon: false, onboardingCompleted: true });
      await desktop.selectConnection("local");
      onDone();
    } catch (err) {
      if (mounted.current) setLocalError(errorText(err));
    } finally {
      busy.current = false;
    }
  }

  /** (b)/(b2)/(c): the user declined a local daemon ("no local daemon" mode). Throws so a form can show the failure. */
  async function finishRemote(conn: Connection) {
    await desktop.selectConnection(conn.id);
    await desktop.appSettingsSet({ manageLocalDaemon: false, onboardingCompleted: true });
    onDone();
  }

  const locked = installing;

  return (
    <div data-testid="first-run" className="flex h-full min-h-0 flex-col bg-background text-foreground">
      <header
        className="flex h-12 shrink-0 items-center gap-2 p-2"
        data-testid="first-run-header"
        data-tauri-drag-region={dragRegion}
        style={leftInset > 0 ? { paddingLeft: BASE_PADDING_PX + leftInset } : undefined}
      >
        <div className="ml-auto" />
        <DesktopWindowControls />
      </header>
      <div className="min-h-0 flex-1 overflow-y-auto">
        <main className="mx-auto flex w-full max-w-xl flex-col gap-6 px-6 pb-10 pt-4">
          <div className="flex flex-col gap-1">
            <h1 className="text-ui-xl font-semibold" data-testid="first-run-title">
              {rerun ? "Run setup again" : "Welcome to smind"}
            </h1>
            <p className="text-ui-base text-muted-foreground">
              Choose how this app gets a daemon to talk to. You can change this later in Settings.
            </p>
          </div>

          {!situation ? (
            <InlineSpinner label="Checking this computer…" />
          ) : (
            <div role="radiogroup" aria-label="How to connect" className="flex flex-col gap-2" data-testid="first-run-choices">
              {situation.existing && (
                <ChoiceCard
                  id="existing"
                  icon={<Icons.connections aria-hidden className="size-4" />}
                  title="Use the daemon already running here"
                  summary={situation.existing.text}
                  selected={choice === "existing"}
                  disabled={locked}
                  onSelect={select}
                >
                  <div>
                    <Button size="sm" data-testid="first-run-existing-connect" onClick={() => void useExisting()}>
                      Connect
                    </Button>
                  </div>
                  <p className="text-ui-sm text-muted-foreground">The app never installs, starts, updates or takes over this daemon.</p>
                  {localError && choice === "existing" && <FirstRunError testId="first-run-error" text={localError} />}
                </ChoiceCard>
              )}

              {situation.unsupported ? (
                <div data-testid="first-run-linux-guidance" className="flex items-start gap-3 rounded-xl border border-border bg-card p-3">
                  <Icons.host aria-hidden className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
                  <div className="flex min-w-0 flex-col gap-1">
                    <span className="text-ui-base font-medium">Run smind on this computer</span>
                    <span className="text-ui-base text-muted-foreground">
                      The app can&apos;t manage a daemon on this platform. Install one as a service with{" "}
                      <code className="font-mono text-ui-sm">smind service install</code>, then run setup again to use it.
                    </span>
                  </div>
                </div>
              ) : (
                <ChoiceCard
                  id="run"
                  icon={<Icons.host aria-hidden className="size-4" />}
                  title="Run smind on this computer"
                  summary={
                    situation.alreadyManaged
                      ? "smind is already running here and managed by this app."
                      : "The app installs and manages a local daemon for you."
                  }
                  selected={choice === "run"}
                  disabled={locked || situation.runBlocked !== null}
                  disabledReason={situation.runBlocked}
                  onSelect={select}
                >
                  <div className="flex items-center gap-2">
                    <Button size="sm" data-testid="first-run-run" disabled={installing} onClick={() => void runHere()}>
                      {localError ? "Retry" : "Run here"}
                    </Button>
                    {installing && <InlineSpinner label={progress ?? "Starting…"} />}
                  </div>
                  {localError && choice === "run" && <FirstRunError testId="first-run-error" text={localError} />}
                </ChoiceCard>
              )}
              {situation.wsl && !situation.existing && (
                <p data-testid="first-run-wsl" className="px-1 text-ui-sm text-muted-foreground">
                  If you run smind inside WSL, stop it there first: its port is forwarded to Windows and would collide with the daemon the app installs.
                </p>
              )}

              <ChoiceCard
                id="url"
                icon={<Icons.web aria-hidden className="size-4" />}
                title="Connect to a daemon"
                summary="Enter the address of a smind daemon you can reach."
                selected={choice === "url"}
                disabled={locked}
                onSelect={select}
              >
                <ConnectMethodPanel method="url" state={connect} onConnected={finishRemote} />
              </ChoiceCard>
              <ChoiceCard
                id="ssh"
                icon={<Icons.terminal aria-hidden className="size-4" />}
                title="Connect over SSH"
                summary="Tunnel to a daemon on another machine with your system ssh."
                selected={choice === "ssh"}
                disabled={locked}
                onSelect={select}
              >
                <ConnectMethodPanel method="ssh" state={connect} onConnected={finishRemote} />
              </ChoiceCard>
              <ChoiceCard
                id="relay"
                icon={<Icons.link aria-hidden className="size-4" />}
                title="Pair via relay link"
                summary="Paste a pairing link from a relay host: an end-to-end encrypted connection through your relay."
                selected={choice === "relay"}
                disabled={locked}
                onSelect={select}
              >
                <ConnectMethodPanel
                  method="relay"
                  state={connect}
                  onConnected={(conn) => finishRemote(conn).catch((err) => setRelayError(errorText(err)))}
                />
                {relayError && <FirstRunError testId="first-run-relay-error" text={relayError} />}
              </ChoiceCard>
            </div>
          )}

          <div className="flex items-center gap-3 border-t pt-4">
            <Button variant="ghost" size="sm" data-testid="first-run-skip" disabled={locked} onClick={onSkip}>
              {rerun ? "Cancel" : "Skip for now"}
            </Button>
            <span className="text-ui-sm text-muted-foreground">
              {rerun ? "Nothing changes until a choice succeeds." : "Nothing is installed or started. You will be asked again next time."}
            </span>
          </div>
        </main>
      </div>
    </div>
  );
}

function FirstRunError({ text, testId }: { text: string; testId: string }) {
  return (
    <p role="alert" data-testid={testId} className="whitespace-pre-wrap break-words text-ui-base text-destructive">
      {text}
    </p>
  );
}
