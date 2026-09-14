import { ToolPayload, stringifyToolPayload } from "@/components/timeline/tool-call-card";
import { toolInput, type ToolIntent } from "@/components/timeline/tool-renderers";
import type { TimelineToolCallItem } from "@/hooks/use-run-timeline";

/** A tool result rendered as text, whatever the provider wrapped it in. */
export function toolResultText(item: TimelineToolCallItem): string {
  const result = item.result;
  if (typeof result === "string") return result;
  // ACP wraps content in `[{type: "content", content: {type: "text", text}}]`;
  // Claude's tool_result content is usually a similar block array. Pull
  // the text out where the shape allows and fall back to JSON otherwise,
  // rather than showing a person a serialized envelope.
  if (Array.isArray(result)) {
    const texts = result
      .map((block) => {
        if (typeof block === "string") return block;
        if (block && typeof block === "object") {
          const b = block as Record<string, unknown>;
          if (typeof b.text === "string") return b.text;
          const inner = b.content;
          if (inner && typeof inner === "object" && typeof (inner as Record<string, unknown>).text === "string") {
            return (inner as Record<string, unknown>).text as string;
          }
        }
        return null;
      })
      .filter((t): t is string => t !== null);
    if (texts.length > 0) return texts.join("\n");
  }
  return stringifyToolPayload(result);
}

/** Counts the hits a search-shaped result reports, or null when it can't tell. */
export function countHits(text: string): number | null {
  const trimmed = text.trim();
  if (trimmed === "") return null;
  const explicit = /^Found (\d+)/i.exec(trimmed);
  if (explicit) return Number(explicit[1]);
  if (/^No (matches|files) found/i.test(trimmed)) return 0;
  return trimmed.split("\n").length;
}

/**
 * The intent-specific card body. Each intent renders the one thing that
 * distinguishes it (Item 9): a terminal call's command line and output, a
 * read's path and range, an edit's inline diff, a search's query and hit
 * count. Anything it can't say better than the raw payload falls through
 * to the generic input/result dump.
 */
export function ToolCallDetail({ item, intent }: { item: TimelineToolCallItem; intent: ToolIntent }) {
  const input = toolInput(item);
  const output = toolResultText(item);

  if (intent === "terminal") {
    const command = typeof input.command === "string" ? input.command : typeof input.cmd === "string" ? input.cmd : "";
    return (
      <>
        {command && <ToolPayload label="Command" body={command} testId="tool-detail-command" />}
        {output && <ToolPayload label="Output" body={output} testId="tool-detail-output" />}
        {!command && !output && <GenericBody item={item} />}
      </>
    );
  }

  if (intent === "edit") {
    const diff = inlineDiff(input);
    if (diff.length > 0) {
      return (
        <div data-testid="tool-detail-diff" className="overflow-x-auto rounded bg-surface-2 p-2 font-mono text-[0.7rem]">
          {diff.map((line, index) => (
            <div
              key={index}
              data-diff-sign={line.sign}
              className={
                line.sign === "+"
                  ? "text-status-success"
                  : line.sign === "-"
                    ? "text-status-danger"
                    : "text-foreground-muted"
              }
            >
              {line.sign}
              {line.text}
            </div>
          ))}
        </div>
      );
    }
    return <GenericBody item={item} />;
  }

  if (intent === "search") {
    const hits = countHits(output);
    return (
      <>
        {hits !== null && (
          <p data-testid="tool-detail-hits" className="text-foreground-muted">
            {hits} {hits === 1 ? "hit" : "hits"}
          </p>
        )}
        {output && <ToolPayload label="Matches" body={output} testId="tool-detail-output" />}
        {!output && <GenericBody item={item} />}
      </>
    );
  }

  if (intent === "read" || intent === "fetch") {
    return output ? <ToolPayload label="Content" body={output} testId="tool-detail-output" /> : <GenericBody item={item} />;
  }

  return <GenericBody item={item} />;
}

function GenericBody({ item }: { item: TimelineToolCallItem }) {
  const input = stringifyToolPayload(item.input);
  const result = stringifyToolPayload(item.result);
  if (!input && !result) return <p className="text-foreground-muted">No detail recorded</p>;
  return (
    <>
      {input && <ToolPayload label="Input" body={input} testId="tool-call-input" />}
      {result && <ToolPayload label="Result" body={result} testId="tool-call-result" />}
    </>
  );
}

export interface DiffLine {
  sign: "+" | "-" | " ";
  text: string;
}

/**
 * A minimal inline diff of an edit's before/after text. Deliberately
 * whole-block rather than line-matched: the daemon forwards the edit's
 * own arguments (`old_string`/`new_string`, or a `content` write), not a
 * computed patch, and inventing an LCS here would be a second, worse
 * diff engine next to the real one the diff pane already uses.
 */
export function inlineDiff(input: Record<string, unknown>): DiffLine[] {
  const before = firstString(input, "old_string", "oldText", "old_text");
  const after = firstString(input, "new_string", "newText", "new_text", "content", "contents");
  if (before === undefined && after === undefined) return [];

  const lines: DiffLine[] = [];
  if (before !== undefined) for (const text of before.split("\n")) lines.push({ sign: "-", text });
  if (after !== undefined) for (const text of after.split("\n")) lines.push({ sign: "+", text });
  return lines;
}

function firstString(input: Record<string, unknown>, ...names: string[]): string | undefined {
  for (const name of names) {
    const value = input[name];
    if (typeof value === "string") return value;
  }
  return undefined;
}
