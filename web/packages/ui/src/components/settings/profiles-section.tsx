import { useEffect, useState, type FormEvent } from "react";
import { MoreHorizontal, Plus, Star } from "lucide-react";

import { registerSettingsSection, type SettingsSectionContext } from "@/components/settings/settings-registry";
import { Button } from "@/components/ui/button";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { StatusDot as SharedStatusDot } from "@/components/ui/status-dot";
import { useDefaultAgentId } from "@/hooks/use-default-agent";
import { approvalPolicies, approvalPolicyLabel } from "@/lib/approval-policies";
import { accountHealthTestKey } from "@/lib/provider-health";
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

/** The add/edit form's field state -- one instance shared by whichever card is open (the "new agent" card or a row's own inline edit), since only one is ever open at a time. */
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

/** Which card is open: "new" for the add card (rendered at the top of the list), a profile id for that row's inline edit, or null for none. Only one is ever open -- opening one closes whichever was open before. */
type OpenCard = number | "new" | null;

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
 * Both "add" and "edit" open the *same* inline `<ProfileForm>` card --
 * add opens it at the top of the list (a header "+ New agent" button, the
 * app-sidebar.tsx "+ New workspace" empty-state pattern), edit opens it
 * inline within that row. `openCard` is the one piece of state that says
 * which (if either) is open; there's exactly one shared form state
 * because only one card is ever open at a time.
 */
function ProfilesSection({ client, events }: SettingsSectionContext) {
  const [profiles, setProfiles] = useState<AgentProfile[] | null>(null);
  const [providers, setProviders] = useState<ProviderInfo[]>(FALLBACK_PROVIDERS);
  const [openCard, setOpenCard] = useState<OpenCard>(null);
  const [form, setForm] = useState<ProfileFormState>(EMPTY_FORM);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);
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
      .catch((err) => setError(err instanceof Error ? err.message : String(err)));
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

  function openNew() {
    setOpenCard("new");
    setForm(EMPTY_FORM);
    setError(null);
  }

  function startEdit(p: AgentProfile) {
    setOpenCard(p.ID);
    setForm(formFromProfile(p));
    setError(null);
  }

  function closeCard() {
    setOpenCard(null);
    setError(null);
  }

  async function handleSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    if (!client || openCard === null || !form.name.trim()) return;
    setPending(true);
    setError(null);
    try {
      const params = {
        name: form.name.trim(),
        provider: form.provider,
        approvalPolicy: form.approvalPolicy,
        thinkingLevel: form.thinkingLevel,
        notes: form.notes,
      };
      // Upsert from the RPC's own returned profile rather than waiting for
      // the profile.created/updated event: the mutation's own tab must show
      // the result immediately even when events is null (not yet connected)
      // or when that event arrives after this promise already resolved --
      // upsertProfile dedupes by ID, so a later event is a harmless no-op.
      const saved =
        openCard === "new"
          ? await client.call<AgentProfile>("profile.create", params)
          : await client.call<AgentProfile>("profile.update", { id: openCard, ...params });
      setProfiles((prev) => (prev ? upsertProfile(prev, saved) : [saved]));
      closeCard();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setPending(false);
    }
  }

  async function handleDelete(id: number) {
    if (!client) return;
    try {
      await client.call("profile.delete", { id });
      setProfiles((prev) => (prev ? prev.filter((p) => p.ID !== id) : prev));
      if (openCard === id) closeCard();
      // A deleted default agent can't stay ★ -- a stale id would silently
      // stop seeding new tasks (run-config-toolbar.tsx's lookup just fails
      // to find it), which reads as "the default stopped working" rather
      // than the honest "there is no default agent anymore".
      if (defaultAgentId === String(id)) setDefaultAgentId(null);
    } catch (err) {
      console.error("profile.delete failed", err);
    }
  }

  const newAgentButton = (
    <Button type="button" size="sm" data-testid="profile-new-button" onClick={openNew}>
      <Plus aria-hidden className="size-3.5" /> New agent
    </Button>
  );

  return (
    <div className="flex flex-col gap-6" data-testid="settings-section-profiles">
      <section className="flex flex-col gap-2">
        <div className="flex items-center justify-between gap-2">
          <h3 className="text-ui-base font-medium text-foreground">Agents</h3>
          {newAgentButton}
        </div>

        {profiles === null ? (
          <p className="text-ui-base text-muted-foreground">Loading…</p>
        ) : (
          <>
            {profiles.length === 0 && openCard !== "new" && (
              <div data-testid="profiles-empty-state" className="flex flex-col items-start gap-2">
                <p className="text-ui-base text-muted-foreground">
                  No agents yet — create one to reuse a provider/approval/thinking bundle from the composer's
                  Agents picker.
                </p>
                <Button type="button" size="sm" data-testid="profiles-empty-new-button" onClick={openNew}>
                  <Plus aria-hidden className="size-3.5" /> New agent
                </Button>
              </div>
            )}
            {(profiles.length > 0 || openCard === "new") && (
              <ul className="flex flex-col gap-1">
                {openCard === "new" && (
                  <li data-testid="profile-new-form" className="rounded-md border px-2 py-1.5">
                    <ProfileForm
                      idPrefix="profile-form"
                      form={form}
                      setForm={setForm}
                      providers={providers}
                      client={client}
                      onSubmit={handleSubmit}
                      onCancel={closeCard}
                      pending={pending}
                      error={error}
                      submitLabel={pending ? "Saving…" : "Add agent"}
                    />
                  </li>
                )}
                {profiles.map((p) => {
                  const isDefault = defaultAgentId === String(p.ID);
                  const editing = openCard === p.ID;
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
                              onSelect={() => (editing ? closeCard() : startEdit(p))}
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
                          form={form}
                          setForm={setForm}
                          providers={providers}
                          client={client}
                          onSubmit={handleSubmit}
                          onCancel={closeCard}
                          pending={pending}
                          error={error}
                          submitLabel={pending ? "Saving…" : "Save"}
                        />
                      )}
                    </li>
                  );
                })}
              </ul>
            )}
          </>
        )}
      </section>
    </div>
  );
}

/**
 * The Name/Provider/Approval/Thinking/Notes fields, reused by both the
 * "New agent" card and each row's inline edit (run-config IA's "Edit
 * opens inline in the row" AC, and the follow-up polish pass's "reuse
 * one form component for both create and edit") -- `idPrefix` keeps
 * their data-testids distinct, though only one instance is ever mounted
 * at a time (ProfilesSection's `openCard`).
 *
 * Provider/Approval/Thinking sit in one 3-column equal grid row (the
 * polish pass's layout fix -- they used to be a 2-column row with
 * Thinking as an inconsistent full-width row below). Thinking is
 * Claude-only, so a non-Claude provider renders an empty grid cell
 * rather than a dead disabled control, keeping the three columns the
 * same width whichever provider is picked.
 */
function ProfileForm({
  idPrefix,
  form,
  setForm,
  providers,
  client,
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
  onSubmit: (e: FormEvent<HTMLFormElement>) => void;
  onCancel: () => void;
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
      <div className="grid grid-cols-3 gap-2">
        <div className="flex min-w-0 items-center gap-1.5">
          <Select value={form.provider} onValueChange={(value) => setForm((f) => ({ ...f, provider: value }))}>
            <SelectTrigger aria-label="Provider" data-testid={`${idPrefix}-provider`} className="w-full">
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
          <ProviderHealthDot client={client} provider={form.provider} providers={providers} testId={`${idPrefix}-provider-health`} />
        </div>
        <Select value={form.approvalPolicy} onValueChange={(value) => setForm((f) => ({ ...f, approvalPolicy: value }))}>
          <SelectTrigger aria-label="Approval policy" data-testid={`${idPrefix}-approval-policy`} className="w-full">
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
        {form.provider === "claude-native" ? (
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
        ) : (
          <div aria-hidden />
        )}
      </div>
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
        <Button type="button" variant="ghost" size="sm" data-testid={`${idPrefix}-cancel`} onClick={onCancel}>
          Cancel
        </Button>
        <Button type="submit" size="sm" disabled={pending || !form.name.trim()} data-testid={`${idPrefix}-submit`}>
          {submitLabel}
        </Button>
      </div>
    </form>
  );
}

/**
 * The inline card's provider health dot (run-config IA AC: "Provider
 * (with a health indicator for its backing account, reusing whatever the
 * accounts dialog already computes for account health)") -- same
 * provider.test RPC and the same ok/failed/untested -> StatusDot mapping
 * as accounts-dialog.tsx's own StatusDot, run independently here rather
 * than sharing that dialog's transient testResults state (this section
 * already fetches its own copies of profile.list/provider.list for the
 * same reason -- see ProfilesSection's doc comment). Shown for both the
 * "new agent" card and an existing row's edit -- the check is about the
 * *provider's* backing account, not whether this particular profile row
 * has been saved yet.
 */
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
