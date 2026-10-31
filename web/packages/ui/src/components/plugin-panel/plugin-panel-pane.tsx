/**
 * One plugin panel's tab content (ADR-0042): the `plugin` tab kind's body.
 *
 * STUB: renders only the panel's identity. The real content -- the
 * `plugin.view` document fetched per (plugin, panel, context) and rendered
 * by the declarative view renderer -- arrives in the next step of the plan.
 */
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
  void taskId;
  void focused;
  return (
    <div
      data-testid="plugin-panel-pane"
      data-plugin={pluginId}
      data-panel={panelId}
      className="flex h-full items-center justify-center text-ui-base text-muted-foreground"
    >
      {pluginId}:{panelId}
    </div>
  );
}
