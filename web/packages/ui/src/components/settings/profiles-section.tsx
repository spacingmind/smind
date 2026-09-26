import { useEffect, useState, type FormEvent } from "react";

import { registerSettingsSection, type SettingsSectionContext } from "@/components/settings/settings-registry";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import type { AgentProfile, ProviderInfo, ProviderListResult } from "@/lib/types";

/** Used until provider.list answers (and kept if it fails), same fallback composer.tsx uses -- this form must never be unusable because one fetch lost. */
const FALLBACK_PROVIDERS: ProviderInfo[] = [{ id: "claude-native" }, { id: "glm" }];

const APPROVAL_POLICIES = [
  { id: "", label: "Composer default" },
  { id: "manual", label: "Manual approval" },
  { id: "auto-safe", label: "Auto-safe" },
  { id: "full-access", label: "Full access" },
];

const THINKING_LEVELS = [
  { id: "", label: "Composer default" },
  { id: "off", label: "Off" },
  { id: "standard", label: "Standard" },
  { id: "extended", label: "Extended" },
];

/** The add/edit form's field state -- shared by the "new profile" form and an in-place row edit (formId identifies which, or null for the add form). */
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
 */
function ProfilesSection({ client, events }: SettingsSectionContext) {
  const [profiles, setProfiles] = useState<AgentProfile[] | null>(null);
  const [providers, setProviders] = useState<ProviderInfo[]>(FALLBACK_PROVIDERS);
  const [form, setForm] = useState<ProfileFormState>(EMPTY_FORM);
  const [editingId, setEditingId] = useState<number | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);

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

  function startEdit(p: AgentProfile) {
    setEditingId(p.ID);
    setForm(formFromProfile(p));
    setError(null);
  }

  function cancelEdit() {
    setEditingId(null);
    setForm(EMPTY_FORM);
    setError(null);
  }

  async function handleSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    if (!client || !form.name.trim()) return;
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
      // or when this same-tab event arrives after this promise already
      // resolved -- upsertProfile dedupes by ID, so a later event for the
      // same mutation is a harmless no-op re-set, not a duplicate row.
      const saved =
        editingId === null
          ? await client.call<AgentProfile>("profile.create", params)
          : await client.call<AgentProfile>("profile.update", { id: editingId, ...params });
      setProfiles((prev) => (prev ? upsertProfile(prev, saved) : [saved]));
      cancelEdit();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setPending(false);
    }
  }

  async function handleDelete(id: number) {
    if (!client) return;
    setError(null);
    try {
      await client.call("profile.delete", { id });
      setProfiles((prev) => (prev ? prev.filter((p) => p.ID !== id) : prev));
      if (editingId === id) cancelEdit();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
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
            {profiles.map((p) => (
              <li
                key={p.ID}
                data-testid={`profile-row-${p.ID}`}
                className="flex items-center justify-between gap-2 rounded-md border px-2 py-1.5"
              >
                <div className="flex min-w-0 flex-col">
                  <span className="truncate text-ui-base font-medium text-foreground">{p.Name}</span>
                  <span className="truncate text-ui-sm text-muted-foreground">
                    {providerLabel(providers, p.Provider)}
                    {p.ApprovalPolicy && ` · ${p.ApprovalPolicy}`}
                    {p.ThinkingLevel && ` · ${p.ThinkingLevel}`}
                  </span>
                </div>
                <div className="flex shrink-0 gap-1">
                  <Button variant="outline" size="xs" data-testid={`profile-edit-${p.ID}`} onClick={() => startEdit(p)}>
                    Edit
                  </Button>
                  <Button variant="ghost" size="xs" data-testid={`profile-delete-${p.ID}`} onClick={() => void handleDelete(p.ID)}>
                    Delete
                  </Button>
                </div>
              </li>
            ))}
          </ul>
        )}
      </section>

      <section className="flex flex-col gap-2 border-t pt-4">
        <p className="text-ui-base font-medium">{editingId === null ? "New agent" : "Edit agent"}</p>
        <form className="flex flex-col gap-2" onSubmit={handleSubmit}>
          <Input
            aria-label="Agent name"
            data-testid="profile-form-name"
            placeholder="Name (e.g. Quick Fixes)"
            value={form.name}
            onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))}
          />
          <div className="grid grid-cols-2 gap-2">
            <Select value={form.provider} onValueChange={(value) => setForm((f) => ({ ...f, provider: value }))}>
              <SelectTrigger aria-label="Provider" data-testid="profile-form-provider">
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
            <Select value={form.approvalPolicy} onValueChange={(value) => setForm((f) => ({ ...f, approvalPolicy: value }))}>
              <SelectTrigger aria-label="Approval policy" data-testid="profile-form-approval-policy">
                <SelectValue placeholder="Approval policy" />
              </SelectTrigger>
              <SelectContent>
                {APPROVAL_POLICIES.map((o) => (
                  <SelectItem key={o.id} value={o.id}>
                    {o.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          {form.provider === "claude-native" && (
            <Select value={form.thinkingLevel} onValueChange={(value) => setForm((f) => ({ ...f, thinkingLevel: value }))}>
              <SelectTrigger aria-label="Thinking level" data-testid="profile-form-thinking-level" className="w-full">
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
            data-testid="profile-form-notes"
            className="h-16 w-full resize-none rounded-lg border border-input bg-transparent px-2.5 py-1 text-ui-base outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
            placeholder="Notes (optional) — when to use this agent."
            value={form.notes}
            onChange={(e) => setForm((f) => ({ ...f, notes: e.target.value }))}
          />
          {error && (
            <p role="alert" data-testid="profile-form-error" className="text-ui-base text-destructive">
              {error}
            </p>
          )}
          <div className="flex justify-end gap-2">
            {editingId !== null && (
              <Button type="button" variant="ghost" size="sm" data-testid="profile-form-cancel" onClick={cancelEdit}>
                Cancel
              </Button>
            )}
            <Button type="submit" size="sm" disabled={pending || !form.name.trim()} data-testid="profile-form-submit">
              {pending ? "Saving…" : editingId === null ? "Add agent" : "Save agent"}
            </Button>
          </div>
        </form>
      </section>
    </div>
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

registerSettingsSection({
  id: "agents",
  label: "Agents",
  order: 175,
  render: (ctx) => <ProfilesSection {...ctx} />,
});
