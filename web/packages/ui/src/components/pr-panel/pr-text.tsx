import type { ReactNode } from "react";

import { safeHttpUrl } from "@/lib/safe-url";
import { Icons } from "@/lib/icons";
import { cn } from "@/lib/utils";

/**
 * GitHub-authored text (titles, comment bodies, authors, check names, paths) is third-party input: it is only ever a
 * React text node here, never HTML or markdown, and it wraps instead of widening the pane.
 */
export function UntrustedText({ children, className, as: Tag = "span" }: { children: string; className?: string; as?: "span" | "p" | "div" }) {
  return <Tag className={cn("min-w-0 whitespace-pre-wrap break-words [overflow-wrap:anywhere]", className)}>{children}</Tag>;
}

/** An external link from a daemon URL field; renders nothing unless the URL is http(s). */
export function ExternalLink({
  href,
  label,
  children,
  className,
  testId,
}: {
  href: string | undefined;
  label: string;
  children?: ReactNode;
  className?: string;
  testId?: string;
}) {
  const safe = safeHttpUrl(href);
  if (!safe) return null;
  return (
    <a
      href={safe}
      target="_blank"
      rel="noopener noreferrer"
      aria-label={label}
      title={label}
      data-testid={testId}
      className={cn("inline-flex shrink-0 items-center gap-1 text-ui-sm text-foreground-muted hover:text-foreground hover:underline", className)}
    >
      {children ?? <Icons.externalLink aria-hidden className="size-3.5" />}
    </a>
  );
}
