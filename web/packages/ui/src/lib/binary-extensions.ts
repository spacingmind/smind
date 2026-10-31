/**
 * Extensions whose files are (almost always) binary. A file tab for one of these asks the daemon (`file.stat`) what the file really is
 * before the editor tries to read it as text. Any other extension goes straight to the editor, exactly as before; a file that turns out
 * not to be UTF-8 (any other binary) is routed through `file.stat` when its read fails (see FileEditorPane).
 */
const BINARY_EXTENSIONS: ReadonlySet<string> = new Set(
  "pdf doc docx xls xlsx ppt pptx odt ods odp png jpg jpeg gif webp bmp ico zip gz tar 7z exe dll so bin mp3 mp4 mov wav woff woff2 ttf otf".split(" "),
);

/** The lower-cased extension of a worktree path (either separator), without the dot; "" when it has none. */
export function pathExtension(path: string): string {
  const name = path.split(/[\\/]/).pop() ?? "";
  const dot = name.lastIndexOf(".");
  return dot <= 0 ? "" : name.slice(dot + 1).toLowerCase();
}

/** True when the extension says the file is binary (case-insensitive). */
export function hasBinaryExtension(path: string): boolean {
  return BINARY_EXTENSIONS.has(pathExtension(path));
}
