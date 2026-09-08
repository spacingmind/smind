import { useState, type FormEvent } from "react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useRunTimeline, type RunEntry } from "@/hooks/use-run-timeline";
import type { ConnectionStatus } from "@/lib/reconnect";
import type { WsClientLike } from "@/lib/ws-client";
import type { Provider, Task } from "@/lib/types";

const PROVIDERS: Provider[] = ["claude-native", "glm"];

/**
 * The main-content pane for a selected task: identity header, a chat-log
 * timeline of its runs (history plus any still-live streaming), and a
 * prompt form that starts new runs via run.start + run.attach (never
 * task.prompt -- see the hook this delegates to for why). All data comes
 * from useRunTimeline; this component is presentation plus the form's own
 * local (provider/prompt/submitting) state.
 */
export function TaskDetailPane({
  client,
  task,
  connectionStatus = "connected",
}: {
  client: WsClientLike | null;
  task: Task;
  /** Real-time connection status from App.tsx -- lets an active run.attach subscription visibly reflect a break instead of silently freezing on stale "live" output. Defaults to "connected" so every existing caller/test not wired up to App.tsx's status keeps behaving exactly as before. */
  connectionStatus?: ConnectionStatus;
}) {
  const { runs, error, submitPrompt, stopRun, respondPermission } = useRunTimeline(client, task.ID);

  return (
    <div className="flex h-full flex-col">
      <div className="border-b px-4 py-3">
        <h2 className="truncate text-sm font-semibold">{task.Title}</h2>
        <div className="mt-1 flex items-center gap-2 text-xs text-muted-foreground">
          <span className="uppercase">{task.Status}</span>
          {task.Branch && <span className="truncate">{task.Branch}</span>}
        </div>
      </div>

      {connectionStatus === "reconnecting" && (
        <p data-testid="connection-banner" className="border-b bg-amber-500/10 px-4 py-1 text-xs text-amber-600">
          Connection lost -- reconnecting to daemon…
        </p>
      )}

      <div className="flex-1 overflow-y-auto px-4 py-3">
        {error && <p className="text-sm text-destructive">{error}</p>}
        {!error && runs === null && <p className="text-sm text-muted-foreground">Loading runs…</p>}
        {!error && runs !== null && runs.length === 0 && (
          <p className="text-sm text-muted-foreground">No runs yet. Send a prompt to start one.</p>
        )}
        {runs !== null && runs.length > 0 && (
          <ul className="space-y-4">
            {runs.map((run) => (
              <RunEntryView key={run.id} run={run} onStop={stopRun} onRespondPermission={respondPermission} />
            ))}
          </ul>
        )}
      </div>

      <PromptForm onSubmit={submitPrompt} disabled={!client} />
    </div>
  );
}

function RunEntryView({
  run,
  onStop,
  onRespondPermission,
}: {
  run: RunEntry;
  onStop: (runId: string) => Promise<void>;
  onRespondPermission: (runId: string, requestId: string, optionId: string) => Promise<void>;
}) {
  const [stopping, setStopping] = useState(false);
  const [stopError, setStopError] = useState<string | null>(null);

  async function handleStop() {
    setStopping(true);
    setStopError(null);
    try {
      await onStop(run.id);
    } catch (err) {
      setStopError(err instanceof Error ? err.message : String(err));
      setStopping(false);
    }
    // On success, leave `stopping` true: the run's own run.attach
    // subscription observes the stop as its terminal response and patches
    // `run.status` away from "running" shortly, which unmounts this button
    // (see the status !== "running" guard below) -- no need to reset here.
  }

  return (
    <li data-testid="run-entry" data-run-id={run.id} className="rounded-lg border p-3">
      <div className="flex items-center justify-between text-xs text-muted-foreground">
        <span>{run.provider}</span>
        <div className="flex items-center gap-2">
          <span className="uppercase" data-testid="run-status">
            {run.status}
          </span>
          {run.status === "running" && (
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="h-5 px-2 text-xs"
              disabled={stopping}
              onClick={handleStop}
            >
              Stop
            </Button>
          )}
        </div>
      </div>
      <p className="mt-1 text-sm font-medium">{run.prompt}</p>
      <pre className="mt-2 whitespace-pre-wrap text-sm" data-testid="run-text">
        {run.text}
      </pre>
      {run.pendingPermission && (
        <PendingPermissionView
          runId={run.id}
          pending={run.pendingPermission}
          onRespond={onRespondPermission}
        />
      )}
      {run.err && <p className="mt-1 text-xs text-destructive">{run.err}</p>}
      {stopError && <p className="mt-1 text-xs text-destructive">stop failed: {stopError}</p>}
    </li>
  );
}

/**
 * Inline prompt for a run's pending permission request: what's being
 * requested, plus one button per option. Clicking a button calls
 * run.respondPermission (via onRespond); this component never clears the
 * pending state itself on click -- the parent's pendingPermission prop
 * disappearing (once a "permission_resolved" event arrives, from this tab's
 * own click or another connection entirely) is what unmounts it, so both
 * cases are handled identically.
 */
function PendingPermissionView({
  runId,
  pending,
  onRespond,
}: {
  runId: string;
  pending: NonNullable<RunEntry["pendingPermission"]>;
  onRespond: (runId: string, requestId: string, optionId: string) => Promise<void>;
}) {
  const [respondingTo, setRespondingTo] = useState<string | null>(null);
  const [respondError, setRespondError] = useState<string | null>(null);

  async function handleClick(optionId: string) {
    setRespondingTo(optionId);
    setRespondError(null);
    try {
      await onRespond(runId, pending.requestId, optionId);
    } catch (err) {
      setRespondError(err instanceof Error ? err.message : String(err));
      setRespondingTo(null);
    }
    // On success, leave the buttons disabled: the run's own subscription
    // observes the matching "permission_resolved" event and this component
    // unmounts (pendingPermission clears) shortly -- no need to reset here.
  }

  return (
    <div data-testid="pending-permission" className="mt-2 rounded-md border border-amber-500/50 bg-amber-500/10 p-2">
      <p className="text-sm font-medium">{pending.summary}</p>
      <div className="mt-2 flex flex-wrap gap-2">
        {pending.options.map((option) => (
          <Button
            key={option.id}
            type="button"
            variant="outline"
            size="sm"
            className="h-6 px-2 text-xs"
            disabled={respondingTo !== null}
            onClick={() => handleClick(option.id)}
          >
            {option.label}
          </Button>
        ))}
      </div>
      {respondError && <p className="mt-1 text-xs text-destructive">{respondError}</p>}
    </div>
  );
}

function PromptForm({
  onSubmit,
  disabled,
}: {
  onSubmit: (provider: Provider, prompt: string) => Promise<void>;
  disabled: boolean;
}) {
  const [provider, setProvider] = useState<Provider>("claude-native");
  const [prompt, setPrompt] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);

  async function handleSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const trimmed = prompt.trim();
    if (!trimmed || submitting) return;

    setSubmitting(true);
    setFormError(null);
    try {
      await onSubmit(provider, trimmed);
      setPrompt("");
    } catch (err) {
      setFormError(err instanceof Error ? err.message : String(err));
    } finally {
      setSubmitting(false);
    }
  }

  const inactive = disabled || submitting;

  return (
    <form onSubmit={handleSubmit} className="flex items-center gap-2 border-t px-4 py-3">
      <select
        aria-label="Provider"
        value={provider}
        onChange={(e) => setProvider(e.target.value as Provider)}
        disabled={inactive}
        className="h-8 shrink-0 rounded-lg border border-input bg-transparent px-2 text-sm outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 disabled:opacity-50"
      >
        {PROVIDERS.map((p) => (
          <option key={p} value={p}>
            {p}
          </option>
        ))}
      </select>
      <Input
        aria-label="Prompt"
        value={prompt}
        onChange={(e) => setPrompt(e.target.value)}
        placeholder="Send a prompt…"
        disabled={inactive}
      />
      <Button type="submit" disabled={inactive || !prompt.trim()}>
        Send
      </Button>
      {formError && <span className="text-xs text-destructive">{formError}</span>}
    </form>
  );
}
