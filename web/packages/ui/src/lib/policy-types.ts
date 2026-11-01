/**
 * The `policy.*` wire shapes (ADR-0047 §2), mirroring internal/wsapi/policy.go field for field:
 * `policyStatusView` / `policyRuleView` for `policy.get`, and the `policy.audit` handler's row map.
 */

/** `policy.get`'s answer: the engine's switches plus every rule loaded from the human-edited file. */
export interface PolicyStatus {
  enabled?: boolean;
  valid?: boolean;
  exists?: boolean;
  error?: string;
  path?: string;
  sha?: string;
  rules: PolicyRule[];
}

/** One rule of `steward/policy.yaml` (policyRuleView); the index signature tolerates loader additions. */
export interface PolicyRule {
  id: string;
  action: string;
  why: string;
  seats?: string[];
  workspaces?: string[];
  tool?: string;
  argv0?: string;
  subcommand?: string;
  [k: string]: unknown;
}

/** One `policy.audit` row: every evaluation writes one, `ask` included (ADR-0047 §2.3). */
export interface PolicyAuditRow {
  id: number;
  createdAt: string;
  runId: string;
  taskId: number;
  requestId: string;
  seat: string;
  workspaceId: number;
  tool: string;
  target: string;
  paths: string[];
  decision: string;
  ruleId: string;
  policySha: string;
  reasons: string;
  latencyMs: number;
}
