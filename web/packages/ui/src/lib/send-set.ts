import { formatBytes } from "@/lib/attachments/format";
import type { MentionRef } from "@/lib/file-mention";

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

/** Which terminal text a chip holds: the live selection or the scrollback tail (DT3-D AC14). */
export type TerminalBlockLabel = "selection" | "last 100 lines";

/**
 * What a terminal context chip shows (DT3-D AC14): the line count of the held text and the size that
 * will actually go in (the clipped size), with "(cut)" when the cap dropped anything.
 */
export function terminalChipLabel(body: string, blockLabel: TerminalBlockLabel): string {
  const n = body.split("\n").length;
  const { shownBytes, omittedBytes } = buildTerminalBlock(body, blockLabel);
  return `${blockLabel === "selection" ? "Terminal selection" : "Terminal output"} · ${n} ${n === 1 ? "line" : "lines"} · ${formatBytes(shownBytes)}${omittedBytes > 0 ? " (cut)" : ""}`;
}

/**
 * What a file-reference context chip shows: the basename, the line range when it has one (`L3` alone or
 * `L1–3`), and the referred text's size. The range dash is an EN DASH (U+2013), like the editor's labels.
 */
export function fileRefChipLabel(ref: MentionRef, bytes: number): string {
  const name = ref.path.slice(ref.path.lastIndexOf("/") + 1);
  const range = ref.line ? ` L${ref.line}${ref.endLine && ref.endLine > ref.line ? `–${ref.endLine}` : ""}` : "";
  return `${name}${range} · ${formatBytes(bytes)}`;
}

/**
 * One item of the composer's context chip tray (DT3-D AC14): a terminal text held out of the draft (its
 * fenced block is built only at send time) or a file reference sent as a `file_ref` attachment.
 */
export type ContextChip = { localId: string; label: string } & (
  | { kind: "terminal"; body: string; blockLabel: TerminalBlockLabel }
  | { kind: "file_ref"; ref: MentionRef }
);

/** A chip without its store-assigned id: what `addChip` takes. Omit must distribute over the kinds -- a plain `Omit<ContextChip, "localId">` collapses the union. */
type ChipWithoutId<T> = T extends { localId: string } ? Omit<T, "localId"> : never;
export type ContextChipInput = ChipWithoutId<ContextChip>;

/**
 * The outgoing prompt (DT3-D AC15): the typed text followed by one fenced block per terminal chip, in the
 * order they were added, each after a blank line. File chips become attachments, not text, so they add
 * nothing here; with no terminal chips the text goes out exactly as typed.
 */
export function appendChipBlocks(text: string, chips: readonly ContextChip[]): string {
  const blocks = chips
    .filter((chip): chip is Extract<ContextChip, { kind: "terminal" }> => chip.kind === "terminal")
    .map((chip) => buildTerminalBlock(chip.body, chip.blockLabel).block);
  if (blocks.length === 0) return text;
  return `${text.trimEnd()}\n\n${blocks.join("\n\n")}`;
}
