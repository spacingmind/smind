// The Add-project dialog's path handling (C1, D3/D5). Everything here is pure string work on the
// client: the daemon's own checks (absolute, exists, directory, has .git) only run at submit, but the
// dialog has to judge a typed path and compare it with the current workspaces' paths before that, so
// it needs a shared definition of "the same path as written". Two deliberate limits: `..` and
// symlinks are never resolved (what the user typed is what gets sent; the daemon is the authority),
// and case is kept (the daemon compares paths exactly).

/** A Windows absolute path: `C:\` or `C:/` (a bare `C:` is drive-relative, not absolute). */
const DRIVE_ROOT_RE = /^[A-Za-z]:[\\/]/;
const DRIVE_LETTER_RE = /^[A-Za-z]:$/;

/** Trims, collapses separator runs (`//a//b/` -> `/a/b`), and drops one trailing separator -- except on a root. */
export function normalizeProjectPath(p: string): string {
  const trimmed = p.trim();
  if (trimmed === "") return "";
  const collapsed = trimmed.replace(/[\\/]+/g, (run) => run.charAt(0));
  if (!/[\\/]$/.test(collapsed)) return collapsed;
  const bare = collapsed.slice(0, -1);
  // Dropping the separator would turn "/" into "" or a drive root "C:\" into the drive-relative "C:".
  return bare === "" || DRIVE_LETTER_RE.test(bare) ? collapsed : bare;
}

/** Expands `~` to the daemon user's home (from `fs.listDir {}`); anything else, including `~user`, is returned unchanged. */
export function expandHome(p: string, home: string): string {
  if (p === "~") return home;
  if (p.startsWith("~/")) return home.endsWith("/") ? `${home}${p.slice(2)}` : `${home}${p.slice(1)}`;
  return p;
}

/** Why a typed path cannot be submitted: nothing typed, or not absolute (`~` still counts: it expands before submit). */
export function pathProblem(p: string): "empty" | "relative" | null {
  const path = p.trim();
  if (path === "") return "empty";
  if (path.startsWith("/") || path.startsWith("~") || DRIVE_ROOT_RE.test(path)) return null;
  return "relative";
}

/** The directory to list to judge a typed path: its parent. Roots ("/", "C:\", "~") are their own parent. */
export function parentOf(p: string): string {
  const path = normalizeProjectPath(p);
  const last = Math.max(path.lastIndexOf("/"), path.lastIndexOf("\\"));
  if (last < 0) return path;
  if (last === 0) return "/";
  const prefix = path.slice(0, last);
  // `C:\code`'s parent is the drive root `C:\` (or `C:/`), not the drive-relative `C:`.
  return DRIVE_LETTER_RE.test(prefix) ? `${prefix}${path.charAt(last)}` : prefix;
}

/** Case-sensitive equality of two paths as written, modulo whitespace, separator runs and a trailing separator. */
export function samePath(a: string, b: string): boolean {
  return normalizeProjectPath(a) === normalizeProjectPath(b);
}
