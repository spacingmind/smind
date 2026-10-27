/**
 * How a failed run's error reads to a person. The daemon stores the raw text
 * ("<agent's own cause> (<transport error chain>)", see runs.joinErrorCause);
 * that text carries two kinds of noise a reader should not meet first: the
 * JSON-RPC / ACP chain ("taskrunner: glm prompt: acp: ... rpc error -32603:
 * Internal error") and, for a provider quota, the provider's own reset time
 * (GLM prints UTC+8 wall-clock time), which disagrees with the local reset
 * the footer shows. Pure; the raw text stays available as `raw`.
 */

export interface RunErrorView {
  /** The plain sentence shown in the error box. */
  message: string;
  /** The stored text, shown only behind a disclosure; null when `message` already is it. */
  raw: string | null;
}

/** The transport chain the daemon appends in parentheses: "(taskrunner: ... rpc error -32603: Internal error)". */
const TRANSPORT_TAIL = /^([\s\S]*?)\s*\(((?:taskrunner:|…|[\s\S]*\brpc error\b)[\s\S]*)\)$/;
const RPC_ERROR = /\brpc error -?\d+:\s*([\s\S]*)$/i;
/** The provider's reset clause, which names a time in the provider's zone. */
const RESET_CLAUSE = /\s*[.,;·-]?\s*(?:your\s+)?(?:limit\s+)?(?:will\s+)?resets?\s+(?:at\s+|in\s+)?\S[\s\S]*$/i;

function plainRpcMessage(inner: string): string {
  const detail = inner.trim().replace(/\.$/, '');
  if (detail === '' || /^internal error$/i.test(detail)) return 'The agent stopped with an internal error.';
  return `The agent failed: ${detail}.`;
}

/** The error box's view of `err`; a rate-limited run drops the provider's reset clause (the footer owns the reset, in local time). */
export function describeRunError(err: string, opts: { rateLimited?: boolean; hasReset?: boolean } = {}): RunErrorView {
  const text = err.trim();
  let message = text;
  const tail = TRANSPORT_TAIL.exec(text);
  if (tail) {
    const cause = tail[1].trim();
    message = cause !== '' ? cause : plainRpcMessage(RPC_ERROR.exec(tail[2])?.[1] ?? '');
  } else if (RPC_ERROR.test(text)) {
    message = plainRpcMessage(RPC_ERROR.exec(text)?.[1] ?? '');
  }
  if (opts.rateLimited) {
    // Z.ai says "for 5 hour"; a reader wants "the 5-hour window".
    const bare = message.replace(/^429\s+/, '').replace(/\bfor (\d+) hours?\b/i, 'for the $1-hour window');
    const stripped = opts.hasReset ? bare.replace(RESET_CLAUSE, '').trim() : bare;
    message = stripped === '' ? "The provider's usage limit was reached." : /[.!?]$/.test(stripped) ? stripped : `${stripped}.`;
  }
  return { message, raw: message === text ? null : text };
}

/** The IANA zone Claude Code prints after a reset time: "resets 1pm (Asia/Saigon)" -- the daemon's reClaudeResetTZ. */
const PRINTED_ZONE = /\bresets?\s+\d{1,2}(?::\d{2})?\s*(?:am|pm)\s*\(([A-Za-z_]+\/[A-Za-z_+\-0-9]+)\)/i;

/**
 * The Details note naming the zone the daemon read the raw text's reset time
 * in; null says nothing (not a rate-limit error, no reset time parsed from
 * the text, or an absolute instant such as an epoch). Mirrors
 * internal/runs/ratelimit: GLM's zone-less wall clock is UTC+8, a Claude time
 * carries its own IANA zone, and the rest was read in the daemon's own zone,
 * which the run does not carry (so it cannot be named here). Kept identical
 * in web/packages/ui/src/lib/run-error.ts and mobile/src/runError.ts.
 */
export function resetZoneNote(raw: string, opts: { provider?: string; rateLimited?: boolean; hasReset: boolean; inferred?: boolean }): string | null {
  if (!opts.rateLimited || !opts.hasReset) return null;
  if (opts.provider === 'glm') return 'As reported by the provider. A time in this text is UTC+8, not your local time.';
  const printed = PRINTED_ZONE.exec(raw)?.[1];
  if (printed) return `As reported by the provider. A time in this text is in ${printed.replace(/_/g, ' ')}, not your local time.`;
  if (opts.inferred) return "As reported by the provider. A time in this text has no zone; it was read in the daemon's time zone.";
  return null;
}

/** The agent's "[error] <cause>" text chunk that the run's error box repeats. */
const ERROR_MARKER = '[error]';

/**
 * Drops the transcript's "[error] <cause>" tail when the run's error box
 * already states it, so a failure reads once. Keeps text the agent wrote
 * before the marker; returns `text` unchanged when there is no marker or its
 * cause is not part of `err` (a different message is real content).
 */
export function stripErrorEcho(text: string, err: string | undefined): string {
  if (!err) return text;
  const at = text.lastIndexOf(ERROR_MARKER);
  if (at < 0) return text;
  const cause = text.slice(at + ERROR_MARKER.length).split('\n')[0].replace(/\s+/g, ' ').trim();
  const flat = err.replace(/\s+/g, ' ');
  if (cause === '' || !flat.includes(cause.slice(0, 120))) return text;
  return text.slice(0, at).trimEnd();
}
