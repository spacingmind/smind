/**
 * The client-side first layer of terminal path validation (DT3-A AC3):
 * turns a raw matched token into a "/"-joined path relative to the task's
 * worktree root, or null when it must be refused outright. This is a
 * filter, not the authority -- whatever it returns is still stat'd by the
 * daemon (`file.stat` sandboxes through files.Resolve) before anything
 * opens.
 *
 * `..` is refused, never normalised away: a refuser that computes its way
 * out of an escape attempt is one parsing bug from letting it through.
 */

const DRIVE_LETTER_RE = /^[A-Za-z]:\//;

export function resolveTerminalPath(raw: string, worktreePath: string | null): string | null {
  if (raw === "") return null;
  if (raw.includes("\0")) return null;
  if (raw.startsWith("~")) return null;
  // UNC and device forms: refused before any filesystem access, mirroring files.Resolve.
  if (raw.startsWith("\\\\") || raw.startsWith("//")) return null;

  let path = raw.replace(/\\/g, "/");

  if (path.startsWith("/") || DRIVE_LETTER_RE.test(path)) {
    if (worktreePath === null) return null;
    const root = worktreePath.replace(/\\/g, "/").replace(/\/+$/, "");
    const bare = path.replace(/\/+$/, "");
    // A drive-letter worktree is a Windows path, where case does not distinguish names.
    const driveRoot = DRIVE_LETTER_RE.test(root);
    const [rootCmp, pathCmp] = driveRoot ? [root.toLowerCase(), bare.toLowerCase()] : [root, bare];
    if (root === "" || !pathCmp.startsWith(`${rootCmp}/`)) return null;
    path = bare.slice(root.length + 1);
  }

  const segments: string[] = [];
  for (const segment of path.split("/")) {
    if (segment === "" || segment === ".") continue;
    if (segment === "..") return null;
    segments.push(segment);
  }
  if (segments.length === 0) return null;
  return segments.join("/");
}
