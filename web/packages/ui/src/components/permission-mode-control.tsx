import { useState } from "react";

import { Button } from "@/components/ui/button";
import { AUTO_ACCEPT_HELP, AUTO_ACCEPT_LABEL } from "@/lib/permission-modes";
import type { ModeInfo } from "@/lib/types";

/**
 * Live control for switching a running run's provider-native permission
 * mode (ADR-0019): the provider's own modes, applied through its own
 * session (Claude Code's set_permission_mode, an ACP agent's
 * session/set_mode -- see run.setPermissionMode), plus the ACP
 * Auto-accept toggle when the provider takes it. task-detail.tsx only
 * mounts this for a running run whose provider supports a live switch
 * (not Codex).
 */
export function PermissionModeControl({
  modes,
  mode,
  autoAccept,
  showAutoAccept,
  onChange,
}: {
  modes: ModeInfo[];
  mode: string;
  autoAccept: boolean;
  showAutoAccept: boolean;
  onChange: (change: { modeId?: string; autoAccept?: boolean }) => Promise<void>;
}) {
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function change(next: { modeId?: string; autoAccept?: boolean }) {
    if (pending) return;
    setPending(true);
    try {
      await onChange(next);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setPending(false);
    }
  }

  return (
    <div
      data-testid="permission-mode-control"
      className="mx-auto flex w-full max-w-3xl shrink-0 flex-wrap items-center gap-2 px-4 py-1.5 text-ui-sm text-foreground-muted"
    >
      <span>Mode:</span>
      {modes.map((candidate) => (
        <Button
          key={candidate.id}
          type="button"
          variant={candidate.id === mode ? "default" : "outline"}
          size="xs"
          title={candidate.description}
          disabled={pending}
          aria-pressed={candidate.id === mode}
          data-testid={`permission-mode-option-${candidate.id}`}
          onClick={() => candidate.id !== mode && change({ modeId: candidate.id })}
        >
          {candidate.label}
        </Button>
      ))}
      {showAutoAccept && (
        <Button
          type="button"
          variant={autoAccept ? "default" : "outline"}
          size="xs"
          title={AUTO_ACCEPT_HELP}
          disabled={pending}
          aria-pressed={autoAccept}
          data-testid="permission-mode-auto-accept"
          onClick={() => change({ autoAccept: !autoAccept })}
        >
          {AUTO_ACCEPT_LABEL}
        </Button>
      )}
      {error && (
        <span data-testid="permission-mode-error" role="alert" className="text-destructive">
          {error}
        </span>
      )}
    </div>
  );
}
