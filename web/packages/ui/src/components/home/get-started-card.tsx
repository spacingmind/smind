import { useEffect, type ReactNode } from "react";

import { Button } from "@/components/ui/button";
import { Icons } from "@/lib/icons";
import type { ProviderHealth } from "@/lib/model-browser";
import { providerLabel } from "@/lib/provider-catalog";
import { agentStatus, agentStatusLabel, agentsReady } from "@/lib/readiness";
import type { ProviderInfo, Workspace } from "@/lib/types";
import { useGetStartedDismissedStore } from "@/stores/get-started-store";

function projectName(w: Workspace): string {
  return w.Title || w.Path.split("/").filter(Boolean).pop() || w.Path;
}

/**
 * The Get started card (E5-A AC1): two live rows -- *Sign in to an agent* and
 * *Add a project* -- shown above the composer until both steps are met or the
 * card is dismissed (a browser preference; Settings > General re-opens it).
 * The parent passes the provider list, health answers and `ensure`; the card
 * never calls a hook that could fetch on render -- `ensure()` runs once per
 * visible mount, inside VisibleCard.
 */
export function GetStartedCard({
  connected = true,
  workspaces,
  providers,
  health,
  rpcFailed,
  ensure,
  onAddProject,
}: {
  /** False while the daemon is unreachable: no agent can be checked and the connection notice is the whole story, so the card hides. */
  connected?: boolean;
  workspaces: Workspace[];
  providers: ProviderInfo[];
  health: Record<string, ProviderHealth | undefined>;
  rpcFailed: ReadonlySet<string>;
  ensure: () => void;
  onAddProject?: () => void;
}) {
  const dismissed = useGetStartedDismissedStore((s) => s.value);
  const ready = agentsReady(providers, health, rpcFailed);
  const hasProject = workspaces.length > 0;
  if (!connected || dismissed || (ready === "ready" && hasProject)) return null;
  return (
    <VisibleCard
      workspaces={workspaces}
      providers={providers}
      health={health}
      ready={ready}
      hasProject={hasProject}
      ensure={ensure}
      onAddProject={onAddProject}
    />
  );
}

/** The rendered card: mounted only while visible, so its mount effect is the card's single `ensure()`. */
function VisibleCard({
  workspaces,
  providers,
  health,
  ready,
  hasProject,
  ensure,
  onAddProject,
}: {
  workspaces: Workspace[];
  providers: ProviderInfo[];
  health: Record<string, ProviderHealth | undefined>;
  ready: "ready" | "none" | "unknown";
  hasProject: boolean;
  ensure: () => void;
  onAddProject?: () => void;
}) {
  const dismiss = useGetStartedDismissedStore((s) => s.set);
  // A fresh machine lacks most agent CLIs: list the ones that exist (signed out or still checking) and fold the rest into a count.
  const missing = providers.filter((p) => agentStatus(health[p.id]) === "not-installed");
  const visibleProviders = providers.filter((p) => agentStatus(health[p.id]) !== "not-installed");
  const missingCount = missing.length;

  // The card usually mounts before provider.list answers; ensure() is a
  // single-flight no-op per already-tested id, so re-running it when the
  // catalog arrives still tests each provider exactly once -- plain
  // re-renders and a hidden card never call it.
  useEffect(() => {
    ensure();
  }, [ensure, providers.length]);

  // One sentence for the agent step: each installed provider's status, then what is missing, joined with a middle dot.
  const parts = visibleProviders.map((p) => `${providerLabel(p.id, [p])} - ${agentStatusLabel(agentStatus(health[p.id]))}`);
  if (visibleProviders.length === 0 && missingCount > 0) parts.push("No agent is signed in");
  if (missingCount > 0) parts.push(`${missingCount} not installed`);
  const agentLine = parts.join(" · ");

  return (
    <section
      data-testid="get-started-card"
      aria-label="Get started"
      className="mb-3 rounded-xl border border-border bg-card px-4 py-3 text-ui-sm text-foreground"
    >
      <div className="flex flex-wrap items-center gap-2">
        <h2 className="text-ui-base font-medium">Get started</h2>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          className="ml-auto max-sm:min-h-11"
          aria-label="Dismiss get started"
          onClick={() => dismiss(true)}
        >
          Dismiss
        </Button>
      </div>
      <ol className="mt-2 flex flex-col gap-2">
        <Step
          done={ready === "ready"}
          label="Sign in to an agent"
          status={ready === "ready" ? "Done" : agentLine}
          action={
            ready === "ready" ? null : (
              <Button
                type="button"
                variant="outline"
                size="sm"
                className="max-sm:min-h-11"
                data-testid="get-started-open-providers"
                onClick={() =>
                  window.dispatchEvent(new CustomEvent("smind:open-settings", { detail: { sectionId: "providers" } }))
                }
              >
                Open Providers
              </Button>
            )
          }
        />
        <Step
          done={hasProject}
          label="Add a project"
          status={hasProject ? `Done - ${projectName(workspaces[0])}` : "No project yet"}
          action={
            hasProject ? null : (
              <Button
                type="button"
                variant="outline"
                size="sm"
                className="max-sm:min-h-11"
                data-testid="get-started-add-project"
                onClick={onAddProject}
              >
                Add a project
              </Button>
            )
          }
        />
      </ol>
    </section>
  );
}

/**
 * One step as a small block: a status icon (check = done, empty circle = to do) and the label on line 1, the status
 * text under it, then the action on its own line (8 px between steps, 12 px between the status text and the action).
 */
function Step({ done, label, status, action }: { done: boolean; label: string; status: string; action: ReactNode }) {
  return (
    <li className="flex flex-col gap-3 py-1">
      <div className="flex flex-col gap-0.5">
        <div className="flex items-center gap-2">
          {done ? (
            <Icons.check aria-hidden className="size-4 shrink-0 text-foreground-muted" />
          ) : (
            <Icons.circle aria-hidden className="size-4 shrink-0 text-foreground-muted" />
          )}
          <span className="font-medium">{label}</span>
          <span className="sr-only">{done ? "(done)" : "(to do)"}</span>
        </div>
        <p className="pl-6 text-foreground-muted">{status}</p>
      </div>
      {action && <div className="pl-6">{action}</div>}
    </li>
  );
}
