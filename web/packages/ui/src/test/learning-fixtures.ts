import type {
  LearnedSkillView,
  ProposalEvidence,
  ProposalItem,
  ProposalView,
} from "@/lib/learning-types";

let nextId = 100;

export function skill(over: Partial<LearnedSkillView> = {}): LearnedSkillView {
  const id = over.id ?? ++nextId;
  return {
    id,
    workspaceId: 1,
    name: `skill-${id}`,
    category: "workflow",
    description: "Run the focused test before the whole suite.",
    status: "active",
    pinned: false,
    source: "reflector",
    createdByRun: "run-7",
    possibleDuplicateOf: "",
    similarity: 0,
    version: 1,
    offeredRuns: 10,
    loadedRuns: 6,
    loads: 8,
    loadsViaSkill: 5,
    loadsViaRead: 3,
    patched: false,
    reusedAfterPatch: false,
    measured: true,
    probation: false,
    loadRate: 0.6,
    lastOfferedAt: "2026-10-17T10:00:00Z",
    lastLoadedAt: "2026-10-17T10:00:00Z",
    createdAt: "2026-10-10T10:00:00Z",
    updatedAt: "2026-10-17T10:00:00Z",
    ...over,
  };
}

export function item(over: Partial<ProposalItem> = {}): ProposalItem {
  return {
    action: "create",
    name: "run-focused-tests",
    category: "workflow",
    description: "Run one package's tests before the whole suite.",
    body: "# Focused tests\n\nRun `go test ./pkg/...` first.\n",
    oldString: "",
    newString: "",
    absorbedInto: "",
    reason: "The user asked twice to run only the failing package.",
    ...over,
  };
}

export const evidence: ProposalEvidence[] = [
  { kind: "user_message", runId: "run-7", seq: 12, quote: "just run the one package, not everything" },
  { kind: "failure_recovery", runId: "run-7", seq: -1, quote: "go test ./... timed out; go test ./pkg passed" },
];

export function proposal(over: Partial<ProposalView> = {}): ProposalView {
  const id = over.id ?? ++nextId;
  return {
    id,
    workspaceId: 1,
    kind: "create",
    source: "reflector",
    name: "run-focused-tests",
    status: "pending",
    note: "",
    possibleDuplicateOf: "",
    similarity: 0,
    createdAt: "2026-10-17T10:00:00Z",
    items: [item()],
    evidence,
    ...over,
  };
}
