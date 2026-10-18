import type { ReactNode } from "react";

import { SkillBody } from "@/components/learning/learned-skill-detail";
import { Alert } from "@/components/ui/alert";
import { formatSimilarity } from "@/lib/learning-format";
import type { LearnedSkillView, ProposalItem } from "@/lib/learning-types";

/** "Possible duplicate of `skill` (87%)": shown for every flagged proposal, learned or human. */
export function DuplicateBanner({ name, similarity, human }: { name: string; similarity: number; human: boolean }) {
  return (
    <Alert
      variant="warning"
      testId="duplicate-banner"
      title={
        <>
          Possible duplicate of <code className="font-mono">{name}</code> ({formatSimilarity(similarity)})
        </>
      }
      description={
        human
          ? "That skill is one you or a plugin wrote: smind never edits it. You can still approve this one as a separate skill, or reject it."
          : "Merge it into the existing skill to keep one, or keep both."
      }
    />
  );
}

function Column({ testId, heading, description, body }: { testId: string; heading: ReactNode; description: string; body: string }) {
  return (
    <section data-testid={testId} className="flex h-full min-w-0 flex-col gap-2 rounded-xl border bg-card p-3">
      <h3 className="text-ui-base font-medium text-foreground">{heading}</h3>
      <p className="text-ui-sm text-muted-foreground">{description}</p>
      <div className="flex min-h-0 flex-1 flex-col [&>pre]:flex-1">
        <SkillBody body={body} testId={`${testId}-body`} />
      </div>
    </section>
  );
}

/**
 * The proposed skill and the learned skill it resembles, side by side: equal-height columns that
 * stack on narrow widths, so the person can see what the two share before choosing.
 */
export function DuplicateCompare({ proposed, existing }: { proposed: ProposalItem; existing: LearnedSkillView }) {
  return (
    <div data-testid="duplicate-compare" className="grid grid-cols-1 items-stretch gap-3 md:grid-cols-2">
      <Column testId="duplicate-proposed" heading={<>Proposed: {proposed.name}</>} description={proposed.description} body={proposed.body} />
      <Column testId="duplicate-existing" heading={<>Existing: {existing.name}</>} description={existing.description} body={existing.body ?? ""} />
    </div>
  );
}
