import type { AgentProfile } from "@/lib/types";

/** The daemon's cap on a seat's instructions (internal/profiles/registry.go), in bytes. */
export const INSTRUCTIONS_MAX_BYTES = 16384;

export type OrchestrationToolsChoice = "unmanaged" | "on" | "off";
export type ChildPolicyChoice = "none" | "leaf" | "profiles";
export type OnRateLimitChoice = "none" | "wait" | "fallback";

/** The ADR-0041 seat-contract and failover slice of the profile form's state. */
export interface ProfileContractState {
  orchestrationTools: OrchestrationToolsChoice;
  instructions: string;
  /** Comma-separated bundled skill names; the daemon validates them. */
  skills: string;
  childPolicy: ChildPolicyChoice;
  childProfileIds: number[];
  onRateLimit: OnRateLimitChoice;
  fallbackProfileId: number | null;
}

export const EMPTY_CONTRACT: ProfileContractState = {
  orchestrationTools: "unmanaged",
  instructions: "",
  skills: "",
  childPolicy: "none",
  childProfileIds: [],
  onRateLimit: "none",
  fallbackProfileId: null,
};

export function contractFromProfile(p: AgentProfile): ProfileContractState {
  return {
    orchestrationTools: p.OrchestrationTools == null ? "unmanaged" : p.OrchestrationTools ? "on" : "off",
    instructions: p.Instructions ?? "",
    skills: (p.Skills ?? []).join(", "),
    childPolicy: p.ChildPolicy === "leaf" || p.ChildPolicy === "profiles" ? p.ChildPolicy : "none",
    childProfileIds: p.ChildProfileIDs ?? [],
    onRateLimit: p.OnRateLimit === "wait" || p.OnRateLimit === "fallback" ? p.OnRateLimit : "none",
    fallbackProfileId: p.FallbackProfileID ?? null,
  };
}

export function instructionsBytes(value: string): number {
  return new TextEncoder().encode(value).length;
}

function parseSkills(skills: string): string[] {
  return skills
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean);
}

/** The wire params for the contract slice. profile.update is a full replace, so every field is always sent. */
export function contractParams(c: ProfileContractState) {
  const skills = parseSkills(c.skills);
  // A legacy profile (no contract at all) keeps childPolicy "" so the daemon leaves it legacy.
  const legacy = c.orchestrationTools === "unmanaged" && c.instructions.trim() === "" && skills.length === 0 && c.childPolicy === "none";
  return {
    orchestrationTools: c.orchestrationTools === "unmanaged" ? null : c.orchestrationTools === "on",
    instructions: c.instructions,
    skills,
    childPolicy: legacy ? "" : c.childPolicy,
    childProfileIds: c.childPolicy === "profiles" ? c.childProfileIds : [],
    onRateLimit: c.onRateLimit,
    fallbackProfileId: c.onRateLimit === "fallback" ? c.fallbackProfileId : null,
  };
}
