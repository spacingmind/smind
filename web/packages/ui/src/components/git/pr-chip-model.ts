import type { PrStale, TaskGitPr } from "@/lib/types";

export interface PrChipModel {
  label: string;
  tone: "neutral" | "danger";
  detail: string;
}

const STALE_SENTENCES: Partial<Record<PrStale, string>> = {
  budget: "GitHub refresh is rate-limited; showing the last known state.",
  backoff: "GitHub isn't answering; showing the last known state.",
  rate_limited: "GitHub is rate-limiting requests; showing the last known state.",
  unauthenticated: "GitHub isn't signed in on the daemon host; showing the last known state.",
  unavailable: "GitHub is unavailable; showing the last known state.",
};

/** Derives the PR chip's label, tone and tooltip text from a TaskGitPr snapshot. Every nested field may be absent without crashing. */
export function prChipModel(pr: TaskGitPr, nowMs: number): PrChipModel {
  const state: string =
    pr.state === "merged" ? "Merged" : pr.state === "closed" ? "Closed" : pr.draft ? "Draft" : "Open";

  let segment = "";
  if (pr.state === "open") {
    const failed = pr.checks?.failed ?? 0;
    if (failed > 0) segment = `✗ ${failed} failing`;
    else if (pr.reviewDecision === "changes_requested") segment = "Changes requested";
    else if (pr.mergeable === "conflicting") segment = "Conflicts";
    else if (pr.checks?.state === "pending") segment = "checks running";
    else if (pr.checks?.state === "success") segment = "✓ checks";
  }

  const danger = segment === "Changes requested" || segment === "Conflicts" || (pr.checks?.failed ?? 0) > 0;
  const tone: "neutral" | "danger" = danger && pr.state === "open" ? "danger" : "neutral";

  let label = segment ? `#${pr.number} · ${state} · ${segment}` : `#${pr.number} · ${state}`;
  // A snapshot the daemon could not refresh says how old it is (never by tooltip alone).
  const fetchedAt = Date.parse(pr.fetchedAt ?? "");
  if (pr.stale && !Number.isNaN(fetchedAt)) {
    label += ` · ${Math.max(1, Math.round((nowMs - fetchedAt) / 60000))} min ago`;
  }

  const parts: string[] = [`${clip(pr.title, 120)} — ${pr.repo ?? ""}`];
  if (!Number.isNaN(fetchedAt) && nowMs - fetchedAt > 2 * 60 * 1000) {
    parts.push(`updated ${Math.max(1, Math.round((nowMs - fetchedAt) / 60000))} min ago`);
  }
  const staleSentence = pr.stale ? STALE_SENTENCES[pr.stale] : undefined;
  if (staleSentence) parts.push(staleSentence);

  return { label, tone, detail: parts.join(" · ") };
}

function clip(text: string | undefined, max: number): string {
  const t = text ?? "";
  return t.length > max ? `${t.slice(0, max - 1)}…` : t;
}
