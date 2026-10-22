import type { KeyboardEvent } from "react";

import { useCanHover } from "@/hooks/use-can-hover";
import { formatBytes } from "@/lib/attachments/format";
import { openExternalUrl } from "@/lib/external-link";
import { Icons } from "@/lib/icons";
import { cn } from "@/lib/utils";
import { draftLabel, isDraftFile, type DraftAttachment } from "@/stores/composer-attachments-store";

/** The type label under a file name: its extension ("PDF", "TXT"), else the MIME subtype ("text/plain" is "PLAIN" only as a last resort). */
function typeLabel(name: string, mimeType: string): string {
  const dot = name.lastIndexOf(".");
  const ext = dot > 0 ? name.slice(dot + 1) : "";
  if (ext.length > 0 && ext.length <= 5 && /^[A-Za-z0-9]+$/.test(ext)) return ext.toUpperCase();
  const sub = mimeType.split("/")[1];
  return sub ? sub.replace(/^x-/, "").split("+")[0].toUpperCase() : mimeType;
}

function ForgeIcon({ kind }: { kind: "pr" | "issue" }) {
  const Icon = kind === "pr" ? Icons.pullRequest : Icons.issue;
  return <Icon aria-hidden className="size-4 shrink-0 text-foreground-muted" />;
}

/**
 * One draft attachment in the composer's tray (ADR-0038 §7, Paseo's AttachmentPill): a 48px square thumbnail for an
 * image, a two-line pill (name over type and size) for anything else, both 48px high so a mixed tray aligns. The
 * 16px close badge is revealed on hover / focus-within where a pointer exists, and always shown on touch and below
 * `md`. An item over a client cap shows the warning icon and the reason as its subtitle. Backspace/Delete on the
 * focused pill removes it, like a chip in a token field.
 */
export function AttachmentPill({
  item,
  onRemove,
  onOpen,
}: {
  item: DraftAttachment;
  onRemove: () => void;
  /** Opens the lightbox; only images with a preview are openable. */
  onOpen?: () => void;
}) {
  const canHover = useCanHover();
  const label = draftLabel(item);
  const file = isDraftFile(item) ? item : null;
  const forge = isDraftFile(item) ? null : item;
  const previewUrl = file?.previewUrl;
  const openable = item.kind === "image" && !!previewUrl && !item.error && !!onOpen;

  function handleKeyDown(e: KeyboardEvent<HTMLDivElement>) {
    // Keys typed into the close badge itself (Enter/Space) keep their default; only the pill's own keys act here.
    if (e.target !== e.currentTarget) return;
    if (e.key === "Backspace" || e.key === "Delete") {
      e.preventDefault();
      onRemove();
    } else if (openable && (e.key === "Enter" || e.key === " ")) {
      e.preventDefault();
      onOpen?.();
    } else if (forge && (e.key === "Enter" || e.key === " ")) {
      e.preventDefault();
      openExternalUrl(forge.url);
    }
  }

  const asImage = item.kind === "image" && !!previewUrl && !item.error;
  return (
    <div
      role="group"
      tabIndex={0}
      aria-label={item.error ? `${label}: ${item.error}` : label}
      data-testid="attachment-pill"
      data-kind={item.kind}
      data-error={item.error ? "true" : undefined}
      onKeyDown={handleKeyDown}
      className="group relative h-12 max-w-56 shrink-0 rounded-lg outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
    >
      {asImage ? (
        <button
          type="button"
          tabIndex={-1}
          aria-label={`Preview ${label}`}
          onClick={onOpen}
          className="block size-12 cursor-zoom-in overflow-hidden rounded-lg"
        >
          <img src={previewUrl} alt={label} className="size-12 rounded-lg border border-border object-cover" />
        </button>
      ) : (
        <div
          onClick={forge ? () => openExternalUrl(forge.url) : undefined}
          className={cn(
            "flex h-12 min-w-0 items-center gap-2 rounded-lg border bg-surface px-2.5",
            item.error ? "border-destructive/50" : "border-border",
            forge && "cursor-pointer hover:bg-surface-hover",
          )}
        >
          {item.error ? (
            <Icons.warning aria-hidden className="size-4 shrink-0 text-destructive" />
          ) : forge ? (
            <ForgeIcon kind={forge.kind} />
          ) : item.kind === "image" ? (
            <Icons.image aria-hidden className="size-4 shrink-0 text-foreground-muted" />
          ) : (
            <Icons.file aria-hidden className="size-4 shrink-0 text-foreground-muted" />
          )}
          <span className="flex min-w-0 flex-col">
            <span className="truncate text-ui-sm text-foreground" title={label}>
              {label}
            </span>
            <span className={cn("truncate text-ui-xs", item.error ? "text-destructive" : "text-foreground-muted")}>
              {item.error ?? (file ? `${typeLabel(file.name, file.mimeType)} · ${formatBytes(file.size)}` : `${forge!.kind === "pr" ? "PR" : "Issue"} · ${forge!.repo}`)}
            </span>
          </span>
        </div>
      )}
      {/* An image that failed a cap has no thumbnail row to carry the reason, so it is shown as the two-line pill above. */}
      <button
        type="button"
        tabIndex={canHover ? -1 : 0}
        aria-label={`Remove ${label}`}
        data-testid="attachment-pill-close"
        onClick={(e) => {
          e.stopPropagation();
          onRemove();
        }}
        className={cn(
          "absolute -right-1.5 -top-1.5 flex size-4 items-center justify-center rounded-full border border-border bg-background text-foreground-muted transition-opacity hover:text-foreground focus-visible:opacity-100 focus-visible:ring-2 focus-visible:ring-ring/50",
          canHover ? "opacity-100 md:opacity-0 md:group-focus-within:opacity-100 md:group-hover:opacity-100" : "opacity-100",
        )}
      >
        <Icons.close aria-hidden className="size-2.5" />
      </button>
    </div>
  );
}
