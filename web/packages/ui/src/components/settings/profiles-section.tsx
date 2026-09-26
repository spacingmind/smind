import { useEffect, useState, type FormEvent } from "react";
import { MoreHorizontal, Star } from "lucide-react";

import { registerSettingsSection, type SettingsSectionContext } from "@/components/settings/settings-registry";
import { Button } from "@/components/ui/button";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { StatusDot as SharedStatusDot } from "@/components/ui/status-dot";
import { useDefaultAgentId } from "@/hooks/use-default-agent";
import { approvalPolicies, approvalPolicyLabel } from "@/lib/approval-policies";
import { thinkingLevelLabel } from "@/lib/thinking-levels";
import type { AgentProfile, ApprovalPolicy, ProviderInfo, ProviderListResult, ProviderTestResult, ThinkingLevel } from "@/lib/types";
import type { WsClient } from "@/lib/ws-client";

/** Used until provider.list answers (and kept if it fails), same fallback composer.tsx uses -- this form must never be unusable because one fetch lost. */
const FALLBACK_PROVIDERS: ProviderInfo[] = [{ id: "claude-native" }, { id: "glm" }];

// "Composer default" (the empty id -- inherit whatever the composer is
// set to) is this form's own entry; the three real tiers come from the
// shared vocabulary in lib/approval-policies.ts. Claude-native here
// because full-access's label is provider-specific and this form shows
// one list for whichever provider is selected above -- a mismatch only
// ever cosmetic.
const APPROVAL_POLICIES = [
  { id: "", label: "Composer default" },
  ...approvalPolicies("claude-native"),
];

const THINKING_LEVELS = [
  { id: "", label: "Composer default" },
  { id: "off", label: "Off" },
  { id: "standard", label: "Standard" },
  { id: "extended", label: "Extended" },
];

/** The add/edit form's field state -- shared by the bottom "New agent" form and each row's own inline edit instance. */
interface ProfileFormState {
  name: string;
  provider: string;
  approvalPolicy: string;
  thinkingLevel: string;
  notes: string;
}

const EMPTY_FORM: ProfileFormState = { name: "", provider: "claude-native", approvalPolicy: "", thinkingLevel: "", notes: "" };

function formFromProfile(p: AgentProfile): ProfileFormState {
  return { name: p.Name, provider: p.Provider, approvalPolicy: p.ApprovalPolicy, thinkingLevel: p.ThinkingLevel, notes: p.Notes };
}

/**
 * Settings -> Profiles (ADR-0014 / docs/plans/active/agent-profiles.md):
 * create, edit, and delete named provider/approvalPolicy/thinkingLevel
 * bundles, stored in the daemon so every client (web, desktop, mobile,
 * CLI) shares the same list. No `model` field -- dropped from v1 entirely,
 * see the ADR's "Deferred: model" section.
 *
 * Live-updates via profile.created/updated/deleted (ctx.events, ADR 0009's
 * shape) in addition to the initial profile.list fetch, so a profile added
 * from a second tab or the CLI appears here without a manual reload.
 *
 * Edit opens inline in the row (run-config IA plan), not a dialog and not
 * a shared bottom form repurposed into "edit mode" -- `editingId` just
 * picks which row renders its own <ProfileForm> instance below its
 * summary line, so the bottom section stays a plain, always-in-"new"-mode
 * add form.
 */
function ProfilesSection({ client, events }: SettingsSectionContext) {
  const [profiles, setProfiles] = useState<AgentProfile[] | null>(null);
  const [providers, setProviders] = useState<ProviderInfo[]>(FALLBACK_PROVIDERS);
  const [newForm, setNewForm] = useState<ProfileFormState>(EMPTY_FORM);
  const [newPending, setNewPending] = useState(false);
  const [newError, setNewError] = useState<string | null>(null);
  const [editingId, setEditingId] = useState<number | null>(null);
  const [editForm, setEditForm] = useState<ProfileFormState>(EMPTY_FORM);
  const [editPending, setEditPending] = useState(false);
  const [editError, setEditError] = useState<string | null>(null);
  // The run-config IA plan's ★ default agent: which profile a brand-new
  // task's composer toolbar starts from (run-config-toolbar.tsx reads the
  // same stored id). Replaces General's old "Defaults for new tasks"
  // control -- see the plan's Decisions section.
  const { defaultAgentId, setDefaultAgentId } = useDefaultAgentId();

  useEffect(() => {
    if (!client) return;
    let cancelled = false;
    client
      .call<AgentProfile[]>("profile.list")
      .then((list) => {
        if (!cancelled) setProfiles(list ?? []);
      })
      .catch((err) => setNewError(err instanceof Error ? err.message : String(err)));
    return () => {
      cancelled = true;
    };
  }, [client]);

  useEffect(() => {
    if (!client) return;
    let cancelled = false;
    client
      .call<ProviderListResult>("provider.list")
      .then((result) => {
        if (!cancelled && result.providers.length > 0) setProviders(result.providers);
      })
      .catch((err) => console.error("provider.list failed, using fallback provider list", err));
    return () => {
      cancelled = true;
    };
  }, [client]);

  useEffect(() => {
    if (!events) return;
    const offCreated = events.subscribe("profile.created", (payload) => {
      const p = (payload as { profile?: AgentProfile } | undefined)?.profile;
      if (!p) return;
      setProfiles((prev) => (prev ? upsertProfile(prev, p) : [p]));
    });
    const offUpdated = events.subscribe("profile.updated", (payload) => {
      const p = (payload as { profile?: AgentProfile } | undefined)?.profile;
      if (!p) return;
      setProfiles((prev) => (prev ? upsertProfile(prev, p) : [p]));
    });
    const offDeleted = events.subscribe("profile.deleted", (payload) => {
      const id = (payload as { id?: number } | undefined)?.id;
      if (id === undefined) return;
      setProfiles((prev) => (prev ? prev.filter((p) => p.ID !== id) : prev));
    });
    return () => {
      offCreated();
      offUpdated();
      offDeleted();
    };
  }, [events]);

  function startEdit(p: AgentProfile) {
    setEditingId(p.ID);
    setEditForm(formFromProfile(p));
    setEditError(null);
  }

  function cancelEdit() {
    setEditingId(null);
    setEditError(null);
  }

  async function handleCreate(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    if (!client || !newForm.name.trim()) return;
    setNewPending(true);
    setNewError(null);
    try {
      const saved = await client.call<AgentProfile>("profile.create", {
        name: newForm.name.trim(),
        provider: newForm.provider,
        approvalPolicy: newForm.approvalPolicy,
        thinkingLevel: newForm.thinkingLevel,
        notes: newForm.notes,
      });
      // Upsert from the RPC's own returned profile rather than waiting for
      // profile.created: the mutation's own tab must show the result
      // immediately even when events is null (not yet connected) or when
      // that event arrives after this promise already resolved --
      // upsertProfile dedupes by ID, so a later event is a harmless no-op.
      setProfiles((prev) => (prev ? upsertProfile(prev, saved) : [saved]));
      setNewForm(EMPTY_FORM);
    } catch (err) {
      setNewError(err instanceof Error ? err.message : String(err));
    } finally {
      setNewPending(false);
    }
  }

  async function handleSaveEdit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    if (!client || editingId === null || !editForm.name.trim()) return;
    setEditPending(true);
    setEditError(null);
    try {
      const saved = await client.call<AgentProfile>("profile.update", {
        id: editingId,
        name: editForm.name.trim(),
        provider: editForm.provider,
        approvalPolicy: editForm.approvalPolicy,
        thinkingLevel: editForm.thinkingLevel,
        notes: editForm.notes,
      });
      setProfiles((prev) => (prev ? upsertProfile(prev, saved) : [saved]));
      cancelEdit();
    } catch (err) {
      setEditError(err instanceof Error ? err.message : String(err));
    } finally {
      setEditPending(false);
    }
  }

  async function handleDelete(id: number) {
    if (!client) return;
    try {
      await client.call("profile.delete", { id });
      setProfiles((prev) => (prev ? prev.filter((p) => p.ID !== id) : prev));
      if (editingId === id) cancelEdit();
      // A deleted default agent can't stay ★ -- a stale id would silently
      // stop seeding new tasks (run-config-toolbar.tsx's lookup just fails
      // to find it), which reads as "the default stopped working" rather
      // than the honest "there is no default agent anymore".
      if (defaultAgentId === String(id)) setDefaultAgentId(null);
    } catch (err) {
      console.error("profile.delete failed", err);
    }
  }

  return (
    <div className="flex flex-col gap-6" data-testid="settings-section-profiles">
      <section className="flex flex-col gap-2">
        <h3 className="text-ui-base font-medium text-foreground">Agents</h3>
        {profiles === null ? (
          <p className="text-ui-base text-muted-foreground">Loading…</p>
        ) : profiles.length === 0 ? (
          <p className="text-ui-base text-muted-foreground" data-testid="profiles-empty-state">
            No agents yet — create one below to reuse a provider/approval/thinking bundle from the composer's
            Agents picker.
          </p>
        ) : (
          <ul className="flex flex-col gap-1">
            {profiles.map((p) => {
              const isDefault = defaultAgentId === String(p.ID);
              const editing = editingId === p.ID;
              return (
                <li key={p.ID} data-testid={`profile-row-${p.ID}`} className="flex flex-col gap-2 rounded-md border px-2 py-1.5">
                  <div className="flex items-center justify-between gap-2">
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon-xs"
                      className="shrink-0 text-foreground-subtlest data-[default=true]:text-warning"
                      data-default={isDefault}
                      data-testid={`profile-default-${p.ID}`}
                      aria-pressed={isDefault}
                      aria-label={isDefault ? `${p.Name} is the default agent` : `Set ${p.Name} as the default agent`}
                      title={isDefault ? "Default agent for new tasks — click to clear" : "Set as default agent for new tasks"}
                      onClick={() => setDefaultAgentId(isDefault ? null : String(p.ID))}
                    >
                      <Star aria-hidden fill={isDefault ? "currentColor" : "none"} />
                    </Button>
                    <div className="flex min-w-0 flex-1 flex-col">
                      <span className="truncate text-ui-base font-medium text-foreground">{p.Name}</span>
                      <span className="truncate text-ui-sm text-muted-foreground">{profileMetaLine(p, providers)}</span>
                      {p.Notes && <span className="truncate text-ui-sm text-foreground-subtlest">{p.Notes}</span>}
                    </div>
                    <DropdownMenu>
                      <DropdownMenuTrigger asChild>
                        <Button
                          type="button"
                          variant="ghost"
                          size="icon-xs"
                          aria-label={`${p.Name} actions`}
                          data-testid={`profile-menu-${p.ID}`}
                          className="shrink-0"
                        >
                          <MoreHorizontal aria-hidden />
                        </Button>
                      </DropdownMenuTrigger>
                      <DropdownMenuContent align="end">
                        <DropdownMenuItem
                          data-testid={`profile-edit-${p.ID}`}
                          onSelect={() => (editing ? cancelEdit() : startEdit(p))}
                        >
                          {editing ? "Close" : "Edit"}
                        </DropdownMenuItem>
                        <DropdownMenuItem data-testid={`profile-delete-${p.ID}`} onSelect={() => void handleDelete(p.ID)}>
                          Delete
                        </DropdownMenuItem>
                      </DropdownMenuContent>
                    </DropdownMenu>
                  </div>
                  {editing && (
                    <ProfileForm
                      idPrefix={`profile-edit-form-${p.ID}`}
                      form={editForm}
                      setForm={setEditForm}
                      providers={providers}
                      client={client}
                      showProviderHealth
                      onSubmit={handleSaveEdit}
                      onCancel={cancelEdit}
                      pending={editPending}
                      error={editError}
                      submitLabel={editPending ? "Saving…" : "Save"}
                    />
                  )}
                </li>
              );
            })}
          </ul>
        )}
      </section>

      <section className="flex flex-col gap-2 border-t pt-4">
        <p className="text-ui-base font-medium">New agent</p>
        <ProfileForm
          idPrefix="profile-form"
          form={newForm}
          setForm={setNewForm}
          providers={providers}
          client={client}
          onSubmit={handleCreate}
          pending={newPending}
          error={newError}
          submitLabel={newPending ? "Saving…" : "Add agent"}
        />
      </section>
    </div>
  );
}

/**
 * The Name/Provider/Approval/Thinking/Notes fields, reused by the bottom
 * "New agent" form and each row's inline edit (run-config IA's "Edit
 * opens inline in the row" AC) -- `idPrefix` keeps their data-testids
 * distinct since both kinds of form can be mounted at once.
 */
function ProfileForm({
  idPrefix,
  form,
  setForm,
  providers,
  client,
  showProviderHealth,
  onSubmit,
  onCancel,
  pending,
  error,
  submitLabel,
}: {
  idPrefix: string;
  form: ProfileFormState;
  setForm: (updater: (f: ProfileFormState) => ProfileFormState) => void;
  providers: ProviderInfo[];
  client: WsClient | null;
  /** Only for an existing agent's inline edit -- a not-yet-created profile has no backing account to check yet. */
  showProviderHealth?: boolean;
  onSubmit: (e: FormEvent<HTMLFormElement>) => void;
  onCancel?: () => void;
  pending: boolean;
  error: string | null;
  submitLabel: string;
}) {
  return (
    <form className="flex flex-col gap-2" onSubmit={onSubmit}>
      <Input
        aria-label="Agent name"
        data-testid={`${idPrefix}-name`}
        placeholder="Name (e.g. Quick Fixes)"
        value={form.name}
        onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))}
      />
      <div className="grid grid-cols-2 gap-2">
        <div className="flex items-center gap-1.5">
          <Select value={form.provider} onValueChange={(value) => setForm((f) => ({ ...f, provider: value }))}>
            <SelectTrigger aria-label="Provider" data-testid={`${idPrefix}-provider`} className="flex-1">
              <SelectValue placeholder="Select provider" />
            </SelectTrigger>
            <SelectContent>
              {providers.map((p) => (
                <SelectItem key={p.id} value={p.id}>
                  {p.label ?? p.id}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          {showProviderHealth && (
            <ProviderHealthDot client={client} provider={form.provider} providers={providers} testId={`${idPrefix}-provider-health`} />
          )}
        </div>
        <Select value={form.approvalPolicy} onValueChange={(value) => setForm((f) => ({ ...f, approvalPolicy: value }))}>
          <SelectTrigger aria-label="Approval policy" data-testid={`${idPrefix}-approval-policy`}>
            <SelectValue placeholder="Approval policy" />
          </SelectTrigger>
          <SelectContent>
            {APPROVAL_POLICIES.map((o) => (
              <SelectItem key={o.id} value={o.id} title={o.id ? approvalPolicies("claude-native").find((a) => a.id === o.id)?.help : undefined}>
                {o.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      {form.provider === "claude-native" && (
        <Select value={form.thinkingLevel} onValueChange={(value) => setForm((f) => ({ ...f, thinkingLevel: value }))}>
          <SelectTrigger aria-label="Thinking level" data-testid={`${idPrefix}-thinking-level`} className="w-full">
            <SelectValue placeholder="Thinking level" />
          </SelectTrigger>
          <SelectContent>
            {THINKING_LEVELS.map((o) => (
              <SelectItem key={o.id} value={o.id}>
                {o.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      )}
      <textarea
        aria-label="Notes"
        data-testid={`${idPrefix}-notes`}
        className="h-16 w-full resize-none rounded-lg border border-input bg-transparent px-2.5 py-1 text-ui-base outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
        placeholder="Notes (optional) — when to use this agent."
        value={form.notes}
        onChange={(e) => setForm((f) => ({ ...f, notes: e.target.value }))}
      />
      {error && (
        <p role="alert" data-testid={`${idPrefix}-error`} className="text-ui-base text-destructive">
          {error}
        </p>
      )}
      <div className="flex justify-end gap-2">
        {onCancel && (
          <Button type="button" variant="ghost" size="sm" data-testid={`${idPrefix}-cancel`} onClick={onCancel}>
            Cancel
          </Button>
        )}
        <Button type="submit" size="sm" disabled={pending || !form.name.trim()} data-testid={`${idPrefix}-submit`}>
          {submitLabel}
        </Button>
      </div>
    </form>
  );
}

/**
 * The inline edit form's provider health dot (run-config IA AC: "Provider
 * (with a health indicator for its backing account, reusing whatever the
 * accounts dialog already computes for account health)") -- same
 * provider.test RPC and the same ok/failed/untested -> StatusDot mapping
 * as accounts-dialog.tsx's own StatusDot, run independently here rather
 * than sharing that dialog's transient testResults state (this section
 * already fetches its own copies of profile.list/provider.list for the
 * same reason -- see ProfilesSection's doc comment).
 *
 * provider.test's `provider` param is accounts-vocabulary
 * (internal/accounts' anthropic/openai/... ids) for a credential-backed
 * provider, but taskrunner-vocabulary (ProviderInfo.id) for a cli-kind one
 * -- accountHealthTestKey resolves AgentProfile.Provider (always
 * taskrunner-vocab) to whichever id provider.test actually expects, per
 * ProviderInfo.accountProvider's own doc comment in lib/types.ts.
 */
function accountHealthTestKey(providerId: string, providers: ProviderInfo[]): string {
  const info = providers.find((p) => p.id === providerId);
  if (!info || info.kind === "cli") return providerId;
  return info.accountProvider ?? providerId;
}

function ProviderHealthDot({
  client,
  provider,
  providers,
  testId,
}: {
  client: WsClient | null;
  provider: string;
  providers: ProviderInfo[];
  testId: string;
}) {
  const testKey = accountHealthTestKey(provider, providers);
  const [result, setResult] = useState<ProviderTestResult | undefined>(undefined);

  useEffect(() => {
    setResult(undefined);
    if (!client || !testKey) return;
    let cancelled = false;
    client
      .call<ProviderTestResult>("provider.test", { provider: testKey })
      .then((r) => {
        if (!cancelled) setResult(r);
      })
      .catch((err) => {
        if (!cancelled) setResult({ ok: false, detail: err instanceof Error ? err.message : String(err) });
      });
    return () => {
      cancelled = true;
    };
  }, [client, testKey]);

  const state = result === undefined ? "unknown" : result.ok ? "ok" : "failed";
  const status = state === "ok" ? "success" : state === "failed" ? "danger" : "neutral";
  const label = state === "ok" ? "Connection ok" : state === "failed" ? "Connection failed" : "Not tested yet";
  return (
    <SharedStatusDot
      status={status}
      className="size-2 shrink-0"
      title={label}
      aria-label={label}
      data-testid={testId}
      data-status={state}
    />
  );
}

function upsertProfile(list: AgentProfile[], p: AgentProfile): AgentProfile[] {
  const index = list.findIndex((existing) => existing.ID === p.ID);
  if (index === -1) return [...list, p];
  const next = list.slice();
  next[index] = p;
  return next;
}

function providerLabel(providers: ProviderInfo[], id: string): string {
  return providers.find((p) => p.id === id)?.label ?? id;
}

/** The card's one metadata line ("<provider> · <approval> · <thinking>", run-config IA): the shared approval-policies.ts/thinking-levels.ts vocabulary, not the raw stored strings -- "auto-safe" reads as "Auto-safe", not the wire id. Thinking is omitted for a non-Claude provider, same rule as the composer's own Thinking control. */
function profileMetaLine(p: AgentProfile, providers: ProviderInfo[]): string {
  const parts = [providerLabel(providers, p.Provider)];
  if (p.ApprovalPolicy) parts.push(approvalPolicyLabel(p.ApprovalPolicy as ApprovalPolicy));
  if (p.Provider === "claude-native" && p.ThinkingLevel) parts.push(thinkingLevelLabel(p.ThinkingLevel as ThinkingLevel));
  return parts.join(" · ");
}

registerSettingsSection({
  id: "agents",
  label: "Agents",
  groupLabel: "Agents & providers",
  order: 175,
  render: (ctx) => <ProfilesSection {...ctx} />,
});
