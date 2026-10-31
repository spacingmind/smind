import type { ReactNode } from "react";
import { useQueryClient } from "@tanstack/react-query";

import { PluginView } from "@/components/plugin-view/plugin-view";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { LoadingRows } from "@/components/ui/loading-rows";
import { PaneHeader } from "@/components/ui/pane-header";
import { ScrollArea } from "@/components/ui/scroll-area";
import { usePluginActionRunner } from "@/hooks/use-plugin-action-runner";
import { findPanel, usePluginPanels, usePluginViewQuery } from "@/hooks/use-plugin-panels";
import { Icons } from "@/lib/icons";
import { queryKeys } from "@/lib/query-keys";

/**
 * One plugin panel's tab content (ADR-0042): the `plugin` tab kind's body.
 * The pane owns fetching the panel's view document (`plugin.view`) and the
 * loading / failure states around it; the document itself is rendered by
 * the declarative `PluginView`, whose every action comes back here through
 * `onAction`.
 */

/** A typed `plugin.view` failure -> the sentence a reader needs, keeping the daemon's detail when it names the cause. */
function panelErrorCopy(err: unknown): { headline: string; detail: string | null } {
  const message = err instanceof Error ? err.message : String(err);
  switch ((err as { code?: string } | null)?.code) {
    case "ui_level_too_low":
      return { headline: "This panel needs a newer app version.", detail: null };
    case "view_invalid":
      // The daemon's message names the JSON path that failed validation.
      return { headline: "The plugin sent an invalid panel.", detail: message };
    case "plugin_unavailable":
      return { headline: "The plugin is not running.", detail: null };
    case "unknown_panel":
      return { headline: "This panel is no longer available.", detail: null };
    default:
      return { headline: "Could not load this panel.", detail: message };
  }
}

export function PluginPanelPane({
  pluginId,
  panelId,
  taskId,
  focused,
}: {
  pluginId: string;
  panelId: string;
  taskId?: number;
  focused?: boolean;
}) {
  void focused;
  const queryClient = useQueryClient();
  const panels = usePluginPanels();
  const title = findPanel(panels.panels, pluginId, panelId)?.title ?? panelId;
  const view = usePluginViewQuery({
    pluginId,
    panelId,
    context: taskId === undefined ? undefined : { taskId },
  });
  const { run, pendingUrl, confirmUrl, cancelUrl } = usePluginActionRunner({ pluginId, panelId });

  const refresh = () => {
    // The panel-scoped prefix, so a task panel's context-specific view refreshes too.
    void queryClient.invalidateQueries({ queryKey: queryKeys.pluginViewOfPanel(pluginId, panelId) });
  };

  let body: ReactNode;
  if (view.isPending) {
    body = (
      <div data-testid="plugin-panel-loading">
        <LoadingRows rows={3} />
      </div>
    );
  } else if (view.isError) {
    const copy = panelErrorCopy(view.error);
    body = (
      <Alert variant="error" testId="plugin-panel-error" title={copy.headline} description={copy.detail ?? undefined}>
        <Button variant="outline" size="sm" data-testid="plugin-panel-error-retry" onClick={() => void view.refetch()}>
          <Icons.retry aria-hidden /> Retry
        </Button>
      </Alert>
    );
  } else {
    body = <PluginView doc={view.data} onAction={(action, extra) => void run(action, extra)} />;
  }

  return (
    <div
      data-testid="plugin-panel-pane"
      data-plugin={pluginId}
      data-panel={panelId}
      className="flex h-full min-h-0 flex-col"
    >
      <PaneHeader
        testId="plugin-panel-header"
        title={
          <>
            <span className="truncate">{title}</span>
            <span className="shrink-0 text-ui-sm font-normal text-foreground-muted">{pluginId}</span>
          </>
        }
        actions={
          <Button
            variant="ghost"
            size="icon-xs"
            aria-label="Refresh panel"
            title="Refresh panel"
            data-testid="plugin-panel-refresh"
            onClick={refresh}
          >
            <Icons.retry aria-hidden />
          </Button>
        }
      />
      <ScrollArea className="min-h-0 flex-1">
        <div className="mx-auto w-full max-w-3xl p-4">{body}</div>
      </ScrollArea>
      <ConfirmDialog
        open={pendingUrl !== null}
        onOpenChange={(open) => {
          if (!open) cancelUrl();
        }}
        title="Open link in your browser?"
        description={
          pendingUrl && (
            <div className="flex flex-col gap-2">
              <p>
                This will open <span className="font-medium text-foreground">{pendingUrl.host}</span> in your browser.
                Plugin {pluginId} asked for it.
              </p>
              <code className="block max-w-full overflow-x-auto rounded-md bg-muted px-2 py-1 font-mono text-ui-sm text-foreground-muted">
                {pendingUrl.url}
              </code>
            </div>
          )
        }
        confirmLabel="Open"
        pending={false}
        destructive={false}
        testId="plugin-openurl-confirm"
        onConfirm={confirmUrl}
      />
    </div>
  );
}
