import { useState, type FormEvent } from "react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useRunTimeline, type RunEntry } from "@/hooks/use-run-timeline";
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
export function TaskDetailPane({ client, task }: { client: WsClientLike | null; task: Task }) {
  const { runs, error, submitPrompt, stopRun } = useRunTimeline(client, task.ID);

  return (
    <div className="flex h-full flex-col">
      <div className="border-b px-4 py-3">
        <h2 className="truncate text-sm font-semibold">{task.Title}</h2>
        <div className="mt-1 flex items-center gap-2 text-xs text-muted-foreground">
          <span className="uppercase">{task.Status}</span>
          {task.Branch && <span className="truncate">{task.Branch}</span>}
        </div>
      </div>

      <div className="flex-1 overflow-y-auto px-4 py-3">
        {error && <p className="text-sm text-destructive">{error}</p>}
        {!error && runs === null && <p className="text-sm text-muted-foreground">Loading runs…</p>}
        {!error && runs !== null && runs.length === 0 && (
          <p className="text-sm text-muted-foreground">No runs yet. Send a prompt to start one.</p>
        )}
        {runs !== null && runs.length > 0 && (
          <ul className="space-y-4">
            {runs.map((run) => (
              <RunEntryView key={run.id} run={run} onStop={stopRun} />
            ))}
          </ul>
        )}
      </div>

      <PromptForm onSubmit={submitPrompt} disabled={!client} />
    </div>
  );
}

function RunEntryView({ run, onStop }: { run: RunEntry; onStop: (runId: string) => Promise<void> }) {
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
      {run.err && <p className="mt-1 text-xs text-destructive">{run.err}</p>}
      {stopError && <p className="mt-1 text-xs text-destructive">stop failed: {stopError}</p>}
    </li>
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
