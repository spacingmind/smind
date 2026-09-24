import { memo } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";

/**
 * Assistant output rendered as real markdown (headings, lists, tables via
 * remark-gfm, fenced code as `<pre><code>`), replacing the raw `<pre>` the
 * run log used to be.
 *
 * The element styling is spelled out as child selectors rather than the
 * `prose` class file-preview.tsx reaches for: `@tailwindcss/typography`
 * isn't installed in this project, so `prose` is a no-op class there and
 * would be here too. Colors come from tokens only (docs/design.md §1).
 *
 * Memoized because react-markdown re-parses its whole source on every
 * render, and a streaming run re-renders its tail row on every chunk --
 * the earlier rows must not pay for that.
 */
export const TimelineMarkdown = memo(function TimelineMarkdown({ content }: { content: string }) {
  return (
    <div
      data-testid="timeline-markdown"
      data-chat-find-text="true"
      className="max-w-none text-content break-words [&_a]:underline [&_blockquote]:border-l-2 [&_blockquote]:border-border [&_blockquote]:pl-3 [&_blockquote]:text-foreground-muted [&_code]:rounded [&_code]:bg-surface-2 [&_code]:px-1 [&_code]:py-0.5 [&_code]:text-[0.85em] [&_h1]:mt-3 [&_h1]:mb-1 [&_h1]:text-base [&_h1]:font-medium [&_h2]:mt-3 [&_h2]:mb-1 [&_h2]:text-sm [&_h2]:font-medium [&_h3]:mt-2 [&_h3]:mb-1 [&_h3]:text-sm [&_h3]:font-medium [&_li]:my-0.5 [&_ol]:my-1.5 [&_ol]:list-decimal [&_ol]:pl-5 [&_p]:my-1.5 [&_pre]:my-2 [&_pre]:overflow-x-auto [&_pre]:rounded-lg [&_pre]:bg-surface-2 [&_pre]:p-2.5 [&_pre_code]:bg-transparent [&_pre_code]:p-0 [&_table]:my-2 [&_table]:w-full [&_table]:text-xs [&_td]:border [&_td]:border-border [&_td]:px-2 [&_td]:py-1 [&_th]:border [&_th]:border-border [&_th]:px-2 [&_th]:py-1 [&_th]:text-left [&_ul]:my-1.5 [&_ul]:list-disc [&_ul]:pl-5 [&>*:first-child]:mt-0 [&>*:last-child]:mb-0"
    >
      <ReactMarkdown remarkPlugins={[remarkGfm]}>{content}</ReactMarkdown>
    </div>
  );
});
