import { useCallback, useEffect, useState } from "react";

import { Button } from "@/components/ui/button";
import { InlineSpinner } from "@/components/ui/inline-spinner";
import { Icons } from "@/lib/icons";
import type { ReadinessFinding } from "@/lib/types";
import { getWorkspaceReadiness } from "@/lib/worktree-setup";
import type { WsClientLike } from "@/lib/ws-client";

const STATUS_ICON = {
  pass: { Icon: Icons.success, className: "text-success", label: "Pass" },
  warn: { Icon: Icons.warning, className: "text-warning", label: "Warning" },
  info: { Icon: Icons.info, className: "text-foreground-subtlest", label: "Info" },
} as const;

/**
 * "Agent readiness": a static, offline check of the workspace's repo (agent
 * instructions, test/lint command, setup script, .gitignore, CI, docs,
 * lockfiles). It runs when mounted and again on Re-check; it only reads, and
 * a warning is advice, never a block.
 */
export function ReadinessPanel({ client, workspaceId }: { client: WsClientLike | null; workspaceId: number }) {
  const [findings, setFindings] = useState<ReadinessFinding[] | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [run, setRun] = useState(0);

  useEffect(() => {
    if (!client) return;
    let cancelled = false;
    setLoading(true);
    setError(null);
    getWorkspaceReadiness(client, workspaceId)
      .then((res) => {
        if (cancelled) return;
        setFindings(res.findings);
        setLoading(false);
      })
      .catch((err) => {
        if (cancelled) return;
        setError(err instanceof Error ? err.message : String(err));
        setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [client, workspaceId, run]);

  const recheck = useCallback(() => setRun((n) => n + 1), []);
  const warns = findings?.filter((f) => f.status === "warn").length ?? 0;

  return (
    <section className="grid min-w-0 gap-2" aria-label="Agent readiness" data-testid="readiness-panel">
      <div className="flex items-center justify-between gap-2">
        <div className="min-w-0">
          <h3 className="text-ui-base font-medium">Agent readiness</h3>
          <p className="text-ui-sm text-muted-foreground">
            {findings && !loading
              ? warns === 0
                ? "Nothing to fix."
                : `${warns} suggestion${warns === 1 ? "" : "s"}. Advice only; tasks are never blocked.`
              : "Checks the repo's files only. Nothing is run."}
          </p>
        </div>
        <Button
          type="button"
          variant="quiet"
          size="sm"
          className="shrink-0"
          onClick={recheck}
          disabled={loading}
          data-testid="readiness-recheck"
        >
          <Icons.refresh aria-hidden className={loading ? "size-3.5 animate-spin" : "size-3.5"} data-icon="inline-start" />
          Re-check
        </Button>
      </div>
      {error && (
        <p role="alert" data-testid="readiness-error" className="text-ui-base text-destructive">
          {error}
        </p>
      )}
      {loading && !findings && <InlineSpinner label="Checking…" />}
      {findings && (
        <ul className="grid min-w-0 gap-1.5" data-testid="readiness-list" aria-busy={loading}>
          {findings.map((f) => {
            const { Icon, className, label } = STATUS_ICON[f.status] ?? STATUS_ICON.info;
            return (
              <li
                key={f.id}
                className="flex items-start gap-2 text-ui-base"
                data-testid={`readiness-${f.id}`}
                data-status={f.status}
              >
                <Icon aria-hidden className={`mt-0.5 size-3.5 shrink-0 ${className}`} />
                <div className="min-w-0">
                  <div>
                    <span className="sr-only">{label}: </span>
                    {f.title}
                  </div>
                  <div className="text-ui-sm break-words text-muted-foreground">{f.evidence}</div>
                  {f.fix && <div className="text-ui-sm text-foreground-muted">{f.fix}</div>}
                </div>
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
}
