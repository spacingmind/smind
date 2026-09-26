import type { ApprovalPolicy, Provider } from "@/lib/types";

/**
 * The one approval-policy vocabulary every surface shares (run-config IA
 * plan: the review found the composer, the agent form, General's defaults
 * and the mid-run control each phrasing the tiers slightly differently).
 *
 * "manual" and "auto-safe" behave identically across every provider (a
 * decider smind installs itself), but "full-access" doesn't -- it hands
 * the provider its own native "auto-approve everything" mechanism, a
 * different mechanism per provider (see internal/taskrunner/runner.go's
 * runClaudeNative/runCodexNative/runACP). The user explicitly rejected
 * one shared generic label for that tier (docs/plans/active/
 * task-move-approval-thinking.md's Context), so its label/help is that
 * provider's own real vocabulary, not smind's own words: Codex's and
 * GLM's copied verbatim from Paseo's real provider metadata, Claude's
 * from Claude Code's own CLI mode name.
 */
export interface ApprovalPolicyInfo {
  id: ApprovalPolicy;
  label: string;
  /** Tooltip copy shown where the consuming surface supports one (SelectItem title). */
  help: string;
}

export const MANUAL_HELP = "Every action needs your approval before it runs.";
export const AUTO_SAFE_HELP =
  "Auto-safe auto-approves allowlisted read-only verification commands (e.g. gofmt, go vet, go test); everything else still needs human approval.";

export const APPROVAL_POLICY_MANUAL: ApprovalPolicyInfo = {
  id: "manual",
  label: "Manual approval",
  help: MANUAL_HELP,
};

export const APPROVAL_POLICY_AUTO_SAFE: ApprovalPolicyInfo = {
  id: "auto-safe",
  label: "Auto-safe",
  help: AUTO_SAFE_HELP,
};

const FULL_ACCESS_BY_PROVIDER: Record<Provider, ApprovalPolicyInfo> = {
  "claude-native": {
    id: "full-access",
    label: "Bypass",
    help: "Skip all permission prompts (use with caution).",
  },
  "codex-native": {
    id: "full-access",
    label: "Full Access",
    help: "Edit files, run commands, and access the network without additional prompts.",
  },
  glm: {
    id: "full-access",
    label: "Bypass all permissions",
    help: "Edits and commands run without prompting.",
  },
  kimi: {
    id: "full-access",
    label: "Bypass all permissions",
    help: "Edits and commands run without prompting.",
  },
};

/** The full-access tier's label/help for a given provider -- the composer's Select, the agent form, anywhere the third tier is offered. */
export function fullAccessPolicy(provider: Provider): ApprovalPolicyInfo {
  return FULL_ACCESS_BY_PROVIDER[provider] ?? FULL_ACCESS_BY_PROVIDER["claude-native"];
}

/** Every tier, in order, as offered at submission time (composer, agent form, General's old defaults control). */
export function approvalPolicies(provider: Provider): ApprovalPolicyInfo[] {
  return [APPROVAL_POLICY_MANUAL, APPROVAL_POLICY_AUTO_SAFE, fullAccessPolicy(provider)];
}

/** Look up a tier's shared label by id, for a surface that has a policy but no provider context (the mid-run control, timeline labels). */
export function approvalPolicyLabel(policy: ApprovalPolicy): string {
  if (policy === "manual") return APPROVAL_POLICY_MANUAL.label;
  if (policy === "auto-safe") return APPROVAL_POLICY_AUTO_SAFE.label;
  return "Full access";
}

/** The two tiers a running task may switch between (see internal/runs.Registry.SetApprovalPolicy). */
export function liveSwitchablePolicies(): ApprovalPolicyInfo[] {
  return [APPROVAL_POLICY_MANUAL, APPROVAL_POLICY_AUTO_SAFE];
}
