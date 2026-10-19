import { Chip } from "@/components/learning/learning-chips";
import { SkillBody } from "@/components/learning/learned-skill-detail";
import { DiffRender } from "@/components/diff-render";
import type { LearnedSkillDetail, ProposalItem } from "@/lib/learning-types";
import { applyPatch, unifiedDiff } from "@/lib/text-diff";

/** The current body of a skill detail: its current version's body. */
export function currentBody(detail: LearnedSkillDetail): string {
  return detail.versions.find((v) => v.version === detail.skill.version)?.body ?? detail.skill.body ?? "";
}

/**
 * Why a patch cannot land as proposed, or null: the target moved past the version the proposal
 * was made against (`baseVersion`), is gone, or no longer contains the text being replaced.
 */
export function staleReason(item: ProposalItem, target: LearnedSkillDetail | undefined, targetMissing: boolean): string | null {
  if (item.action !== "patch" || item.targetSkillId === undefined) return null;
  if (targetMissing) return `${item.name} no longer exists.`;
  if (!target) return null;
  if (item.baseVersion !== undefined && target.skill.version !== item.baseVersion) {
    return `${item.name} changed since this was proposed (it is at version ${target.skill.version}, the proposal was made against version ${item.baseVersion}).`;
  }
  if (applyPatch(currentBody(target), item.oldString, item.newString) === null) {
    return `The text this change replaces is no longer in ${item.name}.`;
  }
  return null;
}

/** One create / patch / delete of a proposal, rendered for review. */
export function ProposalItemView({ item, target }: { item: ProposalItem; target?: LearnedSkillDetail }) {
  if (item.action === "delete") {
    return (
      <li data-testid="proposal-item-delete" className="text-ui-base text-foreground-muted">
        <span className="font-medium text-foreground">{item.name}</span>
        {item.absorbedInto ? ` is archived, merged into ${item.absorbedInto}` : " is archived"}
        {item.reason ? ` — ${item.reason}` : ""}
      </li>
    );
  }
  if (item.action === "create") {
    return (
      <div data-testid="proposal-item-create" className="flex flex-col gap-2">
        <span className="flex flex-wrap items-center gap-2">
          <span className="text-ui-base font-medium text-foreground">{item.name}</span>
          {item.category && <Chip>{item.category}</Chip>}
        </span>
        <p className="text-ui-base text-foreground-muted">{item.description}</p>
        <SkillBody body={item.body} testId="proposal-body" />
      </div>
    );
  }
  // patch: the diff of the target's current body against the body with the change applied.
  const current = target ? currentBody(target) : null;
  const patched = current === null ? null : applyPatch(current, item.oldString, item.newString);
  const diff =
    current !== null && patched !== null
      ? unifiedDiff(current, patched, `${item.name}.md`)
      : unifiedDiff(item.oldString, item.newString, `${item.name}.md`);
  return (
    <div data-testid="proposal-item-patch" className="flex flex-col gap-2">
      <span className="text-ui-base font-medium text-foreground">Change {item.name}</span>
      {patched === null && current !== null && (
        <p className="text-ui-sm text-muted-foreground">Showing only the replaced text; it is not in the current body.</p>
      )}
      <div data-testid="proposal-diff">
        <DiffRender diff={diff} outputFormat="line-by-line" />
      </div>
    </div>
  );
}
