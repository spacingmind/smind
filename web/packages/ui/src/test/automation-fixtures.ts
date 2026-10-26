import type { AutomationAuditRow, AutomationListing, AutomationView } from "@/lib/automation-types";
import type { AgentProfile, Workspace } from "@/lib/types";
import { RpcError } from "@/lib/ws-client";
import { FakeUsageDaemon } from "@/test/usage-test-client";

export function automation(over: Partial<AutomationView> = {}): AutomationView {
  return {
    id: 1,
    name: "Nightly triage",
    enabled: true,
    workspaceId: 7,
    signal: { kind: "cron", cron: "0 3 * * *" },
    template: {
      profileId: 3,
      model: "haiku",
      mode: "default",
      titleTemplate: "Triage {{signal.scheduledAt}}",
      promptTemplate: "Look at the repo.",
      budgetUsd: 3,
      maxRunMinutes: 45,
    },
    rails: { maxRunsPerHour: 2, dedupeWindowMinutes: 0, pauseAfterFailures: 3, memoryEnabled: true, memoryMaxBytes: 16384 },
    status: "active",
    pausedReason: "",
    pausedAt: null,
    consecutiveFailures: 0,
    budgetBlocked: false,
    lastFiredAt: null,
    nextRunAt: "2026-10-28T03:00:00Z",
    nextRuns: ["2026-10-28T03:00:00Z", "2026-10-29T03:00:00Z", "2026-10-30T03:00:00Z"],
    memoryEnabled: true,
    createdAt: "2026-10-26T00:00:00Z",
    updatedAt: "2026-10-26T00:00:00Z",
    ...over,
  };
}

export function ciAutomation(over: Partial<AutomationView> = {}): AutomationView {
  return automation({
    id: 2,
    name: "CI fixer",
    signal: { kind: "github.workflow_run", workflow: "ci", branch: "develop", conclusions: ["failure"] },
    rails: { maxRunsPerHour: 2, dedupeWindowMinutes: 1440, pauseAfterFailures: 3, memoryEnabled: false, memoryMaxBytes: 16384 },
    memoryEnabled: false,
    nextRunAt: null,
    nextRuns: undefined,
    ...over,
  });
}

export function auditRow(over: Partial<AutomationAuditRow> = {}): AutomationAuditRow {
  return {
    id: 1,
    signalKind: "cron",
    fingerprint: "fp",
    decision: "fire",
    reason: "",
    manual: false,
    catchup: false,
    taskId: null,
    runId: "",
    outcome: "",
    note: "",
    createdAt: "2026-10-27T03:00:00Z",
    finishedAt: null,
    ...over,
  };
}

export const forbidden = (message: string) => new RpcError(message, "forbidden");

export const WORKSPACE: Workspace = {
  ID: 7,
  Path: "/work/smind",
  Title: "smind",
  RoutingPolicy: "",
  CreatedAt: "2026-10-01T00:00:00Z",
  UpdatedAt: "2026-10-01T00:00:00Z",
};

export const PROFILE: AgentProfile = {
  ID: 3,
  Name: "Triage agent",
  Provider: "claude-native",
  PermissionMode: "",
  AutoAccept: false,
  ThinkingLevel: "",
  Notes: "",
  CreatedAt: "2026-10-01T00:00:00Z",
  UpdatedAt: "2026-10-01T00:00:00Z",
};

/**
 * A FakeUsageDaemon that also answers the automation surface: automation.list / .audit / .memory.get from mutable
 * fields, workspace.list for the picker, and every mutation recorded in `calls` and answered by `onMutation` (default:
 * the daemon's own behaviour on the held list). Refuse a mutation with `refuse` (applies to every method except
 * those named in `refuseOnly`, when set).
 */
export class AutomationDaemon extends FakeUsageDaemon {
  listing: AutomationListing = { enabled: true, paused: false, automations: [] };
  audit: AutomationAuditRow[] = [];
  memory = "";
  refuse: RpcError | null = null;
  /** Methods the refusal applies to; empty = every mutation. */
  refuseOnly: string[] = [];
  onMutation: Record<string, (params: Record<string, unknown>) => unknown> = {};

  constructor() {
    super([]);
    this.profiles = [PROFILE];
    const inner = (this.client as unknown as { call: (m: string, p?: unknown) => Promise<unknown> }).call;
    (this.client as unknown as { call: (m: string, p?: unknown) => Promise<unknown> }).call = async (method, params) => {
      if (method === "workspace.list") return [WORKSPACE];
      if (method === "space.list" || method === "task.list") return [];
      if (!method.startsWith("automation.")) return inner.call(this.client, method, params);
      this.calls.push({ method, params });
      const p = (params ?? {}) as Record<string, unknown>;
      switch (method) {
        case "automation.list":
          return this.listing;
        case "automation.audit": {
          const before = typeof p.before === "number" && p.before > 0 ? p.before : Infinity;
          const limit = typeof p.limit === "number" && p.limit > 0 ? p.limit : 50;
          return { rows: this.audit.filter((r) => r.id < before).sort((a, b) => b.id - a.id).slice(0, limit) };
        }
        case "automation.memory.get":
          return { memory: this.memory };
      }
      if (this.refuse && (this.refuseOnly.length === 0 || this.refuseOnly.includes(method))) throw this.refuse;
      const custom = this.onMutation[method];
      if (custom) return custom(p);
      const found = this.listing.automations.find((a) => a.id === p.id);
      switch (method) {
        case "automation.create":
          return automation({ ...(p as object), id: 99, enabled: false } as Partial<AutomationView>);
        case "automation.update":
          return { ...found, ...(p as object) };
        case "automation.enable":
          return { ...found, enabled: true };
        case "automation.disable":
          return { ...found, enabled: false };
        case "automation.resume":
          return { ...found, status: "active", pausedReason: "" };
        case "automation.fire":
          return auditRow({ id: 1000, manual: true, decision: "fire", taskId: 55 });
        case "automation.memory.clear":
          this.memory = "";
          return {};
        default:
          return {};
      }
    };
  }
}
