import { useEffect, useMemo, useState, type FormEvent } from "react";

import { Checkbox } from "@/components/ui/checkbox";
import { ChangeList, ConsentSummary, linkWarningText } from "@/components/plugins/consent-summary";
import { OwnerBadge } from "@/components/plugins/owner-badge";
import { registerSettingsSection, type SettingsSectionContext } from "@/components/settings/settings-registry";
import { SettingsEmptyCard } from "@/components/settings/settings-layout";
import { SettingsSectionTitle } from "@/components/settings/settings-title";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { FormError } from "@/components/ui/form-error";
import { Input } from "@/components/ui/input";
import { LoadingRows } from "@/components/ui/loading-rows";
import { StatusBadge } from "@/components/ui/status-badge";
import { useConfirmAction } from "@/hooks/use-confirm-action";
import { useLastDefined } from "@/hooks/use-last-defined";
import { usePluginActions, usePlugins } from "@/hooks/use-plugins";
import { Icons } from "@/lib/icons";
import { diffConsent } from "@/lib/plugin-diff";
import { PLUGINS_SECTION_ID, subscribePluginFocus, takePluginFocus } from "@/lib/plugin-focus";
import type { PluginInfo } from "@/lib/plugin-types";
import { formatRelativeTime } from "@/lib/relative-time";

/** The daemon's refusal of a human-local action (a relay session, or a remote login with plugins.allowRemoteManage off). */
function isForbidden(err: unknown): boolean {
  return (err as { code?: string } | null)?.code === "forbidden";
}

function messageOf(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

/** Why Enable/Disable/... are not offered right now, or null. The switch is a file edit only: this page has no control for it. */
function pluginsOffReason(globalEnabled: boolean): string | null {
  return globalEnabled ? null : "Plugins are off";
}

const SOURCE_PLACEHOLDER = "path, git+https://…@<sha> or archive#sha256=<hash>";

/**
 * Settings -> Plugins (ADR-0042, AC15): the global switch's state (read-only here: only a human editing config.yaml
 * turns plugins on), every installed plugin with its status and last error, the consent summary before Enable, and
 * Enable / Disable / Reload / Update / Remove / Install. Every mutation is human + local only: the daemon may answer
 * `forbidden` (a relay session, or a remote login with plugins.allowRemoteManage off); its message is shown inline
 * and the page goes read-only without retrying. Live through `plugin.updated` (the event bridge refetches the list).
 */
function PluginsSection(_props: SettingsSectionContext) {
  const query = usePlugins();
  const actions = usePluginActions();
  const listing = query.data;
  const plugins = listing?.plugins ?? null;
  const globalEnabled = listing?.globalEnabled ?? false;

  const [readOnlyReason, setReadOnlyReason] = useState<string | null>(null);
  const [expanded, setExpanded] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [rowError, setRowError] = useState<{ id: string; message: string } | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const locked = !globalEnabled || readOnlyReason !== null;

  // An owner badge elsewhere names a plugin: expand its detail (on mount, or while this section is showing).
  useEffect(() => {
    const take = () => {
      const id = takePluginFocus();
      if (id) setExpanded(id);
    };
    take();
    return subscribePluginFocus(take);
  }, []);

  /** Notes a refusal (the page goes read-only with the daemon's own message) and returns the message. */
  function fail(err: unknown): string {
    if (isForbidden(err)) setReadOnlyReason(messageOf(err));
    return messageOf(err);
  }

  async function quick(plugin: PluginInfo, run: () => Promise<unknown>) {
    setBusy(plugin.id);
    setRowError(null);
    setNotice(null);
    try {
      await run();
    } catch (err) {
      setRowError({ id: plugin.id, message: fail(err) });
    } finally {
      setBusy(null);
    }
  }

  // --- install form
  const [installOpen, setInstallOpen] = useState(false);
  const [source, setSource] = useState("");
  const [installing, setInstalling] = useState(false);
  const [installError, setInstallError] = useState<string | null>(null);

  async function handleInstall(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const spec = source.trim();
    if (!spec || installing) return;
    setInstalling(true);
    setInstallError(null);
    try {
      const info = await actions.install(spec);
      setInstallOpen(false);
      setSource("");
      setExpanded(info.id);
      setNotice(`Installed ${info.name}. It is disabled until you enable it: review what it does below.`);
    } catch (err) {
      setInstallError(fail(err));
    } finally {
      setInstalling(false);
    }
  }

  // --- enable confirmation (the consent summary, and the fingerprint the daemon needs as the confirmation)
  const [enableId, setEnableId] = useState<string | null>(null);
  const enableAction = useConfirmAction(enableId !== null);
  const enableTarget = useLastDefined(plugins?.find((p) => p.id === enableId) ?? null);

  // --- remove
  const [removeId, setRemoveId] = useState<string | null>(null);
  const [keepData, setKeepData] = useState(false);
  const removeAction = useConfirmAction(removeId !== null);
  const removeTarget = useLastDefined(plugins?.find((p) => p.id === removeId) ?? null);

  // --- apply a pending update
  const [applyId, setApplyId] = useState<string | null>(null);
  const applyAction = useConfirmAction(applyId !== null);
  const applyTarget = useLastDefined(plugins?.find((p) => p.id === applyId) ?? null);

  // --- check for an update
  const [checkId, setCheckId] = useState<string | null>(null);
  const [checkSource, setCheckSource] = useState("");
  const checkAction = useConfirmAction(checkId !== null);
  const checkTarget = useLastDefined(plugins?.find((p) => p.id === checkId) ?? null);

  const installButton = (
    <Button
      type="button"
      size="sm"
      data-testid="plugin-install-button"
      disabled={locked}
      title={readOnlyReason ?? pluginsOffReason(globalEnabled) ?? undefined}
      onClick={() => {
        setInstallOpen((open) => !open);
        setInstallError(null);
      }}
    >
      <Icons.add aria-hidden className="size-3.5" /> Install plugin
    </Button>
  );

  return (
    <div className="flex flex-col gap-6" data-testid="settings-section-plugins">
      <section className="flex flex-col gap-2">
        <SettingsSectionTitle title="Plugins" actions={installButton} />

        {listing && !globalEnabled && (
          <Alert
            variant="info"
            testId="plugins-off"
            title="Plugins are off"
            description={
              <>
                Set <code className="font-mono text-ui-sm">plugins.enabled: true</code> in config.yaml to turn them on; it applies without a
                restart. Until then no plugin adds anything, and this page is read-only.
              </>
            }
          />
        )}
        {readOnlyReason && (
          <Alert variant="info" testId="plugins-read-only" title="Plugins can't be managed from this connection" description={readOnlyReason} />
        )}
        {notice && <Alert variant="success" testId="plugins-notice" description={notice} />}

        {installOpen && (
          <form className="flex flex-col gap-2 rounded-xl border bg-card p-3" data-testid="plugin-install-form" onSubmit={handleInstall}>
            <Input
              aria-label="Plugin source"
              data-testid="plugin-install-source"
              placeholder={SOURCE_PLACEHOLDER}
              value={source}
              autoComplete="off"
              spellCheck={false}
              onChange={(e) => setSource(e.target.value)}
            />
            <p className="text-ui-sm text-foreground-subtlest">
              A plugin directory, a Git URL pinned to a full commit, or an archive pinned to its sha256. It stays disabled until you enable it.
            </p>
            <FormError message={installError} testId="plugin-install-error" />
            <div className="flex justify-end gap-2">
              <Button type="button" variant="ghost" size="sm" data-testid="plugin-install-cancel" onClick={() => setInstallOpen(false)}>
                Cancel
              </Button>
              <Button type="submit" size="sm" data-testid="plugin-install-submit" disabled={!source.trim() || installing}>
                {installing ? "Installing…" : "Install"}
              </Button>
            </div>
          </form>
        )}

        {plugins === null ? (
          query.isError ? (
            <div className="flex items-center gap-2">
              <Alert variant="error" testId="plugins-load-error" description={messageOf(query.error)} />
              <Button type="button" variant="ghost" size="sm" data-testid="plugins-load-retry" onClick={() => void query.refetch()}>
                Retry
              </Button>
            </div>
          ) : (
            <LoadingRows />
          )
        ) : plugins.length === 0 ? (
          <SettingsEmptyCard
            testId="plugins-empty-state"
            title="No plugins installed"
            description={
              <>
                Install one from a directory with <code className="font-mono text-ui-sm whitespace-nowrap">smind plugin install &lt;path&gt;</code>
              </>
            }
          />
        ) : (
          <ul className="flex flex-col gap-1 rounded-xl border bg-card p-3" data-testid="plugins-list">
            {plugins.map((p) => (
              <PluginRow
                key={p.id}
                plugin={p}
                globalEnabled={globalEnabled}
                locked={locked}
                lockedReason={readOnlyReason ?? pluginsOffReason(globalEnabled)}
                expanded={expanded === p.id}
                busy={busy === p.id}
                error={rowError?.id === p.id ? rowError.message : null}
                onToggle={() => setExpanded((cur) => (cur === p.id ? null : p.id))}
                onEnable={() => {
                  enableAction.reset();
                  setEnableId(p.id);
                }}
                onDisable={() => void quick(p, () => actions.disable(p.id))}
                onReload={() => void quick(p, () => actions.reload(p.id))}
                onRemove={() => {
                  removeAction.reset();
                  setKeepData(false);
                  setRemoveId(p.id);
                }}
                onReviewUpdate={() => {
                  applyAction.reset();
                  setApplyId(p.id);
                }}
                onCheckUpdate={() => {
                  checkAction.reset();
                  setCheckSource("");
                  setCheckId(p.id);
                }}
              />
            ))}
          </ul>
        )}
      </section>

      <ConfirmDialog
        open={enableId !== null}
        onOpenChange={(open) => {
          if (open) return;
          setEnableId(null);
          enableAction.reset();
        }}
        title={`Enable ${enableTarget?.name ?? ""}?`}
        description={<span>Review what this plugin will do. Nothing is loaded until you confirm.</span>}
        confirmLabel="Enable plugin"
        pendingLabel="Enabling…"
        destructive={false}
        pending={enableAction.pending}
        error={enableAction.error}
        testId="plugin-enable-dialog"
        className="max-h-[85vh] overflow-y-auto sm:max-w-xl"
        onConfirm={() => {
          if (!enableTarget) return;
          const { id, consent } = enableTarget;
          // The confirmation is the fingerprint of the summary shown here: the daemon refuses an enable without it.
          void enableAction
            .run(async () => {
              try {
                await actions.enable(id, consent.fingerprint);
              } catch (err) {
                fail(err);
                throw err;
              }
            })
            .then((done) => {
              if (done) setEnableId(null);
            });
        }}
      >
        {enableTarget && <ConsentSummary consent={enableTarget.consent} testId="plugin-enable-consent" />}
      </ConfirmDialog>

      <ConfirmDialog
        open={removeId !== null}
        onOpenChange={(open) => {
          if (open) return;
          setRemoveId(null);
          removeAction.reset();
        }}
        title={`Remove ${removeTarget?.name ?? ""}?`}
        description={
          <span>
            Removes the plugin, the agents, MCP servers and skills it added, and its files.
            {removeTarget?.sourceKind === "link" && " The linked directory itself is never deleted."}
          </span>
        }
        confirmLabel="Remove plugin"
        pendingLabel="Removing…"
        pending={removeAction.pending}
        error={removeAction.error}
        testId="plugin-remove-dialog"
        onConfirm={() => {
          if (!removeTarget) return;
          const id = removeTarget.id;
          void removeAction
            .run(async () => {
              try {
                await actions.remove(id, keepData);
              } catch (err) {
                fail(err);
                throw err;
              }
            })
            .then((done) => {
              if (!done) return;
              setRemoveId(null);
              setExpanded((cur) => (cur === id ? null : cur));
            });
        }}
      >
        <label className="flex items-center gap-2 text-ui-sm text-foreground">
          <Checkbox data-testid="plugin-remove-keep-data" checked={keepData} onCheckedChange={(on) => setKeepData(on === true)} />
          Keep plugin data
        </label>
      </ConfirmDialog>

      <ConfirmDialog
        open={applyId !== null}
        onOpenChange={(open) => {
          if (open) return;
          setApplyId(null);
          applyAction.reset();
        }}
        title={`Apply update to ${applyTarget?.name ?? ""}?`}
        description={
          <span>
            {applyTarget?.pending
              ? `Version ${applyTarget.consent.version} → ${applyTarget.pending.consent.version}. It changes what the plugin declares; the current version keeps running until you apply.`
              : "This update is no longer pending."}
          </span>
        }
        confirmLabel="Apply update"
        pendingLabel="Applying…"
        destructive={false}
        pending={applyAction.pending}
        error={applyAction.error}
        testId="plugin-apply-dialog"
        className="max-h-[85vh] overflow-y-auto sm:max-w-xl"
        onConfirm={() => {
          if (!applyTarget) return;
          const id = applyTarget.id;
          void applyAction
            .run(async () => {
              try {
                await actions.update(id, { apply: true });
              } catch (err) {
                fail(err);
                throw err;
              }
            })
            .then((done) => {
              if (done) setApplyId(null);
            });
        }}
      >
        {applyTarget?.pending && (
          <div className="flex flex-col gap-3">
            <ChangeList changes={diffConsent(applyTarget.consent, applyTarget.pending.consent)} testId="plugin-apply-changes" />
            <ConsentSummary consent={applyTarget.pending.consent} testId="plugin-apply-consent" />
          </div>
        )}
      </ConfirmDialog>

      <ConfirmDialog
        open={checkId !== null}
        onOpenChange={(open) => {
          if (open) return;
          setCheckId(null);
          checkAction.reset();
        }}
        title={`Check ${checkTarget?.name ?? ""} for an update`}
        description={
          <span>
            {checkTarget?.sourceKind === "dir"
              ? "Re-reads the directory it was installed from. An update that changes what the plugin runs waits for your approval."
              : "Enter the new pinned source (a Git URL pinned to a full commit, or an archive pinned to its sha256)."}
          </span>
        }
        confirmLabel="Check for update"
        pendingLabel="Checking…"
        destructive={false}
        pending={checkAction.pending}
        error={checkAction.error}
        testId="plugin-check-dialog"
        onConfirm={() => {
          if (!checkTarget) return;
          const { id, sourceKind } = checkTarget;
          if (sourceKind !== "dir" && !checkSource.trim()) return;
          void checkAction
            .run(async () => {
              try {
                const result = await actions.update(id, { source: sourceKind === "dir" ? undefined : checkSource.trim() });
                setNotice(
                  result.applied
                    ? `Updated ${result.plugin.name} to ${result.plugin.version}.`
                    : result.pending
                      ? `An update to ${result.plugin.name} is waiting for your approval: open its details to review it.`
                      : `${result.plugin.name} is up to date.`,
                );
                if (result.pending) setExpanded(id);
              } catch (err) {
                fail(err);
                throw err;
              }
            })
            .then((done) => {
              if (done) setCheckId(null);
            });
        }}
      >
        {checkTarget && checkTarget.sourceKind !== "dir" && (
          <Input
            aria-label="New plugin source"
            data-testid="plugin-check-source"
            placeholder={SOURCE_PLACEHOLDER}
            value={checkSource}
            autoComplete="off"
            spellCheck={false}
            onChange={(e) => setCheckSource(e.target.value)}
          />
        )}
      </ConfirmDialog>
    </div>
  );
}

function StatusChips({ plugin, globalEnabled }: { plugin: PluginInfo; globalEnabled: boolean }) {
  const id = plugin.id;
  return (
    <>
      <StatusBadge
        status={plugin.enabled ? (globalEnabled ? "success" : "warning") : "neutral"}
        data-testid={`plugin-status-${id}`}
        data-state={plugin.enabled ? "enabled" : "disabled"}
      >
        {plugin.enabled ? (globalEnabled ? "Enabled" : "Enabled (plugins off)") : "Disabled"}
      </StatusBadge>
      {plugin.pending && (
        <StatusBadge status="warning" data-testid={`plugin-pending-${id}`}>
          Pending update
        </StatusBadge>
      )}
      {plugin.lastError && (
        <StatusBadge status="danger" data-testid={`plugin-error-chip-${id}`}>
          Error
        </StatusBadge>
      )}
    </>
  );
}

function PluginRow({
  plugin: p,
  globalEnabled,
  locked,
  lockedReason,
  expanded,
  busy,
  error,
  onToggle,
  onEnable,
  onDisable,
  onReload,
  onRemove,
  onReviewUpdate,
  onCheckUpdate,
}: {
  plugin: PluginInfo;
  globalEnabled: boolean;
  locked: boolean;
  lockedReason: string | null;
  expanded: boolean;
  busy: boolean;
  error: string | null;
  onToggle: () => void;
  onEnable: () => void;
  onDisable: () => void;
  onReload: () => void;
  onRemove: () => void;
  onReviewUpdate: () => void;
  onCheckUpdate: () => void;
}) {
  const linkWarning = p.sourceKind === "link" ? linkWarningText(p.consent) : null;
  const disabled = locked || busy;
  return (
    <li
      data-testid={`plugin-row-${p.id}`}
      data-expanded={expanded}
      className="flex flex-col gap-2 rounded-lg px-2 py-1.5 hover:bg-hover"
    >
      <div className="flex items-start gap-2">
        <Button
          type="button"
          variant="ghost"
          size="icon-xs"
          className="mt-0.5 shrink-0"
          data-testid={`plugin-toggle-${p.id}`}
          aria-expanded={expanded}
          aria-label={`${expanded ? "Hide" : "Show"} details of ${p.name}`}
          onClick={onToggle}
        >
          {expanded ? <Icons.chevronDown aria-hidden /> : <Icons.chevronRight aria-hidden />}
        </Button>
        <div className="flex min-w-0 flex-1 flex-col">
          <span className="flex flex-wrap items-center gap-x-2 gap-y-1">
            <span className="truncate text-ui-base font-medium text-foreground">{p.name}</span>
            <span className="shrink-0 text-ui-sm text-muted-foreground" data-testid={`plugin-version-${p.id}`}>
              {p.version}
            </span>
            <span
              data-testid={`plugin-source-${p.id}`}
              className="shrink-0 rounded-full border border-border px-1.5 text-ui-sm text-muted-foreground"
            >
              {p.sourceKind}
            </span>
            <StatusChips plugin={p} globalEnabled={globalEnabled} />
          </span>
          <span className="truncate text-ui-sm text-muted-foreground">{p.id}</span>
          {p.lastError && (
            <span className="line-clamp-2 break-words text-ui-sm text-destructive" title={p.lastError} data-testid={`plugin-error-${p.id}`}>
              {p.lastError}
            </span>
          )}
          {linkWarning && (
            <span className="flex items-start gap-1 text-ui-sm text-warning-text" data-testid={`plugin-link-warning-${p.id}`}>
              <Icons.warning aria-hidden className="mt-0.5 size-3.5 shrink-0" />
              <span>{linkWarning}</span>
            </span>
          )}
          {error && <FormError message={error} size="sm" testId={`plugin-row-error-${p.id}`} />}
        </div>
        <div className="flex shrink-0 items-center gap-1">
          {p.enabled ? (
            <Button type="button" variant="outline" size="sm" data-testid={`plugin-disable-${p.id}`} disabled={disabled} title={lockedReason ?? undefined} onClick={onDisable}>
              Disable
            </Button>
          ) : (
            <Button type="button" size="sm" data-testid={`plugin-enable-${p.id}`} disabled={disabled} title={lockedReason ?? undefined} onClick={onEnable}>
              Enable
            </Button>
          )}
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button type="button" variant="ghost" size="icon-xs" aria-label={`${p.name} actions`} data-testid={`plugin-menu-${p.id}`} className="shrink-0">
                <Icons.more aria-hidden />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem data-testid={`plugin-reload-${p.id}`} disabled={disabled} onSelect={onReload}>
                Reload
              </DropdownMenuItem>
              {p.pending ? (
                <DropdownMenuItem data-testid={`plugin-review-update-${p.id}`} disabled={disabled} onSelect={onReviewUpdate}>
                  Review update
                </DropdownMenuItem>
              ) : (
                <DropdownMenuItem
                  data-testid={`plugin-check-update-${p.id}`}
                  disabled={disabled || p.sourceKind === "link"}
                  title={p.sourceKind === "link" ? "A linked plugin is read live: use Reload" : undefined}
                  onSelect={onCheckUpdate}
                >
                  Check for update
                </DropdownMenuItem>
              )}
              <DropdownMenuItem data-testid={`plugin-remove-${p.id}`} disabled={disabled} onSelect={onRemove}>
                Remove
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </div>
      {expanded && <PluginDetail plugin={p} locked={locked} onReviewUpdate={onReviewUpdate} />}
    </li>
  );
}

function PluginDetail({ plugin: p, locked, onReviewUpdate }: { plugin: PluginInfo; locked: boolean; onReviewUpdate: () => void }) {
  const consent = p.consent;
  const changes = useMemo(() => (p.pending ? diffConsent(consent, p.pending.consent) : []), [consent, p.pending]);
  const mcp = consent.mcpServers ?? [];
  const skills = consent.skills ?? [];
  const profiles = consent.profiles ?? [];
  const contributes = mcp.length + skills.length + profiles.length > 0;
  return (
    <div className="ml-7 flex flex-col gap-3 border-t pt-2" data-testid={`plugin-detail-${p.id}`}>
      {consent.description && <p className="text-ui-sm text-foreground-muted">{consent.description}</p>}

      {p.pending && (
        <section className="flex flex-col gap-1.5 rounded-lg border border-warning/40 p-2" data-testid={`plugin-pending-card-${p.id}`}>
          <h4 className="text-ui-sm font-medium text-warning-text">
            Update available: {consent.version} → {p.pending.consent.version}
          </h4>
          <ChangeList changes={changes} testId={`plugin-pending-changes-${p.id}`} />
          <div>
            <Button type="button" size="sm" variant="outline" data-testid={`plugin-apply-${p.id}`} disabled={locked} onClick={onReviewUpdate}>
              Review and apply
            </Button>
          </div>
        </section>
      )}

      <section className="flex flex-col gap-1.5">
        <h4 className="text-ui-sm font-medium text-foreground">What it does</h4>
        <ConsentSummary consent={consent} testId={`plugin-consent-${p.id}`} />
      </section>

      <section className="flex flex-col gap-1" data-testid={`plugin-contributions-${p.id}`}>
        <h4 className="text-ui-sm font-medium text-foreground">Contributions</h4>
        {!contributes ? (
          <p className="text-ui-sm text-foreground-muted">Declares no MCP servers, skills or agents.</p>
        ) : (
          <ul className="flex flex-col gap-1">
            {mcp.map((s) => (
              <li key={`mcp-${s.name}`} className="flex items-center gap-2 text-ui-sm">
                <span className="w-16 shrink-0 text-foreground-muted">MCP server</span>
                <span className="min-w-0 break-all">{`${p.id}:${s.name}`}</span>
                <OwnerBadge owner={p.active ? p.id : null} link={false} />
              </li>
            ))}
            {skills.map((s) => (
              <li key={`skill-${s}`} className="flex items-center gap-2 text-ui-sm">
                <span className="w-16 shrink-0 text-foreground-muted">Skill</span>
                <span className="min-w-0 break-all">{`${p.id}:${s}`}</span>
                <OwnerBadge owner={p.active ? p.id : null} link={false} />
              </li>
            ))}
            {profiles.map((a) => (
              <li key={`profile-${a.name}`} className="flex items-center gap-2 text-ui-sm">
                <span className="w-16 shrink-0 text-foreground-muted">Agent</span>
                <span className="min-w-0 break-all">{`${p.id}:${a.name}`}</span>
                <OwnerBadge owner={p.active ? p.id : null} link={false} />
              </li>
            ))}
          </ul>
        )}
        {!p.active && contributes && (
          <p className="text-ui-sm text-foreground-subtlest">
            {p.enabled ? "Not loaded: plugins are off." : "Not loaded: enable the plugin to add these."}
          </p>
        )}
      </section>

      {(p.conflicts ?? []).length > 0 && (
        <section className="flex flex-col gap-1" data-testid={`plugin-conflicts-${p.id}`}>
          <h4 className="text-ui-sm font-medium text-foreground">Not applied</h4>
          <ul className="flex flex-col gap-0.5">
            {(p.conflicts ?? []).map((c) => (
              <li key={`${c.kind}-${c.name}`} className="text-ui-sm text-warning-text">
                {c.kind} <span className="font-medium">{c.name}</span>: {c.reason}
              </li>
            ))}
          </ul>
        </section>
      )}

      <section className="flex flex-col gap-1" data-testid={`plugin-logs-${p.id}`}>
        <h4 className="text-ui-sm font-medium text-foreground">Logs</h4>
        {p.lastError ? (
          <Alert variant="error" testId={`plugin-last-error-${p.id}`} title="Last error" description={p.lastError} />
        ) : (
          <p className="text-ui-sm text-foreground-muted">No errors recorded.</p>
        )}
        <p className="text-ui-sm text-foreground-muted">
          Installed {formatRelativeTime(p.installedAt)} · updated {formatRelativeTime(p.updatedAt)}
        </p>
        <p className="text-ui-sm text-foreground-subtlest">Process logs arrive with plugin processes.</p>
      </section>
    </div>
  );
}

registerSettingsSection({
  id: PLUGINS_SECTION_ID,
  label: "Plugins",
  groupLabel: "Agents & providers",
  description: "Installed plugins and what they add.",
  order: 179,
  icon: <Icons.plugin aria-hidden className="size-3.5" />,
  render: (ctx) => <PluginsSection {...ctx} />,
});
