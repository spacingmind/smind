import { useCopyFeedback } from "@/components/timeline/message-actions";
import { Button } from "@/components/ui/button";
import { useDaemonManagedBy } from "@/hooks/use-attachment-limits";
import { Icons } from "@/lib/icons";

const RESTART_SERVICE = "smind service restart";
const RESTART_MANUAL = "smind stop && smind serve";

const RAILS = ["Each run has a budget cap", "An automation pauses itself after repeated failures", "Automations never merge or release"];

/**
 * What the page shows while the daemon has `automations.enabled: false`: the list is fetched (so the switch to on
 * lands live) but not rendered -- this card explains what automations are and how to turn them on, with no control
 * that would change the flag from here (a config.yaml edit on purpose).
 */
export function AutomationsOffCard() {
  const { copied, copy } = useCopyFeedback();
  const managedBy = useDaemonManagedBy();
  const service = managedBy === "launchd" || managedBy === "systemd";
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
      <p className="text-ui-sm text-foreground">Turn them on from the computer running the daemon:</p>
      <div className="flex items-center gap-2 rounded-lg border border-border bg-muted px-2 py-1.5">
        <code className="min-w-0 flex-1 break-all font-mono text-ui-sm text-foreground">smind config set automations.enabled true</code>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          aria-label="Copy command"
          data-testid="automations-off-copy"
          onClick={() => void copy("smind config set automations.enabled true")}
        >
          {copied ? <Icons.check aria-hidden className="size-3.5" /> : <Icons.copy aria-hidden className="size-3.5" />}
          {copied ? "Copied" : "Copy"}
        </Button>
      </div>
      <p className="text-ui-sm text-foreground">Then restart the daemon:</p>
      {managedBy === undefined ? (
        <>
          <p className="text-ui-sm text-foreground-muted">If you run it as a service:</p>
          <RestartCommand>{RESTART_SERVICE}</RestartCommand>
          <p className="text-ui-sm text-foreground-muted">Otherwise:</p>
          <RestartCommand>{RESTART_MANUAL}</RestartCommand>
        </>
      ) : (
        <RestartCommand>{service ? RESTART_SERVICE : RESTART_MANUAL}</RestartCommand>
      )}
      <p className="text-ui-sm text-foreground-muted">This switch lives in config.yaml on purpose; it can't be changed from here.</p>
    </div>
  );
}

function RestartCommand({ children }: { children: string }) {
  return <code className="rounded-lg border border-border bg-muted px-2 py-1.5 font-mono text-ui-sm text-foreground">{children}</code>;
}
