import { useEffect } from "react";

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
  workspaces,
  providers,
  health,
  rpcFailed,
  ensure,
  onAddProject,
}: {
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
  if (dismissed || (ready === "ready" && hasProject)) return null;
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
          className="ml-auto"
          aria-label="Dismiss get started"
          onClick={() => dismiss(true)}
        >
          Dismiss
        </Button>
      </div>
      <ol className="mt-1 flex flex-col">
        <li className="flex min-h-11 flex-wrap items-center gap-x-2 gap-y-1">
          <span className="font-medium">Sign in to an agent</span>
          {ready === "ready" ? (
            <span className="flex items-center gap-1 text-foreground-muted">
              <Icons.check aria-hidden className="size-3.5" />
              Done
            </span>
          ) : (
            <>
              {visibleProviders.map((p) => (
                <span key={p.id} className="text-foreground-muted">
                  {`${providerLabel(p.id, [p])} - ${agentStatusLabel(agentStatus(health[p.id]))}`}
                </span>
              ))}
              {missingCount > 0 && <span className="text-foreground-muted">{`${missingCount} not installed`}</span>}
              <Button
                type="button"
                variant="outline"
                size="sm"
                data-testid="get-started-open-providers"
                onClick={() =>
                  window.dispatchEvent(new CustomEvent("smind:open-settings", { detail: { sectionId: "providers" } }))
                }
              >
                Open Providers
              </Button>
            </>
          )}
        </li>
        <li className="flex min-h-11 flex-wrap items-center gap-x-2 gap-y-1">
          <span className="font-medium">Add a project</span>
          {hasProject ? (
            <span className="flex items-center gap-1 text-foreground-muted">
              <Icons.check aria-hidden className="size-3.5" />
              {`Done - ${projectName(workspaces[0])}`}
            </span>
          ) : (
            <Button
              type="button"
              variant="outline"
              size="sm"
              data-testid="get-started-add-project"
              onClick={onAddProject}
            >
              Add a project
            </Button>
          )}
        </li>
      </ol>
    </section>
  );
}
