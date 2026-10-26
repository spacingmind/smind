import { useMemo, useState, type FormEvent, type ReactNode } from "react";

import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { FormError } from "@/components/ui/form-error";
import { Input } from "@/components/ui/input";
import { useAutomationActions } from "@/hooks/use-automations";
import { useProfileList } from "@/hooks/use-profiles";
import { useWorkspaceTree } from "@/hooks/use-workspace-tree";
import { formatLocal, formatUtc } from "@/lib/automation-format";
import type { AutomationDef, AutomationSignalKind, AutomationView } from "@/lib/automation-types";
import { nextCronRuns } from "@/lib/cron-next";

/** The scalar signal fields a template may reference (internal/automation scalarFields). Signal text is data, never instructions. */
export const SIGNAL_PLACEHOLDERS = ["repo", "workflow", "branch", "headSha", "runId", "runUrl", "conclusion", "event", "scheduledAt", "firedAt", "catchup"] as const;

/** The documented defaults (internal/automation WithDefaults), shown beside the rails they apply to. */
export const RAIL_DEFAULTS = { maxRunsPerHour: 2, dedupeCi: 1440, dedupeCron: 0, pauseAfterFailures: 3, budgetUsd: 3, maxRunMinutes: 45 } as const;

const MODES = ["default", "acceptEdits", "plan"] as const;
const CONCLUSIONS = ["failure", "cancelled", "timed_out"] as const;

const SELECT_CLASS =
  "h-8 w-full rounded-lg border border-input-border bg-input px-2 text-ui-base outline-none hover:border-input-border-hover focus-visible:border-input-border-focused focus-visible:bg-input-focused disabled:cursor-not-allowed disabled:opacity-50";
const TEXTAREA_CLASS =
  "w-full rounded-lg border border-input-border bg-input px-2.5 py-1.5 font-mono text-ui-base outline-none placeholder:text-foreground-subtlest hover:border-input-border-hover focus-visible:border-input-border-focused focus-visible:bg-input-focused";

function Field({ label, htmlFor, hint, children }: { label: string; htmlFor: string; hint?: ReactNode; children: ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-1">
      <label htmlFor={htmlFor} className="text-ui-sm text-foreground-muted">
        {label}
      </label>
      {children}
      {hint && <span className="text-ui-sm text-foreground-subtlest">{hint}</span>}
    </div>
  );
}

function GroupTitle({ children }: { children: ReactNode }) {
  return <h4 className="border-t pt-2 text-ui-sm font-medium text-foreground">{children}</h4>;
}

interface FormState {
  name: string;
  workspaceId: string;
  profileId: string;
  model: string;
  mode: string;
  kind: AutomationSignalKind;
  cron: string;
  workflow: string;
  branch: string;
  conclusions: string[];
  titleTemplate: string;
  promptTemplate: string;
  maxRunsPerHour: string;
  dedupe: string;
  pauseAfterFailures: string;
  budgetUsd: string;
  maxRunMinutes: string;
  memoryEnabled: boolean;
}

function initialState(a: AutomationView | null): FormState {
  if (!a) {
    return {
      name: "",
      workspaceId: "",
      profileId: "",
      model: "",
      mode: "default",
      kind: "cron",
      cron: "",
      workflow: "",
      branch: "develop",
      conclusions: ["failure"],
      titleTemplate: "",
      promptTemplate: "",
      maxRunsPerHour: "",
      dedupe: "",
      pauseAfterFailures: "",
      budgetUsd: "",
      maxRunMinutes: "",
      memoryEnabled: true,
    };
  }
  return {
    name: a.name,
    workspaceId: String(a.workspaceId),
    profileId: String(a.template.profileId),
    model: a.template.model,
    mode: a.template.mode || "default",
    kind: a.signal.kind,
    cron: a.signal.cron ?? "",
    workflow: a.signal.workflow ?? "",
    branch: a.signal.branch || "develop",
    conclusions: a.signal.conclusions?.length ? a.signal.conclusions : ["failure"],
    titleTemplate: a.template.titleTemplate,
    promptTemplate: a.template.promptTemplate,
    maxRunsPerHour: String(a.rails.maxRunsPerHour),
    dedupe: a.rails.dedupeWindowMinutes === undefined ? "" : String(a.rails.dedupeWindowMinutes),
    pauseAfterFailures: String(a.rails.pauseAfterFailures),
    budgetUsd: String(a.template.budgetUsd),
    maxRunMinutes: String(a.template.maxRunMinutes),
    memoryEnabled: a.rails.memoryEnabled,
  };
}

/** A blank number field means "the documented default" (the daemon fills it in); anything else is sent as typed. */
function num(raw: string): number {
  const n = Number(raw.trim());
  return raw.trim() === "" || Number.isNaN(n) ? 0 : n;
}

function toDef(f: FormState, editing: AutomationView | null): AutomationDef {
  return {
    name: f.name.trim(),
    workspaceId: Number(f.workspaceId),
    signal:
      f.kind === "cron"
        ? { kind: "cron", cron: f.cron.trim() }
        : { kind: "github.workflow_run", workflow: f.workflow.trim(), branch: f.branch.trim(), conclusions: f.conclusions },
    template: {
      profileId: Number(f.profileId),
      model: f.model.trim(),
      mode: f.mode,
      titleTemplate: f.titleTemplate.trim(),
      promptTemplate: f.promptTemplate,
      budgetUsd: num(f.budgetUsd),
      maxRunMinutes: num(f.maxRunMinutes),
    },
    rails: {
      maxRunsPerHour: num(f.maxRunsPerHour),
      ...(f.dedupe.trim() === "" ? {} : { dedupeWindowMinutes: num(f.dedupe) }),
      pauseAfterFailures: num(f.pauseAfterFailures),
      memoryEnabled: f.memoryEnabled,
      memoryMaxBytes: editing?.rails.memoryMaxBytes ?? 0,
    },
  };
}

/** The live "next three fire times" of a cron spec, UTC and local; the daemon's own answer stays the authority on save. */
function CronPreview({ spec }: { spec: string }) {
  const result = useMemo(() => (spec.trim() === "" ? null : nextCronRuns(spec, 3)), [spec]);
  if (!result) return <span className="text-ui-sm text-foreground-subtlest">Five fields: minute hour day-of-month month day-of-week, in UTC. At most every 5 minutes.</span>;
  if (!result.ok) return <FormError message={result.error} size="sm" silent testId="automation-cron-error" />;
  if (result.runs.length === 0) return <FormError message="This schedule never fires." size="sm" silent testId="automation-cron-error" />;
  return (
    <ul className="flex flex-col gap-0.5 text-ui-sm text-foreground-muted" data-testid="automation-cron-preview" aria-label="Next three fire times">
      {result.runs.map((d) => (
        <li key={d.toISOString()}>
          {formatUtc(d.toISOString())} <span className="text-foreground-subtlest">· {formatLocal(d)} local</span>
        </li>
      ))}
    </ul>
  );
}

/**
 * Create / edit one automation (ADR-0044 section 10). Creating always yields a DISABLED automation. The daemon is
 * the validator: its refusal (an unattended mode without the config opt-in, a cadence under 5 minutes, a budget
 * over the ceiling) is shown inline; `onForbidden` lets the page go read-only on a `forbidden` answer.
 */
export function AutomationFormDialog({
  open,
  editing,
  onClose,
  onSaved,
  onForbidden,
}: {
  open: boolean;
  /** null = create. */
  editing: AutomationView | null;
  onClose: () => void;
  onSaved: (message: string) => void;
  onForbidden: (err: unknown) => void;
}) {
  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
      <DialogContent data-testid="automation-form-dialog" className="max-h-[90vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader className="text-left">
          <DialogTitle>{editing ? `Edit ${editing.name}` : "New automation"}</DialogTitle>
          <DialogDescription>
            {editing ? "Changes apply to the next signal." : "Created disabled. Enable it when you are ready."}
          </DialogDescription>
        </DialogHeader>
        {open && <AutomationForm key={editing?.id ?? "new"} editing={editing} onClose={onClose} onSaved={onSaved} onForbidden={onForbidden} />}
      </DialogContent>
    </Dialog>
  );
}

function AutomationForm({
  editing,
  onClose,
  onSaved,
  onForbidden,
}: {
  editing: AutomationView | null;
  onClose: () => void;
  onSaved: (message: string) => void;
  onForbidden: (err: unknown) => void;
}) {
  const actions = useAutomationActions();
  const profiles = useProfileList();
  const { workspaces } = useWorkspaceTree();
  const [form, setForm] = useState<FormState>(() => initialState(editing));
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const set = <K extends keyof FormState>(key: K, value: FormState[K]) => setForm((f) => ({ ...f, [key]: value }));

  const cronOk = form.kind !== "cron" || (form.cron.trim() !== "" && (() => { const r = nextCronRuns(form.cron, 1); return r.ok && r.runs.length > 0; })());
  const signalOk = form.kind === "cron" ? cronOk : form.workflow.trim() !== "" && form.branch.trim() !== "";
  const valid =
    form.name.trim() !== "" &&
    form.workspaceId !== "" &&
    form.profileId !== "" &&
    form.model.trim() !== "" &&
    signalOk &&
    form.titleTemplate.trim() !== "" &&
    form.promptTemplate.trim() !== "";
  const modes = MODES.includes(form.mode as (typeof MODES)[number]) ? [...MODES] : [form.mode, ...MODES];
  const dedupeDefault = form.kind === "cron" ? RAIL_DEFAULTS.dedupeCron : RAIL_DEFAULTS.dedupeCi;

  async function submit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    if (!valid || saving) return;
    setSaving(true);
    setError(null);
    try {
      const def = toDef(form, editing);
      if (editing) {
        await actions.update(editing.id, def);
        onSaved(`Saved ${def.name}.`);
      } else {
        await actions.create(def);
        onSaved("Created disabled. Enable it when ready.");
      }
    } catch (err) {
      onForbidden(err);
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  }

  return (
    <form className="flex flex-col gap-3" data-testid="automation-form" onSubmit={submit}>
      <div className="grid grid-cols-1 gap-2 md:grid-cols-2">
        <Field label="Name" htmlFor="automation-name">
          <Input id="automation-name" data-testid="automation-name" value={form.name} autoComplete="off" onChange={(e) => set("name", e.target.value)} />
        </Field>
        <Field label="Workspace" htmlFor="automation-workspace">
          <select id="automation-workspace" data-testid="automation-workspace" className={SELECT_CLASS} value={form.workspaceId} onChange={(e) => set("workspaceId", e.target.value)}>
            <option value="">Choose a workspace…</option>
            {(workspaces ?? []).map((w) => (
              <option key={w.ID} value={String(w.ID)}>
                {w.Title}
              </option>
            ))}
          </select>
        </Field>
        <Field label="Agent profile" htmlFor="automation-profile">
          <select id="automation-profile" data-testid="automation-profile" className={SELECT_CLASS} value={form.profileId} onChange={(e) => set("profileId", e.target.value)}>
            <option value="">Choose a profile…</option>
            {profiles.map((p) => (
              <option key={p.ID} value={String(p.ID)}>
                {p.Name}
              </option>
            ))}
          </select>
        </Field>
        <Field label="Model" htmlFor="automation-model" hint="An explicit model id; there is no provider default.">
          <Input id="automation-model" data-testid="automation-model" value={form.model} autoComplete="off" spellCheck={false} onChange={(e) => set("model", e.target.value)} />
        </Field>
        <Field label="Mode" htmlFor="automation-mode" hint="Runs are unattended: full-access and autoAccept need an opt-in in config.yaml.">
          <select id="automation-mode" data-testid="automation-mode" className={SELECT_CLASS} value={form.mode} onChange={(e) => set("mode", e.target.value)}>
            {modes.map((m) => (
              <option key={m} value={m}>
                {m}
              </option>
            ))}
          </select>
        </Field>
      </div>

      <GroupTitle>Signal</GroupTitle>
      <div role="radiogroup" aria-label="Signal" className="flex flex-wrap gap-x-4 gap-y-1">
        {(
          [
            ["cron", "Schedule"],
            ["github.workflow_run", "GitHub workflow failed"],
          ] as const
        ).map(([kind, label]) => (
          <label key={kind} className="flex items-center gap-2 text-ui-base text-foreground">
            <input type="radio" name="automation-signal-kind" data-testid={`automation-kind-${kind}`} checked={form.kind === kind} onChange={() => set("kind", kind)} />
            {label}
          </label>
        ))}
      </div>
      {form.kind === "cron" ? (
        <Field label="Cron (UTC)" htmlFor="automation-cron">
          <Input id="automation-cron" data-testid="automation-cron" className="font-mono" placeholder="0 3 * * *" value={form.cron} autoComplete="off" spellCheck={false} onChange={(e) => set("cron", e.target.value)} />
          <CronPreview spec={form.cron} />
        </Field>
      ) : (
        <div className="grid grid-cols-1 gap-2 md:grid-cols-2">
          <Field label="Workflow" htmlFor="automation-workflow" hint="The workflow's name or file.">
            <Input id="automation-workflow" data-testid="automation-workflow" value={form.workflow} autoComplete="off" spellCheck={false} onChange={(e) => set("workflow", e.target.value)} />
          </Field>
          <Field label="Branch" htmlFor="automation-branch">
            <Input id="automation-branch" data-testid="automation-branch" value={form.branch} autoComplete="off" spellCheck={false} onChange={(e) => set("branch", e.target.value)} />
          </Field>
          <fieldset className="flex flex-wrap items-center gap-x-4 gap-y-1 md:col-span-2">
            <legend className="mb-1 text-ui-sm text-foreground-muted">Conclusions that fire</legend>
            {CONCLUSIONS.map((c) => (
              <label key={c} className="flex items-center gap-2 text-ui-base text-foreground">
                <input
                  type="checkbox"
                  data-testid={`automation-conclusion-${c}`}
                  checked={form.conclusions.includes(c)}
                  onChange={(e) => set("conclusions", e.target.checked ? [...form.conclusions, c] : form.conclusions.filter((x) => x !== c))}
                />
                {c}
              </label>
            ))}
          </fieldset>
        </div>
      )}

      <GroupTitle>Task</GroupTitle>
      <Field label="Title template" htmlFor="automation-title">
        <Input id="automation-title" data-testid="automation-title" value={form.titleTemplate} autoComplete="off" onChange={(e) => set("titleTemplate", e.target.value)} />
      </Field>
      <Field label="Prompt template" htmlFor="automation-prompt">
        <textarea id="automation-prompt" data-testid="automation-prompt" rows={6} className={TEXTAREA_CLASS} value={form.promptTemplate} onChange={(e) => set("promptTemplate", e.target.value)} />
      </Field>
      <div className="flex flex-col gap-1 text-ui-sm text-foreground-subtlest" data-testid="automation-placeholders">
        <span>
          Placeholders:{" "}
          {SIGNAL_PLACEHOLDERS.map((p) => (
            <code key={p} className="mr-1.5 inline-block font-mono text-foreground-muted">{`{{signal.${p}}}`}</code>
          ))}
        </span>
        <span>Signal text is data: a placeholder inserts a scalar field (an id, a branch, a sha), never instructions the agent should follow.</span>
      </div>

      <GroupTitle>Rails</GroupTitle>
      <div className="grid grid-cols-2 gap-2 md:grid-cols-3">
        <Field label="Runs per hour" htmlFor="automation-max-runs" hint={`default ${RAIL_DEFAULTS.maxRunsPerHour}`}>
          <Input id="automation-max-runs" data-testid="automation-max-runs" inputMode="numeric" placeholder={String(RAIL_DEFAULTS.maxRunsPerHour)} value={form.maxRunsPerHour} onChange={(e) => set("maxRunsPerHour", e.target.value)} />
        </Field>
        <Field label="Dedupe window (min)" htmlFor="automation-dedupe" hint={`default ${dedupeDefault}${form.kind === "cron" ? "" : " for CI"}`}>
          <Input id="automation-dedupe" data-testid="automation-dedupe" inputMode="numeric" placeholder={String(dedupeDefault)} value={form.dedupe} onChange={(e) => set("dedupe", e.target.value)} />
        </Field>
        <Field label="Pause after failures" htmlFor="automation-pause-after" hint={`default ${RAIL_DEFAULTS.pauseAfterFailures}`}>
          <Input id="automation-pause-after" data-testid="automation-pause-after" inputMode="numeric" placeholder={String(RAIL_DEFAULTS.pauseAfterFailures)} value={form.pauseAfterFailures} onChange={(e) => set("pauseAfterFailures", e.target.value)} />
        </Field>
        <Field label="Budget per run (USD)" htmlFor="automation-budget" hint={`default $${RAIL_DEFAULTS.budgetUsd}`}>
          <Input id="automation-budget" data-testid="automation-budget" inputMode="decimal" placeholder={String(RAIL_DEFAULTS.budgetUsd)} value={form.budgetUsd} onChange={(e) => set("budgetUsd", e.target.value)} />
        </Field>
        <Field label="Max run minutes" htmlFor="automation-max-minutes" hint={`default ${RAIL_DEFAULTS.maxRunMinutes}`}>
          <Input id="automation-max-minutes" data-testid="automation-max-minutes" inputMode="numeric" placeholder={String(RAIL_DEFAULTS.maxRunMinutes)} value={form.maxRunMinutes} onChange={(e) => set("maxRunMinutes", e.target.value)} />
        </Field>
        <label className="flex items-center gap-2 self-end pb-1.5 text-ui-base text-foreground">
          <input type="checkbox" data-testid="automation-memory" checked={form.memoryEnabled} onChange={(e) => set("memoryEnabled", e.target.checked)} />
          Memory <span className="text-ui-sm text-foreground-subtlest">(default on)</span>
        </label>
      </div>

      <FormError message={error} testId="automation-form-error" />
      <DialogFooter>
        <Button type="button" variant="ghost" size="sm" data-testid="automation-form-cancel" onClick={onClose}>
          Cancel
        </Button>
        <Button type="submit" size="sm" data-testid="automation-form-submit" disabled={!valid || saving}>
          {saving ? "Saving…" : editing ? "Save changes" : "Create (disabled)"}
        </Button>
      </DialogFooter>
    </form>
  );
}
