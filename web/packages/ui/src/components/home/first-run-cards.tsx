import { useEffect } from "react";

import { agentSummaryLine, openProvidersSettings } from "@/components/home/get-started-card";
import { Button } from "@/components/ui/button";
import { Icons } from "@/lib/icons";
import type { ProviderHealth } from "@/lib/model-browser";
import { agentsReady } from "@/lib/readiness";
import type { ProviderInfo } from "@/lib/types";

/**
 * The home screen of a machine with no project (docs/plans/active/home-and-sidebar-ui.md PR 3): the composer cannot be used yet, so
 * the two things that unblock it are the whole page, as two cards built from the Get started block's own steps and actions:
 * *Add a project* (the same `onAddProject` as the card) and *Set up providers* (the same Settings -> Providers navigation and the same
 * readiness sentence). Nothing here sends a prompt.
 *
 * A third slot is reserved for "Import a session"; that card does not exist yet, so the grid has exactly two cells and no empty one.
 * Mounted only while there is no project, so its mount effect is the single provider-health `ensure()`.
 */
export function FirstRunCards({
  providers,
  health,
  rpcFailed,
  ensure,
  onAddProject,
}: {
  providers: ProviderInfo[];
  health: Record<string, ProviderHealth | undefined>;
  rpcFailed: ReadonlySet<string>;
  ensure: () => void;
  onAddProject?: () => void;
}) {
  // The catalog usually arrives after the first render; ensure() tests each provider once however often this re-runs.
  useEffect(() => {
    ensure();
  }, [ensure, providers.length]);
  const ready = agentsReady(providers, health, rpcFailed);
  const providersStatus = ready === "ready" ? "An agent is signed in" : agentSummaryLine(providers, health) || "Checking your agents…";

  return (
    <section data-testid="first-run-cards" aria-label="Get started" className="grid w-full grid-cols-1 gap-3 sm:grid-cols-2">
      <article data-testid="first-run-card-project" className="flex flex-col gap-3 rounded-xl border border-border bg-card p-4">
        <div className="flex items-center gap-2">
          <Icons.folder aria-hidden className="size-4 shrink-0 text-foreground-muted" />
          <h2 className="text-ui-base font-medium text-foreground">Add a project</h2>
        </div>
        <p className="flex-1 text-ui-base text-foreground-muted">Choose a folder with your code. Each task works in its own copy of it.</p>
        {onAddProject && (
          <Button type="button" variant="outline" className="self-start max-sm:min-h-11" data-testid="first-run-add-project" onClick={onAddProject}>
            Add a project
          </Button>
        )}
      </article>
      <article data-testid="first-run-card-providers" className="flex flex-col gap-3 rounded-xl border border-border bg-card p-4">
        <div className="flex items-center gap-2">
          <Icons.agent aria-hidden className="size-4 shrink-0 text-foreground-muted" />
          <h2 className="text-ui-base font-medium text-foreground">Set up providers</h2>
        </div>
        <p data-testid="first-run-providers-status" className="flex-1 text-ui-base text-foreground-muted">
          {providersStatus}
        </p>
        <Button type="button" variant="outline" className="self-start max-sm:min-h-11" data-testid="first-run-open-providers" onClick={openProvidersSettings}>
          Open Providers
        </Button>
      </article>
      {/* Import a session: the third card goes here when it exists (plan PR 3 leaves the slot, not a dead cell). */}
    </section>
  );
}
