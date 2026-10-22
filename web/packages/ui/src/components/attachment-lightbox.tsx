import { useState } from "react";
import { Dialog as DialogPrimitive } from "radix-ui";

import { Dialog, DialogDescription, DialogOverlay, DialogPortal, DialogTitle } from "@/components/ui/dialog";
import { Icons } from "@/lib/icons";

/** A small image is shown up to this many times its natural size, so a thumbnail-sized source is still worth opening. */
const MAX_UPSCALE = 3;

/**
 * A full-size look at an attached image on the shared Dialog (Esc closes, the name is the accessible title).
 * The image sits straight on a dark scrim with no frame, so nothing can be low-contrast against it; the close button
 * is a dark round chip in the corner of the screen, outside the image, so it is visible on a white screenshot too.
 * The image is fitted to the viewport and scaled up (to MAX_UPSCALE times its natural size) when it is small.
 */
export function AttachmentLightbox({
  src,
  name,
  open,
  onOpenChange,
}: {
  src: string;
  name: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const [natural, setNatural] = useState<{ w: number; h: number } | null>(null);
  // Fit inside 92vw x 88vh, never below the natural size's cap; before the image has loaded it is only capped.
  const style = natural
    ? {
        width: `min(92vw, ${natural.w * MAX_UPSCALE}px, calc(88vh * ${natural.w} / ${natural.h}))`,
        aspectRatio: `${natural.w} / ${natural.h}`,
      }
    : undefined;
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogPortal>
        <DialogOverlay className="bg-black/85" />
        <DialogPrimitive.Content
          data-testid="attachment-lightbox"
          className="fixed top-[50%] left-[50%] z-50 translate-x-[-50%] translate-y-[-50%] outline-hidden duration-(--duration-menu) data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95"
        >
          <DialogTitle className="sr-only">{name}</DialogTitle>
          <DialogDescription className="sr-only">Full-size preview of the attached image.</DialogDescription>
          <img
            src={src}
            alt={name}
            style={style}
            onLoad={(e) => setNatural({ w: e.currentTarget.naturalWidth, h: e.currentTarget.naturalHeight })}
            className="block max-h-[88vh] max-w-[92vw] rounded-lg object-contain shadow-lg"
          />
        </DialogPrimitive.Content>
        <DialogPrimitive.Close
          data-testid="attachment-lightbox-close"
          className="fixed top-4 right-4 z-[60] flex size-9 items-center justify-center rounded-full bg-black/70 text-white ring-1 ring-white/40 transition-colors hover:bg-black focus-visible:ring-2 focus-visible:ring-white focus-visible:outline-hidden [&>svg]:size-4"
        >
          <Icons.close aria-hidden />
          <span className="sr-only">Close</span>
        </DialogPrimitive.Close>
      </DialogPortal>
    </Dialog>
  );
}
