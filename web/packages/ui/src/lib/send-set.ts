import { formatBytes } from "@/lib/attachments/format";

/** How much terminal text one send may add to the composer (DT3-C AC12). */
export const SEND_CAP_BYTES = 16 * 1024;

const encoder = new TextEncoder();

/** The text's size in UTF-8 bytes -- the unit both the cap and the shown sizes are stated in. */
export function byteLength(text: string): number {
  return encoder.encode(text).length;
}

/**
 * Keeps the text's first `capBytes` bytes, never splitting a code point (the agent would otherwise
 * receive half an emoji or an accent). `omittedBytes` is what did not fit, so the block can say so.
 */
export function clipText(text: string, capBytes: number = SEND_CAP_BYTES): { text: string; omittedBytes: number } {
  const total = byteLength(text);
  if (total <= capBytes) return { text, omittedBytes: 0 };

  let kept = "";
  let keptBytes = 0;
  for (const ch of text) {
    const size = byteLength(ch);
    if (keptBytes + size > capBytes) break;
    kept += ch;
    keptBytes += size;
  }
  return { text: kept, omittedBytes: total - keptBytes };
}

/**
 * One fenced block under a one-line header: the fence is longer than any run of backticks in the
 * body, so the body cannot close the block early (AC12). One trailing newline is dropped so the
 * closing fence sits on the text's own last line.
 */
export function fenceBlock(header: string, body: string): string {
  const longest = body.match(/`+/g)?.reduce((max, run) => Math.max(max, run.length), 0) ?? 0;
  const fence = "`".repeat(Math.max(3, longest + 1));
  const trimmed = body.endsWith("\n") ? body.slice(0, -1) : body;
  return `${header}\n${fence}\n${trimmed}\n${fence}`;
}

/**
 * The terminal text a "Send to agent" entry adds: clipped to the cap, an explicit "(N KiB omitted)"
 * line when anything was cut, and the size in the header being what actually goes in (AC10/AC12).
 */
export function buildTerminalBlock(body: string, label: string): { block: string; shownBytes: number; omittedBytes: number } {
  const { text, omittedBytes } = clipText(body);
  const shownBytes = byteLength(text);
  const withOmitted = omittedBytes > 0 ? `${text}${text.endsWith("\n") || text === "" ? "" : "\n"}(${formatBytes(omittedBytes)} omitted)\n` : text;
  const header = `Terminal output, ${label}, ${formatBytes(shownBytes)}:`;
  return { block: fenceBlock(header, withOmitted), shownBytes, omittedBytes };
}
