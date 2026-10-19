/**
 * A unified diff between two short texts (a skill body is at most ~25 lines), in the shape
 * `DiffRender` / diff2html parses. One hunk that spans the whole text: at this size the unchanged
 * lines are the context a reader needs, and a single hunk has no gap markers to explain.
 * Returns "" when nothing changed.
 */
export function unifiedDiff(oldText: string, newText: string, name: string): string {
  if (oldText === newText) return "";
  const a = splitLines(oldText);
  const b = splitLines(newText);
  const lines = diffLines(a, b);
  const body = lines.map((l) => `${l.op}${l.text}`).join("\n");
  return [
    `diff --git a/${name} b/${name}`,
    `--- a/${name}`,
    `+++ b/${name}`,
    `@@ -${a.length === 0 ? 0 : 1},${a.length} +${b.length === 0 ? 0 : 1},${b.length} @@`,
    body,
    "",
  ].join("\n");
}

/** `oldString` -> `newString` applied once to `body`; null when `oldString` is not in it (the target moved on). */
export function applyPatch(body: string, oldString: string, newString: string): string | null {
  if (oldString === "") return null;
  const at = body.indexOf(oldString);
  if (at === -1) return null;
  return body.slice(0, at) + newString + body.slice(at + oldString.length);
}

function splitLines(text: string): string[] {
  if (text === "") return [];
  const lines = text.split("\n");
  if (lines[lines.length - 1] === "") lines.pop();
  return lines;
}

interface DiffLine {
  op: " " | "+" | "-";
  text: string;
}

/** Longest-common-subsequence line diff. */
function diffLines(a: string[], b: string[]): DiffLine[] {
  const lcs: number[][] = Array.from({ length: a.length + 1 }, () => new Array<number>(b.length + 1).fill(0));
  for (let i = a.length - 1; i >= 0; i--) {
    for (let j = b.length - 1; j >= 0; j--) {
      lcs[i]![j] = a[i] === b[j] ? lcs[i + 1]![j + 1]! + 1 : Math.max(lcs[i + 1]![j]!, lcs[i]![j + 1]!);
    }
  }
  const out: DiffLine[] = [];
  let i = 0;
  let j = 0;
  while (i < a.length && j < b.length) {
    if (a[i] === b[j]) {
      out.push({ op: " ", text: a[i]! });
      i++;
      j++;
    } else if (lcs[i + 1]![j]! >= lcs[i]![j + 1]!) {
      out.push({ op: "-", text: a[i++]! });
    } else {
      out.push({ op: "+", text: b[j++]! });
    }
  }
  while (i < a.length) out.push({ op: "-", text: a[i++]! });
  while (j < b.length) out.push({ op: "+", text: b[j++]! });
  return out;
}
