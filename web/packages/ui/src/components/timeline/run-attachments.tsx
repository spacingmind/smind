import { useState } from "react";

import { AttachmentLightbox } from "@/components/attachment-lightbox";
import { AttachmentChip } from "@/components/timeline/attachment-chip";
import { useInView } from "@/hooks/use-in-view";
import { useRunAttachment } from "@/hooks/use-run-attachment";
import { Icons } from "@/lib/icons";
import type { AttachmentMeta } from "@/lib/types";

/** Fixed box: the skeleton and the loaded thumbnail share it, so nothing reflows when the bytes arrive. */
const THUMB_CLASS = "size-28 shrink-0 sm:size-40 rounded-lg border border-border";

/**
 * One image under a sent message. A thumbnail the client already holds (`previewUrl`, from the optimistic echo)
 * renders at once; any other is fetched with `run.attachment` the first time it scrolls into view. Click opens the lightbox.
 */
function ImageFrame({ runId, attachment }: { runId: string; attachment: AttachmentMeta }) {
  const { ref, inView } = useInView<HTMLDivElement>();
  const fetched = useRunAttachment(runId, attachment.id, inView && !attachment.previewUrl);
  const [open, setOpen] = useState(false);
  const src = attachment.previewUrl ?? fetched.url;
  const name = attachment.name ?? "image";

  if (src) {
    return (
      <>
        <button type="button" aria-label={`Preview ${name}`} onClick={() => setOpen(true)} className="block cursor-zoom-in rounded-lg focus-visible:ring-3 focus-visible:ring-ring/50 focus-visible:outline-hidden">
          <img src={src} alt={name} data-testid="run-attachment-image" className={`${THUMB_CLASS} object-cover`} />
        </button>
        <AttachmentLightbox src={src} name={name} open={open} onOpenChange={setOpen} />
      </>
    );
  }
  if (fetched.error) {
    return (
      <div ref={ref} data-testid="run-attachment-failed" className={`${THUMB_CLASS} flex flex-col items-center justify-center gap-1 bg-surface p-2 text-ui-xs text-foreground-muted`}>
        <Icons.warning aria-hidden className="size-4" />
        <span className="max-w-full truncate">{name}</span>
      </div>
    );
  }
  return <div ref={ref} data-testid="run-attachment-skeleton" aria-busy="true" aria-label={`Loading ${name}`} className={`${THUMB_CLASS} animate-pulse bg-surface motion-reduce:animate-none`} />;
}

/** The attachments of a sent message, read-only, under its text (ADR-0038 §7): image thumbnails, then chips for everything else. */
export function RunAttachments({ runId, attachments }: { runId: string; attachments: AttachmentMeta[] | undefined }) {
  if (!attachments || attachments.length === 0) return null;
  const images = attachments.filter((a) => a.kind === "image");
  const others = attachments.filter((a) => a.kind !== "image");
  return (
    <div data-testid="run-attachments" className="flex max-w-[85%] flex-col items-end gap-1.5">
      {images.length > 0 && (
        <div className="flex flex-wrap justify-end gap-2">
          {images.map((a) => (
            <ImageFrame key={a.id} runId={runId} attachment={a} />
          ))}
        </div>
      )}
      {others.length > 0 && (
        <div className="flex flex-wrap justify-end gap-1.5">
          {others.map((a) => (
            <AttachmentChip key={a.id} attachment={a} />
          ))}
        </div>
      )}
    </div>
  );
}
