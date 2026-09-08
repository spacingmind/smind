import type { ReactNode } from "react";
import { FileCode, ImageIcon } from "lucide-react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";

/** Which content types get a Preview mode in the editor view. */
export type PreviewKind = "html" | "markdown" | "svg";

const EXTENSION_KINDS: Record<string, PreviewKind> = {
  ".html": "html",
  ".htm": "html",
  ".markdown": "markdown",
  ".md": "markdown",
  ".mdx": "markdown",
  ".svg": "svg",
};

/**
 * Extension -> PreviewKind for the editor's preview mode. Extensions match
 * case-insensitively and off the last dot, like most editors. Everything
 * unrecognized returns null and never sees the preview control.
 */
export function previewKind(path: string): PreviewKind | null {
  const dot = path.lastIndexOf(".");
  if (dot === -1) return null;
  return EXTENSION_KINDS[path.slice(dot).toLowerCase()] ?? null;
}

/**
 * Markdown as real formatted output (headings, lists, code blocks, links;
 * GFM tables via remark-gfm). react-markdown never produces raw HTML from
 * the source -- its output is React elements, and its default
 * `urlTransform` neutralizes `javascript:`-style URLs -- so no extra
 * sanitizing wrapper is needed. (Raw HTML *in* the markdown source simply
 * renders as literal text.)
 */
export function MarkdownPreview({ content }: { content: string }) {
  return (
    <div className="prose prose-sm max-w-none flex-1 overflow-auto px-4 py-3" data-testid="markdown-preview">
      <ReactMarkdown remarkPlugins={[remarkGfm]}>{content}</ReactMarkdown>
    </div>
  );
}

/**
 * SVG as a real image via a data URL. Forges an <img>, never an inline
 * <svg> element: an <img>-loaded SVG is processed in image mode and cannot
 * run scripts, and the parent app's stylesheet is kept out of the picture.
 */
export function SvgPreview({ content, path }: { content: string; path: string }) {
  return (
    <div className="flex flex-1 items-center justify-center overflow-auto p-4" data-testid="svg-preview">
      <img
        src={`data:image/svg+xml,${encodeURIComponent(content)}`}
        alt={path}
        className="max-h-full max-w-full object-contain"
      />
    </div>
  );
}

/**
 * HTML previews sandboxed: the frame's `sandbox` attribute lists no
 * permissions (crucially, no `allow-scripts` and no `allow-same-origin`),
 * so the frame renders but cannot execute scripts or reach the UI's
 * origin. srcDoc (not a daemon URL or blob URL) keeps the preview on the
 * editor's current buffer with zero network involvement. Content without
 * an explicit charset gets the UTF-8 meta tag prepended, since srcDoc
 * documents default to windows-1252 and can garble non-ASCII text.
 */
export function HtmlPreview({ content }: { content: string }) {
  const srcDoc = /<meta[^>]+charset/i.test(content) ? content : `<meta charset="utf-8">${content}`;
  return (
    <iframe
      title="HTML preview"
      srcDoc={srcDoc}
      sandbox=""
      data-testid="html-preview-frame"
      className="h-full min-h-0 flex-1 border-0 bg-white"
    />
  );
}

/**
 * The right-hand renderer for preview mode. An empty buffer (still
 * loading, or a genuinely empty file) gets an explanatory state instead of
 * a blank pane.
 */
export function FilePreview({ kind, content, path }: { kind: PreviewKind; content: string; path: string }) {
  if (kind === "markdown") {
    return content ? (
      <MarkdownPreview content={content} />
    ) : (
      <EmptyPreview icon={<FileCode className="size-3.5" />} />
    );
  }
  if (kind === "svg") {
    return content ? (
      <SvgPreview content={content} path={path} />
    ) : (
      <EmptyPreview icon={<ImageIcon className="size-3.5" />} />
    );
  }
  return content ? <HtmlPreview content={content} /> : <EmptyPreview icon={<FileCode className="size-3.5" />} />;
}

function EmptyPreview({ icon }: { icon: ReactNode }) {
  return (
    <div
      className="flex flex-1 items-center justify-center gap-1.5 text-sm text-muted-foreground"
      data-testid="preview-empty"
    >
      {icon}
      Nothing to preview yet
    </div>
  );
}
