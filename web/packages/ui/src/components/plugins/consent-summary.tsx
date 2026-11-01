import type { ReactNode } from "react";

import { Alert } from "@/components/ui/alert";
import { StatusBadge } from "@/components/ui/status-badge";
import { Icons } from "@/lib/icons";
import { describeMcp, type PluginChange } from "@/lib/plugin-diff";
import type { PluginConsent } from "@/lib/plugin-types";

/** The text shown wherever a linked plugin is enabled, from the daemon's `linkWarning` ("live directory: ..."), capitalised. */
export function linkWarningText(consent: PluginConsent): string | null {
  const w = consent.linkWarning?.trim();
  if (!w) return null;
  return w.charAt(0).toUpperCase() + w.slice(1);
}

function Row({ label, children, testId }: { label: string; children: ReactNode; testId?: string }) {
  return (
    <div className="grid grid-cols-[7rem_minmax(0,1fr)] gap-2 text-ui-sm" data-testid={testId}>
      <dt className="text-foreground-muted">{label}</dt>
      <dd className="min-w-0 break-words text-foreground">{children}</dd>
    </div>
  );
}

const mono = "font-mono text-ui-xs break-all";

/**
 * What a plugin would do, read before enabling it (ADR-0042 section 2): whether anything runs, the capabilities it asks for,
 * every command it would launch, its hook events, the agents it adds (flagged when one raises the permission mode, skips
 * prompts, gets orchestration tools or may launch children), where it came from and its content hash. The same block backs
 * the detail, the enable confirmation, the install result and an update's Apply.
 */
export function ConsentSummary({ consent, testId = "plugin-consent" }: { consent: PluginConsent; testId?: string }) {
  const linkWarning = linkWarningText(consent);
  const mcp = consent.mcpServers ?? [];
  const acp = consent.acpProviders ?? [];
  const hooks = consent.hookEvents ?? [];
  const profiles = consent.profiles ?? [];
  const skills = consent.skills ?? [];
  const caps = consent.capabilities ?? [];
  return (
    <div className="flex flex-col gap-2" data-testid={testId}>
      {consent.dataOnly ? (
        <p className="text-ui-sm text-foreground-muted" data-testid={`${testId}-data-only`}>
          Nothing runs: this plugin only adds data (MCP servers, skills, agents).
        </p>
      ) : (
        <Alert variant="warning" testId={`${testId}-runs-code`} description="This plugin runs code as you: the commands below start with your files and credentials." />
      )}
      {linkWarning && <Alert variant="warning" testId={`${testId}-link-warning`} description={linkWarning} />}
      <dl className="flex flex-col gap-1.5">
        <Row label="Source">
          <span className="mr-1.5 rounded-full border border-border px-1.5 text-ui-xs text-muted-foreground">{consent.sourceKind}</span>
          <span className={mono}>{consent.source}</span>
        </Row>
        <Row label="Version">{consent.version}</Row>
        <Row label="Content hash">
          <span className={mono} title={consent.hash}>
            {consent.hash}
          </span>
        </Row>
        <Row label="Capabilities" testId={`${testId}-capabilities`}>
          {caps.length ? caps.join(", ") : <span className="text-foreground-muted">none</span>}
        </Row>
        {consent.process && consent.process.length > 0 && (
          <Row label="Process" testId={`${testId}-process`}>
            <span className={mono}>{consent.process.join(" ")}</span>
          </Row>
        )}
        {mcp.length > 0 && (
          <Row label="MCP servers" testId={`${testId}-mcp`}>
            <ul className="flex flex-col gap-0.5">
              {mcp.map((s) => (
                <li key={s.name}>
                  <span className="font-medium">{s.name}</span> <span className={mono}>{describeMcp(s)}</span>
                  {s.env && Object.keys(s.env).length > 0 && <span className="text-foreground-muted"> · env {Object.keys(s.env).join(", ")}</span>}
                </li>
              ))}
            </ul>
          </Row>
        )}
        {acp.length > 0 && (
          <Row label="ACP providers" testId={`${testId}-acp`}>
            <ul className="flex flex-col gap-0.5">
              {acp.map((e) => (
                <li key={e.id}>
                  <span className="font-medium">{e.id}</span> <span className={mono}>{e.command.join(" ")}</span>
                </li>
              ))}
            </ul>
          </Row>
        )}
        {hooks.length > 0 && (
          <Row label="Hook events" testId={`${testId}-hooks`}>
            {hooks.map((h) => `${h.event} (on error: ${h.onError ?? "open"})`).join(", ")}
          </Row>
        )}
        {skills.length > 0 && (
          <Row label="Skills" testId={`${testId}-skills`}>
            {skills.join(", ")}
          </Row>
        )}
        {profiles.length > 0 && (
          <Row label="Agents" testId={`${testId}-profiles`}>
            <ul className="flex flex-col gap-1">
              {profiles.map((p) => {
                const flags = p.escalations ?? [];
                return (
                  <li key={p.name} data-testid={`${testId}-profile-${p.name}`} data-escalating={flags.length > 0}>
                    <span className="font-medium">{p.name}</span> <span className="text-foreground-muted">{p.provider}</span>
                    {flags.length > 0 && (
                      <StatusBadge status="warning" className="ml-1.5 gap-1 px-1.5 py-0" data-testid={`${testId}-escalation-${p.name}`}>
                        <Icons.permissionDanger aria-hidden className="size-3" /> Escalates
                      </StatusBadge>
                    )}
                    {flags.map((f) => (
                      <p key={f} className="text-ui-xs text-warning-text">
                        {f}
                      </p>
                    ))}
                  </li>
                );
              })}
            </ul>
          </Row>
        )}
      </dl>
    </div>
  );
}

/** A pending update's changes, one line each; the lines that grant or run something more are marked. */
export function ChangeList({ changes, testId = "plugin-changes" }: { changes: PluginChange[]; testId?: string }) {
  if (changes.length === 0) return <p className="text-ui-sm text-foreground-muted">Declares the same things as the current version.</p>;
  const sign = { added: "+", removed: "−", changed: "~" } as const;
  return (
    <ul className="flex flex-col gap-0.5" data-testid={testId}>
      {changes.map((c, i) => (
        <li key={`${c.what}-${i}`} className="flex gap-1.5 text-ui-sm" data-kind={c.kind} data-runs={c.runs}>
          <span aria-hidden className="w-3 shrink-0 text-foreground-muted">
            {sign[c.kind]}
          </span>
          <span className="min-w-0 break-words">
            <span className="text-foreground-muted">{c.what}: </span>
            <span className={c.runs ? "text-warning-text" : "text-foreground"}>{c.text}</span>
          </span>
        </li>
      ))}
    </ul>
  );
}
