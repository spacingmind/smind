import { useState } from "react";

import { BrowserAllowlistEditor } from "@/components/settings/browser-allowlist-editor";
import { BrowserReachList } from "@/components/settings/browser-reach-list";
import { registerSettingsSection } from "@/components/settings/settings-registry";
import { SettingsEmptyCard, SettingsGroup, SettingsRow } from "@/components/settings/settings-layout";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { LoadingRows } from "@/components/ui/loading-rows";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { StatusBadge } from "@/components/ui/status-badge";
import { Switch } from "@/components/ui/switch";
import { useBrowserSettings, useClearBrowserData, useSetBrowserSettings } from "@/hooks/use-browser-settings";
import { useWorkspaceTree } from "@/hooks/use-workspace-tree";
import {
  daemonSentence,
  BROWSER_RESIDUAL_LOCAL,
  BROWSER_RESIDUAL_SHELL,
  BROWSER_RESIDUAL_WINDOWS,
} from "@/lib/browser-settings-text";
import { Icons } from "@/lib/icons";

/** The message of a failed call, as the daemon worded it. */
function messageOf(error: unknown): string {
  return daemonSentence(error instanceof Error ? error.message : String(error));
}

/**
 * Settings -> Browser (ADR-0061, docs/plans/completed/browser-for-agents-settings-ui.md): the per-workspace switch
 * that lets agents open and read pages on this machine, its allowlist, the residual limits stated next to the
 * switch, which agents get the browser, and whether a run is using it now. Everything shown comes from the one
 * `browser.settings.get` view; the only changes are `browser.settings.set` and `browser.data.clear`, both
 * human-only and local-only. Nothing is decided here: every refusal is the daemon's own text.
 */
function BrowserSection() {
  const { workspaces, error: treeError } = useWorkspaceTree();
  const [chosen, setChosen] = useState<number | null>(null);
  const workspace = workspaces?.find((w) => w.ID === chosen) ?? workspaces?.[0] ?? null;
  const workspaceId = workspace?.ID ?? null;

  const view = useBrowserSettings(workspaceId);
  const set = useSetBrowserSettings(workspaceId);
  const clear = useClearBrowserData(workspaceId);
  const [clearing, setClearing] = useState(false);
  const [switchError, setSwitchError] = useState<string | null>(null);

  if (!workspaces) {
    return (
      <div className="flex flex-col gap-3" data-testid="settings-section-browser">
        {treeError ? <Alert variant="error" description={treeError} /> : <LoadingRows />}
      </div>
    );
  }
  if (!workspace) {
    return (
      <div data-testid="settings-section-browser">
        <SettingsEmptyCard testId="browser-no-workspaces" title="No workspaces yet" description="Add a workspace to choose whether its agents may use a browser." />
      </div>
    );
  }

  const data = view.data;
  const live = data?.sessionLive === true;
  // Only a change of the switch itself marks the switch busy (an allowlist add or remove has its own busy state).
  const switchPending = set.isPending && set.variables !== undefined && "enabled" in set.variables;

  async function change(c: Parameters<typeof set.mutateAsync>[0]): Promise<void> {
    try {
      await set.mutateAsync(c);
    } catch (e) {
      throw new Error(messageOf(e)); // the allowlist editor shows exactly this text under the input
    }
  }

  return (
    <div className="flex flex-col gap-6" data-testid="settings-section-browser">
      <Select
        value={String(workspace.ID)}
        onValueChange={(v) => {
          setChosen(Number(v));
          setSwitchError(null);
          set.reset();
        }}
      >
        <SelectTrigger aria-label="Workspace" data-testid="browser-workspace-select" className="w-full max-w-xs">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {workspaces.map((w) => (
            <SelectItem key={w.ID} value={String(w.ID)}>
              {w.Title}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>

      {view.isPending && <LoadingRows />}
      {view.error && !data && (
        <div className="flex flex-col gap-2">
          <Alert variant="error" testId="browser-load-error" description={messageOf(view.error)} />
          <div>
            <Button type="button" variant="outline" size="sm" onClick={() => void view.refetch()}>
              Retry
            </Button>
          </div>
        </div>
      )}

      {data && view.isError && (
        <Alert variant="warning" testId="browser-stale" description={`Could not refresh this view, so what is shown may be out of date: ${messageOf(view.error)}`} />
      )}
      {data && (
        <>
          {data.killSwitch && (
            <Alert
              variant="warning"
              testId="browser-kill-switch"
              title="The browser is switched off by the daemon's configuration"
              description="browser.disabled in the daemon's configuration (or SMIND_BROWSER_DISABLED) turns the browser off for every workspace, so the switch below has no effect until that is changed. Open sessions are ended."
            />
          )}
          {!data.available && (
            <Alert variant="info" testId="browser-unavailable" title="Not offered to agents in this version" description="The setting is stored, but agents are not given the browser tools by this daemon version." />
          )}
          {!data.chromeInstalled && (
            <Alert
              variant="warning"
              testId="browser-no-chrome"
              title="No browser installed"
              description={
                <>
                  <span className="block">sMind never downloads one. Until a browser is installed the browser tools are not offered to agents.</span>
                  {data.installStep && (
                    <span data-testid="browser-install-step" className="mt-1 block break-words">
                      {data.installStep}
                    </span>
                  )}
                </>
              }
            />
          )}

          <SettingsGroup
            title="Browser access"
            description="Lets an agent in this workspace open and read pages on this machine. It is off until you turn it on, and the daemon refuses an agent's own request to change it. Read the limits below."
            testId="browser-access"
          >
            <SettingsRow
              label="Let agents use a browser in this workspace"
              description={
                <>
                  Default off. Each use asks you with a permission card, unless you allowed it for that run.
                  {/* Always one line, so a run starting or ending never moves the page: the badge or the plain "not in use" text. */}
                  <span className="mt-1.5 flex h-5 items-center" data-testid="browser-live-line" role="status" aria-live="polite" aria-atomic="true">
                    {live ? (
                      <StatusBadge status="running" dot data-testid="browser-live">
                        In use now
                      </StatusBadge>
                    ) : (
                      <span data-testid="browser-idle">Not in use right now.</span>
                    )}
                  </span>
                </>
              }
              control={
                <div className="flex items-center gap-2">
                  <span data-testid="browser-switch-state" className="text-ui-sm text-foreground-muted">
                    {data.killSwitch || !data.chromeInstalled || !data.available ? `${data.enabled ? "On" : "Off"}, not in effect` : data.enabled ? "On" : "Off"}
                  </span>
                  <Switch
                    aria-label="Let agents use a browser in this workspace"
                    data-testid="browser-switch"
                    checked={data.enabled}
                    disabled={data.killSwitch || switchPending}
                    aria-busy={switchPending}
                    onCheckedChange={(next) => {
                      setSwitchError(null);
                      change({ enabled: next }).catch((e: unknown) => setSwitchError(messageOf(e)));
                    }}
                  />
                </div>
              }
            >
              {switchError && (
                <p role="alert" data-testid="browser-switch-error" className="text-ui-sm text-destructive">
                  {switchError}
                </p>
              )}
            </SettingsRow>
            <div className="flex flex-col gap-2 px-4 py-3" data-testid="browser-residual">
              <p className="text-ui-base text-foreground">What this does not protect against</p>
              <div className="flex flex-col gap-2 text-ui-sm text-foreground-muted">
                <p data-testid="browser-residual-local">{BROWSER_RESIDUAL_LOCAL}</p>
                <p data-testid="browser-residual-shell">{BROWSER_RESIDUAL_SHELL}</p>
                <p data-testid="browser-residual-windows">{BROWSER_RESIDUAL_WINDOWS}</p>
              </div>
            </div>
          </SettingsGroup>

          <SettingsGroup
            title="Allowed hosts"
            description="Besides this machine's own localhost, an agent's browser may reach only the exact hosts you add here. Changing the list ends a running browser session."
            testId="browser-allowlist"
          >
            <div className="px-4 py-3">
              <BrowserAllowlistEditor
                key={workspace.ID /* a draft typed for one workspace must never reach another */}
                hosts={data.allowHosts}
                note={data.note}
                onAdd={(host) => change({ allowAdd: [host] })}
                onRemove={(host) => change({ allowRemove: [host] })}
              />
            </div>
          </SettingsGroup>

          <SettingsGroup title="Profile" description="Each session starts with a fresh profile that is removed when the session ends." testId="browser-profile">
            {/* A container query on the card, not the viewport: beside each other only where the text keeps a readable column. */}
            <div className="@container/profile" data-testid="browser-profile-row">
              <div className="flex flex-col gap-3 px-4 py-3 @2xl/profile:flex-row @2xl/profile:items-center @2xl/profile:justify-between">
                <div className="min-w-0 @2xl/profile:flex-1" data-testid="browser-profile-text">
                  <p className="text-ui-base text-foreground">Ephemeral profile</p>
                  <p className="text-ui-sm text-foreground-muted">
                    Cookies and logins do not outlive a session. A persistent profile is not available yet.
                  </p>
                </div>
                <div className="flex flex-wrap items-center gap-2 @2xl/profile:shrink-0">
                  {live && (
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      data-testid="browser-end-session"
                      disabled={clear.isPending}
                      onClick={() => {
                        clear.reset();
                        clear.mutate();
                      }}
                    >
                      <Icons.close aria-hidden className="size-3.5" />
                      End session (interrupts the agent)
                    </Button>
                  )}
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    data-testid="browser-clear"
                    onClick={() => {
                      clear.reset();
                      setClearing(true);
                    }}
                  >
                    Clear browser data
                  </Button>
                </div>
              </div>
              {clear.error && (
                <p role="alert" data-testid="browser-clear-error" className="px-4 pb-3 text-ui-sm text-destructive">
                  {messageOf(clear.error)}
                </p>
              )}
            </div>
          </SettingsGroup>

          <SettingsGroup
            title="Which agents get the browser"
            description="Decided by sMind per agent, not by the agent: an agent without the browser is told so, with the reason."
            testId="browser-reach"
          >
            <div className="px-4 py-3">
              <BrowserReachList providers={data.providers} />
            </div>
          </SettingsGroup>
        </>
      )}

      <ConfirmDialog
        open={clearing}
        onOpenChange={(open) => {
          setClearing(open);
          if (!open) clear.reset();
        }}
        title="Clear this workspace's browser data?"
        description="This ends the browser session now, if there is one, and removes its profile: cookies, logins and cache. It does not change the switch or the allowed hosts."
        confirmLabel="Clear browser data"
        pendingLabel="Clearing…"
        pending={clear.isPending}
        error={clear.error ? messageOf(clear.error) : null}
        testId="browser-clear-dialog"
        onConfirm={() => clear.mutate(undefined, { onSuccess: () => setClearing(false) })}
      />
    </div>
  );
}

registerSettingsSection({
  id: "browser",
  label: "Browser",
  groupLabel: "Safety & usage",
  description: "Let agents open and read pages on this machine, per workspace.",
  order: 192,
  icon: <Icons.web aria-hidden className="size-3.5" />,
  render: () => <BrowserSection />,
});
