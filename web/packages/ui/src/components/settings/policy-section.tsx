import { useEffect, useRef, useState } from "react";

import { SettingsGroup, SettingsRow } from "@/components/settings/settings-layout";
import { registerSettingsSection, type SettingsSectionContext } from "@/components/settings/settings-registry";
import { Alert } from "@/components/ui/alert";
import { FormError } from "@/components/ui/form-error";
import { LoadingRows } from "@/components/ui/loading-rows";
import { StatusBadge, type StatusBadgeStatus } from "@/components/ui/status-badge";
import { usePolicyAudit, usePolicyStatus } from "@/hooks/use-policy";
import { Icons } from "@/lib/icons";
import type { PolicyAuditRow, PolicyRule, PolicyStatus } from "@/lib/policy-types";
import { formatRelativeTime } from "@/lib/relative-time";
import { EM_DASH } from "@/lib/usage-format";

const ACTION_BADGE: Record<string, StatusBadgeStatus> = {
  allow: "success",
  ask: "warning",
  deny: "danger",
};

/** The action/decision pill: a known action in its own tier, anything else as raw text on the neutral shell. */
function ActionBadge({ value }: { value: string }) {
  return <StatusBadge status={ACTION_BADGE[value] ?? "neutral"}>{value}</StatusBadge>;
}

/** A rule's matching scope as plain text ("tool Bash, argv0 git"), or an em dash when the rule is unscoped. */
function ruleScope(rule: PolicyRule): string {
  const parts: string[] = [];
  if (rule.seats?.length) parts.push(`seats ${rule.seats.join(", ")}`);
  if (rule.workspaces?.length) parts.push(`workspaces ${rule.workspaces.join(", ")}`);
  if (rule.tool) parts.push(`tool ${rule.tool}`);
  if (rule.argv0) parts.push(`argv0 ${rule.argv0}`);
  if (rule.subcommand) parts.push(`subcommand ${rule.subcommand}`);
  return parts.length > 0 ? parts.join(", ") : EM_DASH;
}

/** The audit row's "what was asked for" cell: tool + target on one truncated line, the full text in `title`. */
function auditTarget(row: PolicyAuditRow): { text: string; full: string } {
  const text = row.target ? `${row.tool} ${row.target}` : row.tool;
  return { text, full: text };
}

/**
 * Settings -> Permission policy (ADR-0047 §9): the human-edited policy
 * file's validity, rules and recent decisions, read-only -- the page never
 * writes the file, so there is no control on it that could.
 */
function PolicySection(_ctx: SettingsSectionContext) {
  const status = usePolicyStatus();
  const audit = usePolicyAudit(50);
  const [announcement, setAnnouncement] = useState("");

  // The polite live region: a spoken line only when validity flips to false
  // after the first load (never on the first render, valid or not).
  const lastValid = useRef<boolean | null>(null);
  const valid = status.data?.valid;
  useEffect(() => {
    if (valid === undefined) return;
    if (lastValid.current === true && valid === false) setAnnouncement("Policy file is invalid");
    lastValid.current = valid;
  }, [valid]);

  return (
    <div className="flex flex-col gap-6" data-testid="settings-section-permission-policy">
      <div role="status" aria-live="polite" className="sr-only" data-testid="policy-live">
        {announcement}
      </div>

      {status.isPending ? (
        <LoadingRows />
      ) : status.isError ? (
        <FormError message="Could not load the policy." testId="policy-error" />
      ) : (
        <>
          <ValidityAlert status={status.data} />
          <SettingsGroup title="Policy file">
            <SettingsRow
              label="File"
              control={
                <span data-testid="policy-path" className="font-mono text-ui-sm text-foreground-muted">
                  {status.data?.path || EM_DASH}
                </span>
              }
            />
            <SettingsRow label="Edit it on disk; this page never writes it." />
          </SettingsGroup>

          <div className="flex flex-col gap-3">
            <h3 className="text-ui-base font-medium text-foreground">Rules</h3>
            {(status.data?.rules ?? []).length === 0 ? (
              <p className="text-ui-sm text-foreground-muted" data-testid="policy-rules">
                No rules.
              </p>
            ) : (
              <div className="overflow-x-auto rounded-xl border bg-card" data-testid="policy-rules">
                <table className="w-full text-ui-sm">
                  <caption className="sr-only">Policy rules</caption>
                  <thead>
                    <tr className="text-left text-muted-foreground">
                      <th className="px-3 py-2 font-normal">Rule</th>
                      <th className="px-3 py-2 font-normal">Action</th>
                      <th className="px-3 py-2 font-normal">Why</th>
                      <th className="px-3 py-2 font-normal">Scope</th>
                    </tr>
                  </thead>
                  <tbody>
                    {(status.data?.rules ?? []).map((rule) => (
                      <tr key={rule.id} data-testid={`policy-rule-${rule.id}`} className="border-t">
                        <td className="px-3 py-2 font-mono text-foreground">{rule.id}</td>
                        <td className="px-3 py-2">
                          <ActionBadge value={rule.action} />
                        </td>
                        <td className="px-3 py-2 text-foreground-muted">{rule.why}</td>
                        <td className="px-3 py-2 text-foreground-muted">{ruleScope(rule)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>
        </>
      )}

      <div className="flex flex-col gap-3">
        <h3 className="text-ui-base font-medium text-foreground">Recent decisions</h3>
        {audit.isPending ? (
          <LoadingRows />
        ) : audit.isError ? (
          <FormError message="Could not load the audit." testId="policy-audit-error" />
        ) : (audit.data?.rows ?? []).length === 0 ? (
          <p className="text-ui-sm text-foreground-muted" data-testid="policy-audit-empty">
            No decisions recorded yet.
          </p>
        ) : (
          <div className="overflow-x-auto rounded-xl border bg-card" data-testid="policy-audit">
            <table className="w-full text-ui-sm">
              <caption className="sr-only">Recent policy decisions</caption>
              <thead>
                <tr className="text-left text-muted-foreground">
                  <th className="px-3 py-2 font-normal">When</th>
                  <th className="px-3 py-2 font-normal">Decision</th>
                  <th className="px-3 py-2 font-normal">Rule</th>
                  <th className="px-3 py-2 font-normal">Target</th>
                  <th className="px-3 py-2 font-normal">Seat</th>
                </tr>
              </thead>
              <tbody>
                {(audit.data?.rows ?? []).map((row) => {
                  const target = auditTarget(row);
                  return (
                    <tr key={row.id} data-testid={`policy-decision-${row.id}`} className="border-t">
                      <td className="whitespace-nowrap px-3 py-2 text-foreground-muted">
                        {formatRelativeTime(row.createdAt)}
                      </td>
                      <td className="px-3 py-2">
                        <ActionBadge value={row.decision} />
                      </td>
                      <td className="px-3 py-2 font-mono text-foreground-muted">{row.ruleId || EM_DASH}</td>
                      <td className="px-3 py-2 text-foreground-muted">
                        <span className="block max-w-xs truncate" title={target.full}>
                          {target.text}
                        </span>
                      </td>
                      <td className="px-3 py-2 text-foreground-muted">{row.seat}</td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  );
}

function ValidityAlert({ status }: { status: PolicyStatus | undefined }) {
  if (!status) return null;
  if (status.valid === false) {
    return <Alert variant="error" testId="policy-validity" title="Policy file is invalid" description={status.error} />;
  }
  if (status.exists === false) {
    return (
      <Alert
        variant="default"
        testId="policy-validity"
        title="No policy file yet"
        description="Every request that is not floor-denied asks you."
      />
    );
  }
  if (status.enabled) return <Alert variant="success" testId="policy-validity" title="Policy active" />;
  return (
    <Alert variant="default" testId="policy-validity" title="Policy file is valid but the engine is off (steward.policy.enabled)" />
  );
}

registerSettingsSection({
  id: "permission-policy",
  label: "Permission policy",
  groupLabel: "Agents & providers",
  description: "Rules that auto-allow or deny agent permission requests. Read-only.",
  order: 195,
  icon: <Icons.permission aria-hidden className="size-3.5" />,
  render: (ctx) => <PolicySection {...ctx} />,
});
