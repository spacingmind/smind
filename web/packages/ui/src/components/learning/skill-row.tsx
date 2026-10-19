import type { ReactNode } from "react";

import { Chip, SkillBadges, SourceChip } from "@/components/learning/learning-chips";
import { formatPercent, formatSimilarity, proposalSummary } from "@/lib/learning-format";
import type { LearnedSkillView, ProposalView } from "@/lib/learning-types";
import { formatRelativeTime } from "@/lib/relative-time";
import { cn } from "@/lib/utils";

/** The shared row shell: one full-width button, so a row is keyboard-reachable and opens on Enter/Space. */
function RowShell({
  testId,
  label,
  onOpen,
  dim,
  selected,
  children,
}: {
  testId: string;
  label: string;
  onOpen?: () => void;
  dim?: boolean;
  selected?: boolean;
  children: ReactNode;
}) {
  return (
    <li>
      <button
        type="button"
        data-testid={testId}
        aria-label={label}
        aria-current={selected || undefined}
        onClick={onOpen}
        className={cn(
          "flex w-full flex-col gap-0.5 rounded-lg px-2 py-1.5 text-left outline-none hover:bg-hover focus-visible:bg-hover focus-visible:ring-2 focus-visible:ring-ring/50",
          selected && "bg-selected",
          dim && "opacity-60",
        )}
      >
        {children}
      </button>
    </li>
  );
}

/** A learned skill: name, source, badges, the description and the load counters. */
export function LearnedSkillRow({
  skill,
  selected,
  onOpen,
}: {
  skill: LearnedSkillView;
  selected?: boolean;
  onOpen?: () => void;
}) {
  return (
    <RowShell
      testId={`learned-skill-row-${skill.id}`}
      label={skill.name}
      onOpen={onOpen}
      selected={selected}
      dim={skill.status !== "active"}
    >
      <span className="flex min-w-0 items-center gap-2">
        <span className="truncate text-ui-base font-medium text-foreground">{skill.name}</span>
        <SourceChip source={skill.source} />
        {skill.category && <Chip>{skill.category}</Chip>}
        <SkillBadges skill={skill} />
      </span>
      <span className="truncate text-ui-sm text-muted-foreground">{skill.description}</span>
      <span data-testid={`learned-skill-counters-${skill.id}`} className="text-ui-sm tabular-nums text-foreground-subtlest">
        {skill.offeredRuns} offered · {skill.loadedRuns} loaded · {formatPercent(skill.loadRate, skill.measured)}
        {skill.lastLoadedAt ? ` · last loaded ${formatRelativeTime(skill.lastLoadedAt)}` : ""}
      </span>
    </RowShell>
  );
}

/** A pending proposal: name, kind, source, a duplicate flag and what it would do. */
export function ProposalRow({
  proposal,
  selected,
  onOpen,
}: {
  proposal: ProposalView;
  selected?: boolean;
  onOpen?: () => void;
}) {
  return (
    <RowShell testId={`proposal-row-${proposal.id}`} label={proposal.name} onOpen={onOpen} selected={selected}>
      <span className="flex min-w-0 items-center gap-2">
        <span className="truncate text-ui-base font-medium text-foreground">{proposal.name}</span>
        <Chip testId="proposal-kind">{proposal.kind}</Chip>
        <SourceChip source={proposal.source} />
        {proposal.possibleDuplicateOf && (
          <Chip testId="proposal-duplicate">duplicate? {formatSimilarity(proposal.similarity)}</Chip>
        )}
      </span>
      <span className="truncate text-ui-sm text-muted-foreground">{proposalSummary(proposal)}</span>
      <span className="text-ui-sm text-foreground-subtlest">proposed {formatRelativeTime(proposal.createdAt)}</span>
    </RowShell>
  );
}
