import type { ReactNode } from "react";

import { Checkbox } from "@/components/ui/checkbox";
import { instructionsBytes, INSTRUCTIONS_MAX_BYTES, type ChildPolicyChoice, type OnRateLimitChoice, type OrchestrationToolsChoice, type ProfileContractState } from "@/components/settings/profile-contract";
import { OwnerBadge } from "@/components/plugins/owner-badge";
import { Input } from "@/components/ui/input";
import { useSkillList } from "@/hooks/use-plugins";
import type { AgentProfile } from "@/lib/types";

const SELECT_CLASS =
  "h-8 w-full rounded-lg border border-input-border bg-input px-2.5 text-ui-base outline-none hover:border-input-border-hover focus-visible:border-input-border-focused focus-visible:bg-input-focused disabled:cursor-not-allowed disabled:opacity-50";

function Field({ label, htmlFor, hint, children }: { label: string; htmlFor: string; hint?: ReactNode; children: ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-1">
      <label htmlFor={htmlFor} className="text-ui-sm text-foreground-muted">
        {label}
      </label>
      {children}
      {hint}
    </div>
  );
}

function GroupTitle({ children }: { children: ReactNode }) {
  return <h4 className="border-t pt-2 text-ui-sm font-medium text-foreground">{children}</h4>;
}

/**
 * The ADR-0041 "Seat contract" and "When rate limited" groups of the profile
 * form. The daemon validates the combination (failover on a different provider,
 * `profiles` needs children, ...); its error text is shown by the form.
 */
export function ProfileContractFields<S extends ProfileContractState>({
  idPrefix,
  form,
  setForm,
  otherProfiles,
}: {
  idPrefix: string;
  form: S;
  setForm: (updater: (f: S) => S) => void;
  /** Every saved profile except the one being edited. */
  otherProfiles: AgentProfile[];
}) {
  const id = (name: string) => `${idPrefix}-${name}`;
  const bytes = instructionsBytes(form.instructions);
  const over = bytes > INSTRUCTIONS_MAX_BYTES;
  const toolsOff = form.orchestrationTools === "off";
  const available = useSkillList().data ?? [];
  const named = new Set(form.skills.split(/[\s,]+/).filter(Boolean));

  return (
    <>
      <GroupTitle>Seat contract</GroupTitle>
      <div className="grid grid-cols-1 gap-2 md:grid-cols-3">
        <Field label="Orchestration tools" htmlFor={id("orchestration-tools")}>
          <select
            id={id("orchestration-tools")}
            data-testid={id("orchestration-tools")}
            className={SELECT_CLASS}
            value={form.orchestrationTools}
            onChange={(e) => {
              const orchestrationTools = e.target.value as OrchestrationToolsChoice;
              setForm((f) => ({ ...f, orchestrationTools, childPolicy: orchestrationTools === "off" ? "none" : f.childPolicy }));
            }}
          >
            <option value="unmanaged">Unmanaged</option>
            <option value="on">On</option>
            <option value="off">Off</option>
          </select>
        </Field>
        <Field label="Skills" htmlFor={id("skills")}>
          <Input
            id={id("skills")}
            data-testid={id("skills")}
            placeholder="e.g. smind-peer, smind-leader"
            value={form.skills}
            onChange={(e) => setForm((f) => ({ ...f, skills: e.target.value }))}
          />
          {available.length > 0 && (
            <div className="flex flex-wrap gap-1" data-testid={id("skills-available")}>
              {available.map((skill) => (
                <span key={skill.name} className="inline-flex items-center gap-1">
                  <button
                    type="button"
                    data-testid={id(`skill-${skill.name}`)}
                    disabled={named.has(skill.name)}
                    title={skill.description || undefined}
                    className="rounded-full border border-border px-1.5 text-ui-sm text-foreground-muted hover:bg-hover disabled:opacity-50"
                    onClick={() => setForm((f) => ({ ...f, skills: f.skills.trim() ? `${f.skills.trim().replace(/,$/, "")}, ${skill.name}` : skill.name }))}
                  >
                    {skill.name}
                  </button>
                  <OwnerBadge owner={skill.owner} testId={id(`skill-owner-${skill.name}`)} />
                </span>
              ))}
            </div>
          )}
        </Field>
        <Field label="Child policy" htmlFor={id("child-policy")}>
          <select
            id={id("child-policy")}
            data-testid={id("child-policy")}
            className={SELECT_CLASS}
            value={form.childPolicy}
            onChange={(e) => setForm((f) => ({ ...f, childPolicy: e.target.value as ChildPolicyChoice }))}
          >
            <option value="none">None</option>
            <option value="leaf" disabled={toolsOff}>
              Leaf
            </option>
            <option value="profiles" disabled={toolsOff}>
              Profiles
            </option>
          </select>
        </Field>
      </div>
      {form.childPolicy === "profiles" && (
        <fieldset className="flex min-w-0 flex-col gap-1" data-testid={id("child-profiles")}>
          <legend className="mb-1 text-ui-sm text-foreground-muted">Child profiles this seat may start</legend>
          {otherProfiles.length === 0 ? (
            <p className="text-ui-sm text-foreground-subtlest">No other agents to choose from yet.</p>
          ) : (
            <div className="flex max-h-40 flex-col gap-0.5 overflow-y-auto rounded-lg border border-input-border bg-input p-1.5">
              {otherProfiles.map((p) => (
                <label key={p.ID} className="flex min-w-0 items-center gap-2 text-ui-sm text-foreground">
                  <Checkbox
                    data-testid={id(`child-profile-${p.ID}`)}
                    checked={form.childProfileIds.includes(p.ID)}
                    onCheckedChange={(on) =>
                      setForm((f) => ({
                        ...f,
                        childProfileIds: on === true ? [...f.childProfileIds, p.ID] : f.childProfileIds.filter((x) => x !== p.ID),
                      }))
                    }
                  />
                  <span className="truncate">{p.Name}</span>
                  <span className="shrink-0 text-foreground-subtlest">{p.Provider}</span>
                </label>
              ))}
            </div>
          )}
        </fieldset>
      )}
      <Field
        label="Instructions"
        htmlFor={id("instructions")}
        hint={
          <span data-testid={id("instructions-count")} className={over ? "text-ui-sm text-destructive" : "text-ui-sm text-foreground-subtlest"}>
            {bytes} / {INSTRUCTIONS_MAX_BYTES} bytes{over ? " — too long to save, shorten the instructions" : ""}
          </span>
        }
      >
        <textarea
          id={id("instructions")}
          data-testid={id("instructions")}
          aria-invalid={over || undefined}
          className="h-24 w-full resize-y rounded-lg border border-input-border bg-input px-2.5 py-1 text-ui-base outline-none hover:border-input-border-hover focus-visible:border-input-border-focused focus-visible:bg-input-focused aria-invalid:border-destructive"
          placeholder="Standing instructions for this seat (optional)."
          value={form.instructions}
          onChange={(e) => setForm((f) => ({ ...f, instructions: e.target.value }))}
        />
      </Field>

      <GroupTitle>When rate limited</GroupTitle>
      <div className="grid grid-cols-1 gap-2 md:grid-cols-2">
        <Field label="On rate limit" htmlFor={id("on-rate-limit")}>
          <select
            id={id("on-rate-limit")}
            data-testid={id("on-rate-limit")}
            className={SELECT_CLASS}
            value={form.onRateLimit}
            onChange={(e) => setForm((f) => ({ ...f, onRateLimit: e.target.value as OnRateLimitChoice }))}
          >
            <option value="none">None</option>
            <option value="wait">Wait</option>
            <option value="fallback">Fallback</option>
          </select>
        </Field>
        {form.onRateLimit === "fallback" && (
          <Field label="Fallback agent" htmlFor={id("fallback-profile")}>
            <select
              id={id("fallback-profile")}
              data-testid={id("fallback-profile")}
              className={SELECT_CLASS}
              value={form.fallbackProfileId ?? ""}
              onChange={(e) => setForm((f) => ({ ...f, fallbackProfileId: e.target.value === "" ? null : Number(e.target.value) }))}
            >
              <option value="">Choose an agent…</option>
              {otherProfiles.map((p) => (
                <option key={p.ID} value={p.ID}>
                  {p.Name} ({p.Provider})
                </option>
              ))}
            </select>
          </Field>
        )}
      </div>
    </>
  );
}
