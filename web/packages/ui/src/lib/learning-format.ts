import type { LearnedSkillView, ProposalView } from "@/lib/learning-types";
import { EM_DASH } from "@/lib/usage-format";

/** "60%" for a 0..1 ratio; an em dash when there is nothing to measure yet. */
export function formatPercent(ratio: number, measured = true): string {
  return measured && Number.isFinite(ratio) ? `${Math.round(ratio * 100)}%` : EM_DASH;
}

/** A similarity score (0..1) as "87%". */
export function formatSimilarity(similarity: number): string {
  return `${Math.round(similarity * 100)}%`;
}

/** Offered but never measured: the skill has been shown to runs, with too few to compute a load rate. */
export function isUnmeasured(skill: LearnedSkillView): boolean {
  return !skill.measured && skill.offeredRuns > 0;
}

/** One line describing what a proposal would do, for its row. */
export function proposalSummary(p: ProposalView): string {
  if (p.malformed) return "Could not be read; reject it to clear it.";
  if (p.kind === "bundle") {
    const archived = p.items.filter((i) => i.action === "delete").length;
    const target = p.items.find((i) => i.action !== "delete");
    const into = target ? ` into ${target.name}` : "";
    return `Merge ${archived} skill${archived === 1 ? "" : "s"}${into}`;
  }
  const first = p.items[0];
  if (!first) return "";
  return p.kind === "patch" ? first.reason || `Change ${first.name}` : first.description;
}

/** Counts of non-blank lines, for the 25-line body limit. */
export function nonBlankLines(text: string): number {
  return text.split("\n").filter((line) => line.trim() !== "").length;
}
