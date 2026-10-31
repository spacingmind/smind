/**
 * Parser for the prompt of a run delivered from a parent's child-event queue
 * item (ADR-0041): a header paragraph, then one block per child --
 * `- task #12 "title" run r-4f2: error · rate_limited ...` optionally followed by
 * `<child-response task="12" untrusted="true">escaped text</child-response>`.
 * Mirrors internal/runs/childevent_text.go. Pure, never throws; callers fall
 * back to showing the raw prompt when `blocks` is empty. The response is
 * untrusted data: it is only ever rendered as a text node.
 */
export interface ChildEventBlock {
  /** The status line without its leading "- ". */
  line: string;
  /** The child's last text, entity-unescaped; undefined when the block had none. */
  response?: string;
}

export interface ChildEventPrompt {
  blocks: ChildEventBlock[];
}

// Escaped text never contains a literal "<", so the first literal closing tag
// ends the response even when the text mentions one (it arrives as &lt;/...&gt;).
const BLOCK_RE = /^- (task #[^\n]*)(?:\n<child-response\b[^>\n]*>([\s\S]*?)<\/child-response>)?/gm;

function unescapeChildText(s: string): string {
  return s.replace(/&(amp|lt|gt);/g, (_, e: string) => (e === "amp" ? "&" : e === "lt" ? "<" : ">"));
}

export function parseChildEventPrompt(prompt: string): ChildEventPrompt {
  const blocks: ChildEventBlock[] = [];
  try {
    for (const m of (prompt ?? "").matchAll(BLOCK_RE)) {
      const block: ChildEventBlock = { line: m[1].trimEnd() };
      if (m[2] !== undefined && m[2].trim() !== "") block.response = unescapeChildText(m[2]);
      blocks.push(block);
    }
  } catch {
    return { blocks: [] };
  }
  return { blocks };
}

/** A status line the child's provider throttled: shown in the warning tone. */
export function isRateLimitedLine(line: string): boolean {
  return line.includes("rate_limited") || line.includes("plan_window_exhausted");
}
