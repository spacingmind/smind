/**
 * Extensions whose files are (almost always) binary. A file tab for one of these asks the daemon (`file.stat`) what the file really is
 * before the editor tries to read it as text. Any other extension goes straight to the editor, exactly as before; a file that turns out
 * not to be UTF-8 is routed through `file.stat` when its read fails (see FileEditorPane).
 */
const BINARY_EXTENSIONS: ReadonlySet<string> = new Set([
  "pdf", "doc", "docx", "xls", "xlsx", "ppt", "pptx", "odt", "ods", "odp", "pages", "numbers", "key",
  "png", "jpg", "jpeg", "gif", "webp", "bmp", "ico", "tif", "tiff", "heic", "avif",
  "zip", "gz", "tgz", "tar", "7z", "rar", "bz2", "xz",
  "exe", "dll", "so", "dylib", "bin", "o", "class", "wasm",
  "mp3", "mp4", "m4a", "mov", "wav", "avi", "webm", "mkv", "flac", "ogg",
  "woff", "woff2", "ttf", "otf", "eot",
]);

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
