const MAX_SUMMARY = 160;

export interface ErrorSummary {
  /** One short line safe to show inline. */
  summary: string;
  /** The full raw message when it says more than `summary`, else null (nothing to disclose). */
  details: string | null;
}

/**
 * Splits a wrapped daemon error into a one-line summary and the raw text.
 *
 * The daemon wraps every layer, so a failed `gh` call arrives as
 * `task.createPr: create pr 1: gh pr create: gh [pr create --base develop
 * --head ... --body ...]: exit status 1: none of the git remotes ... Run
 * `gh auth login`` -- the argv in brackets is noise to a reader, and what
 * they need is the tool's own reason after it. The summary is that reason's
 * first sentence (or, with no argv in the message, the first line); the
 * full text stays available as `details` (git-github-ux G6).
 */
export function summarizeError(raw: string): ErrorSummary {
  const message = raw.trim();
  // The text after the argv's closing bracket, then after a trailing "exit status N:".
  const argvEnd = message.lastIndexOf("]: ");
  const reason = (argvEnd >= 0 ? message.slice(argvEnd + 3) : message).replace(/^exit status \d+:\s*/, "");
  const firstLine = reason.split("\n").find((line) => line.trim() !== "")?.trim() ?? message;
  // First sentence: stop at ". " so a long stderr paragraph keeps only its headline.
  const sentenceEnd = firstLine.search(/\.\s/);
  let summary = sentenceEnd > 0 ? firstLine.slice(0, sentenceEnd + 1) : firstLine;
  if (summary.length > MAX_SUMMARY) summary = `${summary.slice(0, MAX_SUMMARY - 1).trimEnd()}…`;
  return { summary, details: message === summary ? null : message };
}
