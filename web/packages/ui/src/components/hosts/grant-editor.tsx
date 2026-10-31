import { Checkbox } from "@/components/ui/checkbox";
import { FormError } from "@/components/ui/form-error";
import { Input } from "@/components/ui/input";
import { grantNeedsWriteWorkspace, type FleetGrant } from "@/lib/fleet-types";

const WRITE_OPS: { op: string; label: string }[] = [
  { op: "create", label: "Create tasks" },
  { op: "send", label: "Send messages" },
  { op: "stop", label: "Stop runs" },
];

/** A number field's value: NaN and negatives clamp to 0, so a half-typed field never writes garbage into the grant. */
function clamp(raw: string): number {
  const n = Number(raw);
  return Number.isFinite(n) && n > 0 ? n : 0;
}

/**
 * The grant a host is approved with (ADR-0048 section 1), as a controlled form: which ops it may run, the
 * workspaces it may write in, and the three caps. Read is always on. Read scope and providers are not
 * editable here (they pass through untouched): providers stay "any this host has configured" because the
 * caller names the model itself.
 */
export function GrantEditor({
  grant,
  onChange,
  workspaces,
  disabled = false,
}: {
  grant: FleetGrant;
  onChange: (grant: FleetGrant) => void;
  workspaces: { id: number; title: string }[];
  disabled?: boolean;
}) {
  const toggleOp = (op: string, on: boolean) =>
    onChange({ ...grant, ops: on ? [...grant.ops.filter((o) => o !== op), op] : grant.ops.filter((o) => o !== op) });
  const toggleWorkspace = (id: number, on: boolean) =>
    onChange({
      ...grant,
      workspacesWrite: on ? [...grant.workspacesWrite.filter((w) => w !== id), id] : grant.workspacesWrite.filter((w) => w !== id),
    });

  return (
    <div className="flex flex-col gap-3" data-testid="grant-editor">
      <fieldset className="flex flex-col gap-1" disabled={disabled}>
        <legend className="mb-1 text-ui-sm font-medium text-foreground">Allowed actions</legend>
        <label className="flex items-center gap-2 text-ui-sm text-foreground">
          <Checkbox checked disabled hint="Reading is always allowed" data-testid="grant-op-read" />
          <span>Read workspaces, tasks and run status</span>
        </label>
        {WRITE_OPS.map(({ op, label }) => (
          <label key={op} className="flex items-center gap-2 text-ui-sm text-foreground">
            <Checkbox
              data-testid={`grant-op-${op}`}
              checked={grant.ops.includes(op)}
              onCheckedChange={(on) => toggleOp(op, on === true)}
            />
            <span>{label}</span>
          </label>
        ))}
      </fieldset>

      <fieldset className="flex flex-col gap-1" disabled={disabled}>
        <legend className="mb-1 text-ui-sm font-medium text-foreground">May write in</legend>
        {workspaces.length === 0 ? (
          <p className="text-ui-sm text-muted-foreground">No workspaces yet.</p>
        ) : (
          workspaces.map((ws) => (
            <label key={ws.id} className="flex items-center gap-2 text-ui-sm text-foreground">
              <Checkbox
                data-testid={`grant-ws-${ws.id}`}
                checked={grant.workspacesWrite.includes(ws.id)}
                onCheckedChange={(on) => toggleWorkspace(ws.id, on === true)}
              />
              <span className="truncate">{ws.title}</span>
            </label>
          ))
        )}
        <FormError
          message={
            grantNeedsWriteWorkspace(grant)
              ? "Pick at least one workspace this host may write in, or turn off create, send and stop."
              : null
          }
          size="sm"
          testId="grant-write-required"
          silent
        />
      </fieldset>

      <div className="grid grid-cols-3 items-end gap-2">
        <label className="flex flex-col gap-1 text-ui-sm text-foreground-muted">
          Live agents
          <Input
            type="number"
            min={0}
            inputMode="numeric"
            data-testid="grant-max-agents"
            disabled={disabled}
            value={grant.maxLiveAgents}
            onChange={(e) => onChange({ ...grant, maxLiveAgents: Math.floor(clamp(e.target.value)) })}
          />
        </label>
        <label className="flex flex-col gap-1 text-ui-sm text-foreground-muted">
          Minutes per run
          <Input
            type="number"
            min={0}
            inputMode="numeric"
            data-testid="grant-max-minutes"
            disabled={disabled}
            value={Math.round(grant.maxWallSeconds / 60)}
            onChange={(e) => onChange({ ...grant, maxWallSeconds: Math.floor(clamp(e.target.value)) * 60 })}
          />
        </label>
        <label className="flex flex-col gap-1 text-ui-sm text-foreground-muted">
          USD per 24 h
          <Input
            type="number"
            min={0}
            step="0.5"
            inputMode="decimal"
            data-testid="grant-max-cost"
            disabled={disabled}
            value={grant.maxCostUsd}
            onChange={(e) => onChange({ ...grant, maxCostUsd: clamp(e.target.value) })}
          />
        </label>
      </div>

      <p className="text-ui-sm text-muted-foreground">Providers: any this host has configured (the caller must name the model).</p>
    </div>
  );
}
