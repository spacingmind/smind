import type { LucideIcon } from "lucide-react";
import {
  Binary,
  Database,
  File,
  FileArchive,
  FileCode,
  FileCog,
  FileJson,
  FileTerminal,
  FileText,
  ImageIcon,
  Lock,
  Palette,
} from "lucide-react";

import { cn } from "@/lib/utils";

/**
 * A stable, testable name for the icon a path resolves to. It's the value
 * FileIcon renders as `data-icon`, so a test can assert "a .go path gets
 * the Go icon, an unknown extension gets the generic one" without
 * depending on lucide's internal SVG markup (ui-redesign-parity plan,
 * Item 17's first scenario).
 */
export type FileIconKey =
  | "go"
  | "code"
  | "json"
  | "markdown"
  | "style"
  | "markup"
  | "config"
  | "shell"
  | "image"
  | "archive"
  | "binary"
  | "database"
  | "lock"
  | "text"
  | "file";

/**
 * Extension -> icon key. Matched off the last dot, case-insensitively,
 * exactly like file-preview.tsx's previewKind (the two deliberately use
 * the same lookup shape -- previewKind decides whether a *preview* exists,
 * this one decides what the *row* looks like, and neither is derivable
 * from the other).
 */
const EXTENSION_ICONS: Record<string, FileIconKey> = {
  ".go": "go",
  ".ts": "code",
  ".tsx": "code",
  ".js": "code",
  ".jsx": "code",
  ".mjs": "code",
  ".cjs": "code",
  ".py": "code",
  ".rs": "code",
  ".rb": "code",
  ".java": "code",
  ".kt": "code",
  ".swift": "code",
  ".c": "code",
  ".h": "code",
  ".cc": "code",
  ".cpp": "code",
  ".hpp": "code",
  ".json": "json",
  ".jsonc": "json",
  ".md": "markdown",
  ".mdx": "markdown",
  ".markdown": "markdown",
  ".css": "style",
  ".scss": "style",
  ".sass": "style",
  ".less": "style",
  ".html": "markup",
  ".htm": "markup",
  ".xml": "markup",
  ".svg": "image",
  ".yaml": "config",
  ".yml": "config",
  ".toml": "config",
  ".ini": "config",
  ".env": "config",
  ".sh": "shell",
  ".bash": "shell",
  ".zsh": "shell",
  ".fish": "shell",
  ".png": "image",
  ".jpg": "image",
  ".jpeg": "image",
  ".gif": "image",
  ".webp": "image",
  ".ico": "image",
  ".avif": "image",
  ".zip": "archive",
  ".tar": "archive",
  ".gz": "archive",
  ".tgz": "archive",
  ".bz2": "archive",
  ".zst": "archive",
  ".wasm": "binary",
  ".so": "binary",
  ".dylib": "binary",
  ".dll": "binary",
  ".exe": "binary",
  ".db": "database",
  ".sqlite": "database",
  ".sql": "database",
  ".txt": "text",
  ".log": "text",
};

/**
 * Whole-filename matches, checked before the extension table. These are
 * the files a repo view is mostly *made of* -- a lockfile or a Dockerfile
 * has no extension at all, so an extension-only table would render them
 * generic (the exact thing file icons exist to avoid).
 */
const FILENAME_ICONS: Record<string, FileIconKey> = {
  dockerfile: "config",
  makefile: "config",
  taskfile: "config",
  "taskfile.yml": "config",
  "go.mod": "go",
  "go.sum": "lock",
  "go.work": "go",
  "bun.lock": "lock",
  "bun.lockb": "lock",
  "package-lock.json": "lock",
  "yarn.lock": "lock",
  "pnpm-lock.yaml": "lock",
  "cargo.lock": "lock",
  ".gitignore": "config",
  ".gitattributes": "config",
  ".editorconfig": "config",
  license: "text",
  "readme.md": "markdown",
};

const ICON_COMPONENTS: Record<FileIconKey, LucideIcon> = {
  go: FileCode,
  code: FileCode,
  json: FileJson,
  markdown: FileText,
  style: Palette,
  markup: FileCode,
  config: FileCog,
  shell: FileTerminal,
  image: ImageIcon,
  archive: FileArchive,
  binary: Binary,
  database: Database,
  lock: Lock,
  text: FileText,
  file: File,
};

/**
 * The icon key for a worktree-relative path. Returns `"file"` (the
 * generic document) for anything unrecognized -- there is deliberately no
 * "unknown" key, because the fallback is a real, rendered icon, not an
 * absence.
 */
export function fileIconKey(path: string): FileIconKey {
  const name = (path.split("/").pop() ?? path).toLowerCase();
  const byName = FILENAME_ICONS[name];
  if (byName) return byName;

  // `.gitignore` is a name, not an extension: a leading dot with no other
  // dot means the whole thing is the filename, so don't treat it as one.
  const dot = name.lastIndexOf(".");
  if (dot <= 0) return "file";
  return EXTENSION_ICONS[name.slice(dot)] ?? "file";
}

/**
 * A file-type icon for a path, used in the explorer tree and on file tabs
 * (Item 17). Purely decorative -- the filename is always rendered beside
 * it -- so it carries `aria-hidden` and the row's accessible name comes
 * from the text, not from here. `data-icon` is the assertable key; see
 * FileIconKey.
 */
export function FileIcon({ path, className }: { path: string; className?: string }) {
  const key = fileIconKey(path);
  const Icon = ICON_COMPONENTS[key];
  return <Icon aria-hidden data-testid="file-icon" data-icon={key} className={cn("size-3.5 shrink-0", className)} />;
}
