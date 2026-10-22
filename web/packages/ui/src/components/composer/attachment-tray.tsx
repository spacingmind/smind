import { useState } from "react";

import { AttachmentLightbox } from "@/components/attachment-lightbox";
import { AttachmentPill } from "@/components/composer/attachment-pill";
import type { DraftAttachment } from "@/stores/composer-attachments-store";

/**
 * The draft attachments inside the composer card, above the textarea (ADR-0038 §7). Renders nothing while empty,
 * and never animates its height: a half-animated frame reads as a glitch, so it simply appears.
 */
export function AttachmentTray({ items, onRemove }: { items: DraftAttachment[]; onRemove: (localId: string) => void }) {
  const [previewId, setPreviewId] = useState<string | null>(null);
  if (items.length === 0) return null;
  const previewing = items.find((i) => i.localId === previewId && i.previewUrl);
  return (
    <>
      <div data-testid="attachment-tray" role="list" aria-label="Attachments" className="flex flex-wrap gap-2 px-3 pt-3">
        {items.map((item) => (
          <div key={item.localId} role="listitem">
            <AttachmentPill item={item} onRemove={() => onRemove(item.localId)} onOpen={() => setPreviewId(item.localId)} />
          </div>
        ))}
      </div>
      {previewing?.previewUrl && (
        <AttachmentLightbox src={previewing.previewUrl} name={previewing.name} open onOpenChange={(open) => !open && setPreviewId(null)} />
      )}
    </>
  );
}
