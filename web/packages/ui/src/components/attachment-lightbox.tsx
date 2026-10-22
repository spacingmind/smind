import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";

/**
 * A full-size look at an attached image, on the shared Dialog (rounded-2xl, Esc closes). The image is capped at
 * 80vh so a tall screenshot stays inside the viewport; the name is the alt text and the dialog's accessible title.
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
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent data-testid="attachment-lightbox" className="w-fit max-w-[calc(100%-2rem)] gap-2 p-3 sm:max-w-4xl">
        <DialogTitle className="sr-only">{name}</DialogTitle>
        <DialogDescription className="sr-only">Full-size preview of the attached image.</DialogDescription>
        <img src={src} alt={name} className="mx-auto max-h-[80vh] max-w-full rounded-lg object-contain" />
      </DialogContent>
    </Dialog>
  );
}
