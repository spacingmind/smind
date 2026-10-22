import { formatBytes } from "@/lib/attachments/format";
import { externalLinkProps } from "@/lib/external-link";
import { Icons, type IconComponent } from "@/lib/icons";
import type { AttachmentMeta } from "@/lib/types";

const CHIP_CLASS =
  "inline-flex h-7 max-w-64 items-center gap-1.5 rounded-lg border border-border bg-surface px-2 text-ui-xs text-foreground-muted";

/** The icon and label a metadata item reads as: a name-only chip for queued rows, a full chip under a sent message. */
function describe(a: AttachmentMeta): { Icon: IconComponent; label: string; detail?: string } {
  switch (a.kind) {
    case "image":
      return { Icon: Icons.image, label: a.name ?? "image", detail: a.size !== undefined ? formatBytes(a.size) : undefined };
    case "file":
      return { Icon: Icons.file, label: a.name ?? "file", detail: a.size !== undefined ? formatBytes(a.size) : undefined };
    case "file_ref":
      return { Icon: a.isDir ? Icons.folder : Icons.file, label: a.path ?? "path" };
    case "github_pr":
      return { Icon: Icons.pullRequest, label: `#${a.number}${a.title ? ` ${a.title}` : ""}` };
    case "github_issue":
      return { Icon: Icons.issue, label: `#${a.number}${a.title ? ` ${a.title}` : ""}` };
  }
}

/**
 * A read-only chip for one attachment (ADR-0038 §7). GitHub items with a url open it externally; a `file_ref` is not
 * clickable in this PR. `compact` drops the size detail (queued rows).
 */
export function AttachmentChip({ attachment, compact = false }: { attachment: AttachmentMeta; compact?: boolean }) {
  const { Icon, label, detail } = describe(attachment);
  const body = (
    <>
      <Icon aria-hidden className="size-3.5 shrink-0" />
      <span className="min-w-0 truncate text-foreground" title={label}>
        {label}
      </span>
      {!compact && detail && <span className="shrink-0">{detail}</span>}
    </>
  );
  const isGithub = attachment.kind === "github_pr" || attachment.kind === "github_issue";
  if (isGithub && attachment.url) {
    return (
      <a data-testid="attachment-chip" data-kind={attachment.kind} {...externalLinkProps(attachment.url)} className={`${CHIP_CLASS} hover:bg-surface-hover`}>
        {body}
      </a>
    );
  }
  return (
    <span data-testid="attachment-chip" data-kind={attachment.kind} className={CHIP_CLASS}>
      {body}
    </span>
  );
}
