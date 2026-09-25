import { useEffect, useRef, useState } from "react";

import { ToolPayload, stringifyToolPayload } from "@/components/timeline/tool-call-card";
import { ToolReadPreview } from "@/components/timeline/tool-read-preview";
import { num, toolInput, type ToolIntent } from "@/components/timeline/tool-renderers";
import type { TimelineToolCallItem } from "@/hooks/use-run-timeline";
import { cn } from "@/lib/utils";

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

/** Lines a card shows before collapsing long content behind a toggle -- terminal output and a search's match list share the same rhythm. */
const OUTPUT_PREVIEW_LINES = 6;
const OUTPUT_COLLAPSE_LINES = 12;
const OUTPUT_COLLAPSE_CHARS = 800;
const MATCH_PREVIEW_LINES = 20;

/**
 * The intent-specific card body. Each intent renders the one thing that
 * distinguishes it (Item 9): a terminal call's command line and output, a
 * read's path and range, an edit's inline diff, a search's query and hit
 * count. Anything it can't say better than the raw payload falls through
 * to the generic IN/OUT dump.
 */
export function ToolCallDetail({ item, intent }: { item: TimelineToolCallItem; intent: ToolIntent }) {
  const input = toolInput(item);
  const output = toolResultText(item);

  if (intent === "terminal") {
    const command = typeof input.command === "string" ? input.command : typeof input.cmd === "string" ? input.cmd : "";
    return (
      <>
        {command && <TerminalCommand body={command} />}
        {output && <TerminalOutput output={output} failed={item.status === "failure"} />}
        {!command && !output && <GenericBody item={item} />}
      </>
    );
  }

  if (intent === "edit") {
    const diff = inlineDiff(input);
    if (diff.length > 0) {
      return (
        <div data-testid="tool-detail-diff" className="overflow-x-auto rounded bg-surface p-2 font-mono text-ui-sm font-medium">
          {diff.map((line, index) => (
            <div
              key={index}
              data-diff-sign={line.sign}
              className={
                line.sign === "+"
                  ? "text-success"
                  : line.sign === "-"
                    ? "text-destructive"
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
        {output && <SearchMatches output={output} />}
        {!output && <GenericBody item={item} />}
      </>
    );
  }

  if (intent === "read") {
    if (!output) return <GenericBody item={item} />;
    const startLine = num(input, "offset", "start_line") ?? 1;
    return (
      <div className="mt-1 first:mt-0">
        <p className="text-ui-sm font-medium tracking-wide text-foreground-muted uppercase">Content</p>
        <ToolReadPreview content={output} startLine={startLine} testId="tool-detail-output" />
      </div>
    );
  }

  if (intent === "fetch") {
    return output ? <ToolPayload label="Content" body={output} testId="tool-detail-output" /> : <GenericBody item={item} />;
  }

  return <GenericBody item={item} />;
}

/** The command line of a terminal call, styled to match the terminal pane's own chrome (`lib/terminal-theme.ts`) rather than the generic embedded-well look every other payload uses. */
function TerminalCommand({ body }: { body: string }) {
  return (
    <div className="mt-1 first:mt-0">
      <p className="text-ui-sm font-medium tracking-wide text-foreground-muted uppercase">Command</p>
      <pre
        data-testid="tool-detail-command"
        className="mt-0.5 overflow-x-auto rounded border bg-background p-2 font-mono text-foreground whitespace-pre-wrap"
      >
        {body}
      </pre>
    </div>
  );
}

/**
 * A terminal call's output: collapsed behind a "Show output" toggle once
 * it's long, so a `find`/`go test -v` dump doesn't push the rest of the
 * transcript down by default. A failed call skips the collapse entirely
 * and scrolls its own bounded region to the bottom on mount, so the
 * actionable last line (the error) is on screen without the person
 * needing to know to scroll for it.
 */
function TerminalOutput({ output, failed }: { output: string; failed: boolean }) {
  const [expanded, setExpanded] = useState(failed);
  const preRef = useRef<HTMLPreElement>(null);
  const lines = output.split("\n");
  const long = !failed && (lines.length > OUTPUT_COLLAPSE_LINES || output.length > OUTPUT_COLLAPSE_CHARS);

  useEffect(() => {
    if (failed) setExpanded(true);
  }, [failed]);

  useEffect(() => {
    if (failed && preRef.current) preRef.current.scrollTop = preRef.current.scrollHeight;
  }, [failed, output]);

  const visible = long && !expanded ? lines.slice(0, OUTPUT_PREVIEW_LINES).join("\n") : output;

  return (
    <div className="mt-1 first:mt-0">
      <p className="text-ui-sm font-medium tracking-wide text-foreground-muted uppercase">Output</p>
      <pre
        ref={preRef}
        data-testid="tool-detail-output"
        className={cn(
          "mt-0.5 overflow-auto rounded border bg-background p-2 font-mono text-foreground whitespace-pre-wrap",
          failed ? "max-h-48" : "max-h-64",
        )}
      >
        {visible}
      </pre>
      {long && (
        <button
          type="button"
          data-testid="tool-output-toggle"
          onClick={() => setExpanded((prev) => !prev)}
          className="mt-1 text-foreground-muted underline-offset-2 outline-none hover:underline focus-visible:ring-3 focus-visible:ring-ring/50"
        >
          {expanded ? "Hide output" : "Show output"}
        </button>
      )}
    </div>
  );
}

/** A search's match list: bounded by default (never an unbounded dump) with an expand toggle, mirroring terminal output's rhythm. */
function SearchMatches({ output }: { output: string }) {
  const [expanded, setExpanded] = useState(false);
  const lines = output.split("\n");
  const bounded = lines.length > MATCH_PREVIEW_LINES;
  const visible = bounded && !expanded ? lines.slice(0, MATCH_PREVIEW_LINES) : lines;

  return (
    <div className="mt-1 first:mt-0">
      <p className="text-ui-sm font-medium tracking-wide text-foreground-muted uppercase">Matches</p>
      <pre data-testid="tool-detail-output" className="mt-0.5 max-h-64 overflow-auto rounded bg-surface p-2 whitespace-pre-wrap">
        {visible.join("\n")}
      </pre>
      {bounded && (
        <button
          type="button"
          data-testid="tool-detail-matches-toggle"
          onClick={() => setExpanded((prev) => !prev)}
          className="mt-1 text-foreground-muted underline-offset-2 outline-none hover:underline focus-visible:ring-3 focus-visible:ring-ring/50"
        >
          {expanded ? "Show fewer matches" : `Show all ${lines.length} matches`}
        </button>
      )}
    </div>
  );
}

/**
 * The unclassified fallback: a labeled IN/OUT layout (dsh's tool-card
 * pattern) instead of a raw JSON dump with no framing -- a small
 * uppercase gutter label plus a divider between the two sections, so a
 * mystery tool's input and its result read as two distinct things rather
 * than one wall of braces.
 */
function GenericBody({ item }: { item: TimelineToolCallItem }) {
  const input = stringifyToolPayload(item.input);
  const result = stringifyToolPayload(item.result);
  if (!input && !result) return <p className="text-foreground-muted">No detail recorded</p>;
  return (
    <div className="divide-y divide-border overflow-hidden rounded border">
      {input && <ToolSection label="IN" body={input} testId="tool-call-input" />}
      {result && <ToolSection label="OUT" body={result} testId="tool-call-result" />}
    </div>
  );
}

function ToolSection({ label, body, testId }: { label: string; body: string; testId: string }) {
  return (
    <div className="flex gap-2 bg-surface p-2">
      <span className="w-6 shrink-0 text-ui-sm font-medium tracking-wide text-foreground-muted uppercase">{label}</span>
      <pre data-testid={testId} className="min-w-0 flex-1 overflow-x-auto whitespace-pre-wrap font-mono text-ui-sm font-medium">
        {body}
      </pre>
    </div>
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
