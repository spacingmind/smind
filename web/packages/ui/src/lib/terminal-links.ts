/**
 * Pure matchers that turn one line of terminal text into link targets
 * (DT3-A): `path`, `path:line`, `path:line:col` and http(s) URLs. Pure on
 * purpose -- no DOM, no xterm -- so the security-relevant surface (which
 * text becomes a clickable action) is testable without an emulator.
 *
 * A match inside a URL is a URL link, never a path link: path matching
 * runs on the line with every URL span blanked out.
 */

export type TerminalLinkMatch = { start: number; end: number; text: string } & (
  | { kind: "url"; url: string }
  | { kind: "path"; path: string; line?: number; col?: number }
);

/** http(s) URLs only. The scheme is anchored by the regex itself, so `file:`, `javascript:`, `data:` and friends never match at all. */
const URL_RE = /https?:\/\/[^\s<>"'`]+/gi;

/** Sentence/bracket punctuation a URL never ends with. */
const URL_TRAILING_TRIM = ".,;:!?'\"]}>";
/** A path token, optionally with a `:line` or `:line:col` suffix. `\` and `.` are token chars, so Windows separators and `./` prefixes stay whole; `(?!\d)` refuses a digit run longer than the 9 digits a line/col may have. */
const PATH_TOKEN_RE = /[A-Za-z0-9_@+~./\\-]+(?::(\d{1,9})(?::(\d{1,9}))?)?(?!\d)/g;
/** The extension that makes an extension-only token (no separator) a path: the part after the last dot. */
const EXTENSION_RE = /\.([A-Za-z0-9]{1,8})$/;
/** Tokens made only of separators and dots (`..`, `./`, `///`) are not paths. */
const ONLY_SEPARATORS_RE = /^[./\\]+$/;

/** One opening bracket/quote a path may sit inside. */
const PATH_LEADING_TRIM = "([{'\"`";
/** Trailing sentence punctuation around a path; only `.` is a token char, so it is the one that ever needs trimming. */
const PATH_TRAILING_TRIM = ".,)]";

/** URL cap (AC5): a longer match is refused rather than linkified. */
const MAX_URL_LENGTH = 2048;

/**
 * Finds the links in one line of terminal output, sorted by position and
 * non-overlapping.
 */
export function findTerminalLinks(lineText: string): TerminalLinkMatch[] {
  const matches: TerminalLinkMatch[] = [];
  const chars = [...lineText];

  for (const match of lineText.matchAll(URL_RE)) {
    const raw = match[0];
    const start = match.index ?? 0;
    // Blank the whole raw span (not just the accepted part): nothing
    // inside a URL -- trimmed punctuation included -- may become a path.
    for (let i = start; i < start + raw.length; i++) chars[i] = " ";

    const url = trimUrl(raw);
    if (url === null) continue;
    matches.push({ kind: "url", url, start, end: start + url.length, text: url });
  }

  const blanked = chars.join("");
  for (const match of blanked.matchAll(PATH_TOKEN_RE)) {
    const full = match[0];
    const matchStart = match.index ?? 0;
    const lineRaw = match[1];
    const colRaw = match[2];
    const suffixLength = (lineRaw ? lineRaw.length + 1 : 0) + (colRaw ? colRaw.length + 1 : 0);

    let token = full.slice(0, full.length - suffixLength);
    // A token directly after a scheme-like `<letter>:` prefix (`data:text/html`,
    // `file:src/a.ts`) is never a path link -- and refusing `C:\work\...`'s
    // tail too is safer than linking it as a worktree-relative path.
    if (blanked[matchStart - 1] === ":" && /[A-Za-z]/.test(blanked[matchStart - 2] ?? "")) continue;
    if (!isPathToken(token)) continue;

    // A zero line or col is not a line reference: keep the file link, drop the suffix.
    let line: number | undefined;
    let col: number | undefined;
    if (lineRaw !== undefined && Number(lineRaw) >= 1) {
      line = Number(lineRaw);
      if (colRaw !== undefined && Number(colRaw) >= 1) col = Number(colRaw);
    }

    // Peel surrounding punctuation off the token; only leading brackets/quotes
    // and trailing `.` can ever be part of it (the rest are not token chars).
    let tokenStart = matchStart;
    if (PATH_LEADING_TRIM.includes(token[0] ?? "")) {
      token = token.slice(1);
      tokenStart += 1;
    }
    while (token.length > 0 && PATH_TRAILING_TRIM.includes(token[token.length - 1]!)) {
      token = token.slice(0, -1);
    }
    if (token === "") continue;

    const end = tokenStart + token.length + (line !== undefined ? suffixLength : 0);
    matches.push({
      kind: "path",
      path: token,
      ...(line !== undefined ? { line } : {}),
      ...(col !== undefined ? { col } : {}),
      start: tokenStart,
      end,
      text: blanked.slice(tokenStart, end),
    });
  }

  return matches.sort((a, b) => a.start - b.start);
}

/** Trims trailing punctuation (and an unbalanced `)`), then enforces the parse-time rules: exact http/https protocol, no userinfo, length cap. */
function trimUrl(raw: string): string | null {
  // Bound the work first: the trimming loop below is quadratic on a long run of ")".
  if (raw.length > MAX_URL_LENGTH + 64) return null;
  let trimmed = trimTrailing(raw);
  let open = 0;
  let close = 0;
  for (const ch of trimmed) {
    if (ch === "(") open++;
    else if (ch === ")") close++;
  }
  while (trimmed.endsWith(")") && close > open) {
    trimmed = trimTrailing(trimmed.slice(0, -1));
    close--;
  }
  const text = trimmed;
  if (text.length === 0 || text.length > MAX_URL_LENGTH) return null;

  let url: URL;
  try {
    url = new URL(text);
  } catch {
    return null;
  }
  if (url.protocol !== "http:" && url.protocol !== "https:") return null;
  if (url.username !== "" || url.password !== "") return null;
  return text;
}

function trimTrailing(text: string): string {
  let out = text;
  while (out.length > 0 && URL_TRAILING_TRIM.includes(out[out.length - 1]!)) {
    out = out.slice(0, -1);
  }
  return out;
}

/** The path-ness rules: no UNC-style leading `//`/`\\` (so `file:///etc/passwd` yields nothing, matching the resolver's UNC refusal), a separator somewhere, or a `.` + 1-8 alphanumeric extension whose extension contains a letter (so `1.2.3`, `10.0.0.1` and `e.g.` are not paths), and not only separators. */
function isPathToken(token: string): boolean {
  if (token.startsWith("//") || token.startsWith("\\\\")) return false;
  if (ONLY_SEPARATORS_RE.test(token)) return false;
  if (token.includes("/") || token.includes("\\")) return true;
  const extension = EXTENSION_RE.exec(token)?.[1];
  return extension !== undefined && /[A-Za-z]/.test(extension);
}

/** Opening a link needs Ctrl (or Cmd on macOS) held: a plain click only selects text, so an accidental click never opens anything (DT3-A AC2). */
export function activateTerminalLink(event: { ctrlKey: boolean; metaKey: boolean }, run: () => void): void {
  if (event.ctrlKey || event.metaKey) run();
}
