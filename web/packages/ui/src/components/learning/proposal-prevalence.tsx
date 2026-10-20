import type { ProposalPrevalenceSignature } from "@/lib/learning-types";
import { useProposalPrevalence } from "@/hooks/use-learning";

export function prevalenceText(sig: ProposalPrevalenceSignature, windowDays: number): string {
  if (sig.runs === 0) return `Not seen in the last ${windowDays} days`;
  const prefix = sig.truncated ? "at least " : "";
  return `Seen in ${prefix}${sig.runs} ${sig.runs === 1 ? "run" : "runs"} across ${sig.chats} ${sig.chats === 1 ? "chat" : "chats"} (last ${windowDays} days)`;
}

/**
 * Advisory prevalence per signature under the proposal's Evidence list: how often the pattern
 * was seen recently. Quiet by design — nothing while loading, on error, or when the daemon has
 * no signatures — so it never interrupts the review.
 */
export function ProposalPrevalenceLine({ id, pending }: { id: number; pending: boolean }) {
  const prevalence = useProposalPrevalence(pending ? id : null, pending);
  const signatures = prevalence.data?.signatures;
  if (!signatures || signatures.length === 0) return null;
  const windowDays = prevalence.data?.windowDays ?? 0;
  return (
    <div className="flex flex-col gap-0.5" data-testid="proposal-prevalence">
      {signatures.map((sig, i) => (
        <div key={`${sig.kind}:${i}`} className="flex flex-col" data-testid="prevalence-row">
          <p className="text-ui-sm text-foreground-subtle">{prevalenceText(sig, windowDays)}</p>
          <p className="text-ui-sm text-muted-foreground">{sig.label}</p>
        </div>
      ))}
    </div>
  );
}
