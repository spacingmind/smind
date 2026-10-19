import { RunLink } from "@/components/learning/run-link";
import type { EvidenceKind, ProposalEvidence } from "@/lib/learning-types";

const KIND_LABEL: Record<EvidenceKind, string> = {
  user_message: "You said",
  failure_recovery: "Failure, then recovery",
  permission_denied: "Permission denied",
  loaded_skill_wrong: "A loaded skill was wrong",
};

/** The redacted quotes a skill or proposal came from, each labelled by kind and linked to its run. */
export function EvidenceList({ evidence, workspaceId }: { evidence: ProposalEvidence[]; workspaceId: number }) {
  if (evidence.length === 0) {
    return <p className="text-ui-sm text-muted-foreground">No evidence was recorded.</p>;
  }
  return (
    <ul data-testid="evidence-list" className="flex flex-col gap-2">
      {evidence.map((e, i) => (
        <li key={`${e.runId}:${e.seq}:${i}`} className="flex flex-col gap-0.5" data-testid="evidence-item">
          <span className="flex flex-wrap items-center gap-x-2 text-ui-sm text-foreground-subtle">
            <span data-testid="evidence-kind">{KIND_LABEL[e.kind] ?? e.kind}</span>
            <RunLink runId={e.runId} seq={e.seq} workspaceId={workspaceId} />
          </span>
          <blockquote className="border-l-2 border-border pl-2 text-ui-base text-foreground-muted">{e.quote}</blockquote>
        </li>
      ))}
    </ul>
  );
}
