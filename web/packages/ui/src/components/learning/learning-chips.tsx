import type { ReactNode } from "react";

import { StatusBadge } from "@/components/ui/status-badge";
import { isUnmeasured } from "@/lib/learning-format";
import type { LearnedSkillSource, LearnedSkillView } from "@/lib/learning-types";
import { Icons } from "@/lib/icons";
import { cn } from "@/lib/utils";

/** The quiet neutral pill used across Settings rows (same shell as the MCP transport chip). */
export function Chip({ children, className, testId }: { children: ReactNode; className?: string; testId?: string }) {
  return (
    <span
      data-testid={testId}
      className={cn(
        "inline-flex shrink-0 items-center gap-1 rounded-full border border-border px-1.5 text-ui-sm text-muted-foreground",
        className,
      )}
    >
      {children}
    </span>
  );
}

export function SourceChip({ source }: { source: LearnedSkillSource }) {
  return <Chip testId="learning-source">{source}</Chip>;
}

/** probation / pinned / unmeasured / disabled, in that order; nothing when none apply. */
export function SkillBadges({ skill }: { skill: LearnedSkillView }) {
  return (
    <>
      {skill.probation && (
        <StatusBadge status="warning" data-testid="badge-probation" className="shrink-0 px-1.5 py-0 text-ui-sm font-normal">
          probation
        </StatusBadge>
      )}
      {skill.pinned && (
        <Chip testId="badge-pinned">
          <Icons.pin aria-hidden className="size-3" />
          pinned
        </Chip>
      )}
      {isUnmeasured(skill) && <Chip testId="badge-unmeasured">unmeasured</Chip>}
      {skill.status === "disabled" && <Chip testId="badge-disabled">disabled</Chip>}
    </>
  );
}
