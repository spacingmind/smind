import { useEffect, useMemo, useState, type FormEvent } from "react";

import { SettingsSectionTitle } from "@/components/settings/settings-title";
import { registerSettingsSection, type SettingsSectionContext } from "@/components/settings/settings-registry";
import { Button } from "@/components/ui/button";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { StatusDot as SharedStatusDot } from "@/components/ui/status-dot";
import { useDefaultAgentId } from "@/hooks/use-default-agent";
import { useProviderCatalog } from "@/hooks/use-provider-catalog";
import { useProviderHealth } from "@/hooks/use-provider-health";
import { useCreateProfile, useDeleteProfile, useProfiles, useUpdateProfile } from "@/hooks/use-profiles";
import { AutoAcceptToggle } from "@/components/pickers/auto-accept-toggle";
import { ModeSelect } from "@/components/pickers/mode-select";
import { ProviderModelPicker } from "@/components/pickers/provider-model-picker";
import { ThinkingSelect } from "@/components/pickers/thinking-select";
import { buildBrowserProviders } from "@/lib/model-browser";
import { isDangerousMode, providerModes, supportsAutoAccept } from "@/lib/permission-modes";
import { subscribeSettingsIntent, takeSettingsIntent, type ProfileSeed } from "@/lib/settings-intent";
import { supportsThinking, THINKING_LEVELS } from "@/lib/thinking-levels";
import { describeProfile } from "@/lib/describe-profile";
import type { AgentProfile, ModeInfo, ProviderInfo } from "@/lib/types";
import { Icons } from "@/lib/icons";

// "Provider default" (the empty id -- the provider's own default mode) is
// this form's own entry; the rest are the selected provider's own
// permission modes from provider.list (ADR-0019).
const PROVIDER_DEFAULT_MODE = { id: "", label: "Provider default" };

/** The form's levels: the shared vocabulary (lib/thinking-levels.ts) plus "unset", which defers to the composer. */
const FORM_THINKING_LEVELS = [{ id: "", label: "Composer default", help: "Use whatever the composer is set to for this agent's runs." }, ...THINKING_LEVELS];

/** The add/edit form's field state -- one instance shared by whichever card is open (the "new agent" card or a row's own inline edit), since only one is ever open at a time. */
interface ProfileFormState {
  name: string;
  provider: string;
  permissionMode: string;
  autoAccept: boolean;
  thinkingLevel: string;
  /** ADR-0025: a model id from the provider's catalog; "" is the provider default. */
  model: string;
  notes: string;
}

const EMPTY_FORM: ProfileFormState = { name: "", provider: "claude-native", permissionMode: "", autoAccept: false, thinkingLevel: "", model: "", notes: "" };

function formFromProfile(p: AgentProfile): ProfileFormState {
  return {
    name: p.Name,
    provider: p.Provider,
    permissionMode: p.PermissionMode,
    autoAccept: p.AutoAccept,
    thinkingLevel: p.ThinkingLevel,
    model: p.Model ?? "",
    notes: p.Notes,
  };
}

function formFromSeed(seed: ProfileSeed): ProfileFormState {
  return {
    ...EMPTY_FORM,
    name: seed.name,
    provider: seed.provider,
    model: seed.model,
    permissionMode: seed.permissionMode ?? "",
    autoAccept: seed.autoAccept ?? false,
    thinkingLevel: seed.thinkingLevel ?? "",
  };
}

/** Which card is open: "new" for the add card (rendered at the top of the list), a profile id for that row's inline edit, or null for none. Only one is ever open -- opening one closes whichever was open before. */
type OpenCard = number | "new" | null;

/**
 * Settings -> Profiles (ADR-0014 / docs/plans/active/agent-profiles.md):
 * create, edit, and delete named provider/permissionMode/thinkingLevel
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
function ProfilesSection(_props: SettingsSectionContext) {
  // One profile.list / provider.list cache entry per connection (ADR-0032):
  // live updates arrive through the event bridge, so this section no longer
  // subscribes to profile.* itself, and a profile saved here reaches the
  // composer picker and the palette immediately.
  const profilesQuery = useProfiles();
  const profiles = profilesQuery.data ?? null;
  const { providersOrFallback: providers, settled: providersSettled } = useProviderCatalog();
  const [openCard, setOpenCard] = useState<OpenCard>(null);
  const [form, setForm] = useState<ProfileFormState>(EMPTY_FORM);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // The run-config IA plan's ★ default agent: which profile a brand-new
  // task's composer toolbar starts from (run-config-toolbar.tsx reads the
  // same stored id). Replaces General's old "Defaults for new tasks"
  // control -- see the plan's Decisions section.
  const { defaultAgentId, setDefaultAgentId } = useDefaultAgentId();
  const createProfile = useCreateProfile();
  const updateProfile = useUpdateProfile();
  const deleteProfile = useDeleteProfile();

  // Composer picker handoff (lib/settings-intent.ts): applied once the list has
  // loaded (an edit intent needs the profile), and again whenever one arrives
  // while this section is already open.
  const [intentTick, setIntentTick] = useState(0);
  useEffect(() => subscribeSettingsIntent(() => setIntentTick((t) => t + 1)), []);
  useEffect(() => {
    if (profiles === null) return;
    const intent = takeSettingsIntent();
    if (!intent) return;
    if (intent.kind === "newProfile") {
      setOpenCard("new");
      setForm(formFromSeed(intent.seed));
      setError(null);
      return;
    }
    const target = profiles.find((p) => p.ID === intent.id);
    if (!target) return;
    setOpenCard(target.ID);
    setForm(formFromProfile(target));
    setError(null);
  }, [profiles, intentTick]);

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
    if (openCard === null || !form.name.trim()) return;
    setPending(true);
    setError(null);
    const params = {
      name: form.name.trim(),
      provider: form.provider,
      permissionMode: form.permissionMode,
      autoAccept: supportsAutoAccept(providers, form.provider) && form.autoAccept,
      thinkingLevel: form.thinkingLevel,
      model: form.model,
      notes: form.notes,
    };
    // The mutation upserts the RPC's own returned profile into the shared
    // cache, so every surface (composer picker, palette, task header) sees
    // it immediately; the profile.* event arriving too is idempotent.
    try {
      if (openCard === "new") await createProfile.mutateAsync(params);
      else await updateProfile.mutateAsync({ id: openCard, ...params });
      closeCard();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setPending(false);
    }
  }

  async function handleDelete(id: number) {
    try {
      await deleteProfile.mutateAsync(id);
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
      <Icons.add aria-hidden className="size-3.5" /> New agent
    </Button>
  );

  return (
    <div className="flex flex-col gap-6" data-testid="settings-section-profiles">
      <section className="flex flex-col gap-2">
        <SettingsSectionTitle title="Agents" actions={newAgentButton} />

        {profiles === null ? (
          <p className="text-ui-base text-muted-foreground">Loading…</p>
        ) : (
          <>
            {profiles.length === 0 && openCard !== "new" && (
              <div data-testid="profiles-empty-state" className="flex flex-col items-start gap-2">
                <p className="text-ui-base text-muted-foreground">
                  No agents yet — create one with New agent to reuse a provider/mode/thinking bundle from the
                  composer's Agents picker.
                </p>
              </div>
            )}
            {(profiles.length > 0 || openCard === "new") && (
              <ul className="flex flex-col gap-1 rounded-xl border bg-card p-3">
                {openCard === "new" && (
                  <li data-testid="profile-new-form" className="rounded-lg px-2 py-1.5">
                    <ProfileForm
                      idPrefix="profile-form"
                      form={form}
                      setForm={setForm}
                      providers={providers}
                      onSubmit={handleSubmit}
                      onCancel={closeCard}
                      pending={pending}
                      error={error}
                      submitLabel={pending ? "Saving…" : "Add agent"}
                      providersLoading={!providersSettled}
                    />
                  </li>
                )}
                {profiles.map((p) => {
                  const isDefault = defaultAgentId === String(p.ID);
                  const editing = openCard === p.ID;
                  return (
                    <li key={p.ID} data-testid={`profile-row-${p.ID}`} className="flex flex-col gap-2 rounded-lg px-2 py-1.5 hover:bg-hover">
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
                          <Icons.defaultAgent aria-hidden fill={isDefault ? "currentColor" : "none"} />
                        </Button>
                        <div className="flex min-w-0 flex-1 flex-col">
                          <span className="truncate text-ui-base font-medium text-foreground">{p.Name}</span>
                          <span className="truncate text-ui-sm text-muted-foreground">{describeProfile(p, providers)}</span>
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
                              <Icons.more aria-hidden />
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
                          onSubmit={handleSubmit}
                          onCancel={closeCard}
                          pending={pending}
                          error={error}
                          submitLabel={pending ? "Saving…" : "Save"}
                          providersLoading={!providersSettled}
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
 * The Name/Provider/Mode/Thinking/Notes fields, reused by both the
 * "New agent" card and each row's inline edit (run-config IA's "Edit
 * opens inline in the row" AC, and the follow-up polish pass's "reuse
 * one form component for both create and edit") -- `idPrefix` keeps
 * their data-testids distinct, though only one instance is ever mounted
 * at a time (ProfilesSection's `openCard`).
 *
 * Provider/Mode/Thinking sit in one 3-column equal grid row (the
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
  onSubmit,
  onCancel,
  pending,
  error,
  submitLabel,
  providersLoading,
}: {
  idPrefix: string;
  form: ProfileFormState;
  setForm: (updater: (f: ProfileFormState) => ProfileFormState) => void;
  providers: ProviderInfo[];
  onSubmit: (e: FormEvent<HTMLFormElement>) => void;
  onCancel: () => void;
  pending: boolean;
  error: string | null;
  submitLabel: string;
  /** provider.list has not settled: the picker says "Loading…" like the composer's, instead of fallback ids. */
  providersLoading: boolean;
}) {
  const browserProviders = useMemo(() => buildBrowserProviders(providers, {}, { loading: providersLoading }), [providers, providersLoading]);
  return (
    <form className="flex flex-col gap-2" onSubmit={onSubmit}>
      <Input
        aria-label="Agent name"
        data-testid={`${idPrefix}-name`}
        placeholder="Name (e.g. Quick Fixes)"
        value={form.name}
        onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))}
      />
      <div className="grid grid-cols-2 gap-2 md:grid-cols-4">
        <div className="col-span-2 flex min-w-0 items-center gap-1.5">
          {/* The composer's own provider+model picker (field variant). Provider health here is advisory (the dot): a profile may be
              set up for a provider whose login is still to come, so an unhealthy provider is not made unselectable. */}
          <ProviderModelPicker
            variant="field"
            testId={`${idPrefix}-provider`}
            providers={browserProviders}
            selectedProvider={form.provider}
            selectedModel={form.model}
            boundProvider={null}
            disabled={false}
            // Modes and models are provider-scoped: a new provider starts from its own defaults.
            onSelect={(provider, model) =>
              setForm((f) =>
                provider === f.provider
                  ? { ...f, model }
                  : { ...f, provider, permissionMode: "", autoAccept: false, model },
              )
            }
          />
          <ProviderHealthDot provider={form.provider} testId={`${idPrefix}-provider-health`} />
        </div>
        <ModeSelect
          variant="field"
          testId={`${idPrefix}-permission-mode`}
          modes={providerModes(providers, form.provider)}
          extraItems={[PROVIDER_DEFAULT_MODE as ModeInfo]}
          value={form.permissionMode}
          dangerous={isDangerousMode(providers, form.provider, form.permissionMode, form.autoAccept)}
          onChange={(value) => setForm((f) => ({ ...f, permissionMode: value }))}
        />
        {supportsThinking(form.provider) ? (
          <ThinkingSelect
            variant="field"
            testId={`${idPrefix}-thinking-level`}
            levels={FORM_THINKING_LEVELS}
            value={form.thinkingLevel}
            onChange={(value) => setForm((f) => ({ ...f, thinkingLevel: value }))}
          />
        ) : null}
      </div>
      {supportsAutoAccept(providers, form.provider) && (
        <AutoAcceptToggle
          variant="field"
          testId={`${idPrefix}-auto-accept`}
          checked={form.autoAccept}
          onChange={(checked) => setForm((f) => ({ ...f, autoAccept: checked }))}
        />
      )}
      <textarea
        aria-label="Notes"
        data-testid={`${idPrefix}-notes`}
        className="h-16 w-full resize-none rounded-lg border border-input-border bg-input px-2.5 py-1 text-ui-base outline-none hover:border-input-border-hover focus-visible:border-input-border-focused focus-visible:bg-input-focused"
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
function ProviderHealthDot({ provider, testId }: { provider: string; testId: string }) {
  const { health, ensure, rpcFailed } = useProviderHealth();
  // Fills this provider's answer if nothing has (cached answers are kept; see runProviderTest).
  useEffect(() => {
    ensure();
  }, [ensure]);

  const result = health[provider];
  const state = rpcFailed.has(provider) ? "failed" : result === undefined ? "unknown" : result.ok ? "ok" : "failed";
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

registerSettingsSection({
  id: "agents",
  label: "Agents",
  groupLabel: "Agents & providers",
  order: 175,
  icon: <Icons.agent aria-hidden className="size-3.5" />,
  render: (ctx) => <ProfilesSection {...ctx} />,
});
