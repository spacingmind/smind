import { useCopyFeedback } from "@/components/timeline/message-actions";
import { Button } from "@/components/ui/button";
import { ConfigSwitchRow } from "@/components/settings/config-switch-row";
import { useConfigKey } from "@/hooks/use-config";
import { Icons } from "@/lib/icons";

const COMMAND = "smind config set automations.enabled true";

const RAILS = ["Each run has a budget cap", "An automation pauses itself after repeated failures", "Automations never merge or release"];

/**
 * What the page shows while the daemon has `automations.enabled: false`: the list is fetched (so the switch to on
 * lands live) but not rendered -- this card explains what automations are and holds the switch (live, no restart). Where the
 * connection is read-only it keeps the command to run on the daemon's machine instead.
 */
export function AutomationsOffCard() {
  const { copied, copy } = useCopyFeedback();
  // The old command block is the way in only when this connection cannot flip the switch itself (a relay session); never while the answer is still loading.
  const { view, isPending } = useConfigKey("automations.enabled");
  const readOnly = !isPending && view?.writable !== true && view?.source !== "env";
  return (
    <div data-testid="automations-off-card" className="flex flex-col gap-3 rounded-xl border bg-card p-4">
      <p className="text-ui-base font-medium text-foreground">Automations are off</p>
      <p className="text-ui-sm text-foreground-muted">
        An automation starts a task for you: on a cron schedule (always UTC) or when a GitHub workflow fails (CI failure becomes a task).
      </p>
      <div className="flex flex-col gap-1">
        <span className="text-ui-sm font-medium text-foreground">Rails</span>
        <ul className="flex flex-col gap-1">
          {RAILS.map((rail) => (
            <li key={rail} className="text-ui-sm text-foreground-muted">
              {rail}
            </li>
          ))}
        </ul>
      </div>
      <ConfigSwitchRow
        configKey="automations.enabled"
        label="Automations"
        description="Rules that start a task when a signal fires."
        testId="config-automations-enabled"
        flush
      />
      {readOnly && (
        <>
          <p className="text-ui-sm text-foreground">Turn them on from the computer running the daemon:</p>
          <div className="flex items-center gap-2 rounded-lg border border-border bg-muted px-2 py-1.5">
            <code className="min-w-0 flex-1 break-all font-mono text-ui-sm text-foreground">{COMMAND}</code>
            <Button type="button" variant="ghost" size="sm" aria-label="Copy command" data-testid="automations-off-copy" onClick={() => void copy(COMMAND)}>
              {copied ? <Icons.check aria-hidden className="size-3.5" /> : <Icons.copy aria-hidden className="size-3.5" />}
              {copied ? "Copied" : "Copy"}
            </Button>
          </div>
        </>
      )}
    </div>
  );
}
