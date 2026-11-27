import { useLayoutEffect, useRef } from "react";
import { Dialog as DialogPrimitive } from "radix-ui";

import { listSettingsSections } from "@/components/settings/settings-registry";
import { SettingsScreen } from "@/components/settings/settings-screen";
import { Dialog, DialogOverlay, DialogPortal, DialogTitle } from "@/components/ui/dialog";
import type { DaemonEvents } from "@/hooks/use-daemon-events";
import { cn } from "@/lib/utils";
import type { WsClient } from "@/lib/ws-client";

/**
 * Settings as a modal (UI cleanup B3 D1): the modal variant of
 * {@link SettingsScreen} inside a Radix `Dialog`, above the still-mounted
 * shell -- focus trap, labelled title, focus restore, dimmed backdrop.
 *
 * The content is built from the Radix primitive rather than the stock
 * `DialogContent`: that one centres a small `max-w-lg` grid and appends its
 * own close button, while this dialog is the settings screen's own frame
 * (960x680, a column, and the X lives in the screen's content header).
 *
 * Escape is owned by `SettingsScreen`'s own document handler (so a narrow
 * detail page pops to the list first): this dialog's Radix layer therefore
 * consumes the native default of every Escape (`onEscapeKeyDown` calls
 * `preventDefault`, so Radix never dismisses) and lets the screen decide --
 * see the modal-variant notes there. Pointer-down outside closes via
 * Radix's default `onOpenChange(false)`.
 */
export function SettingsDialog({
  open,
  onClose,
  client,
  events,
  initialSectionId,
  openNonce,
}: {
  open: boolean;
  onClose: () => void;
  client: WsClient | null;
  events?: DaemonEvents | null;
  initialSectionId?: string;
  /** Bumped by every open request: remounts the screen onto the requested section. */
  openNonce: number;
}) {
  const contentRef = useRef<HTMLDivElement | null>(null);
  const openerRef = useRef<HTMLElement | null>(null);

  // The element to restore focus to on close (D4): whatever was focused when
  // the dialog opened -- the opener for a click, the previously focused
  // element for a keyboard trigger. Layout phase, before the content's own
  // auto-focus effect moves focus into the dialog.
  useLayoutEffect(() => {
    if (open) openerRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
  }, [open]);

  return (
    <Dialog open={open} onOpenChange={(o) => { if (!o) onClose(); }}>
      <DialogPortal>
        <DialogOverlay />
        <DialogPrimitive.Content
          ref={contentRef}
          tabIndex={-1}
          data-testid="settings-dialog"
          aria-describedby={undefined}
          className={cn(
            "fixed left-1/2 top-1/2 z-50 -translate-x-1/2 -translate-y-1/2 flex h-[min(680px,90vh)] w-[min(960px,90vw)] flex-col overflow-hidden rounded-2xl border bg-background shadow-lg duration-(--duration-menu) data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95",
          )}
          onOpenAutoFocus={(e) => {
            e.preventDefault();
            // Focus the requested section's nav item (D4) -- the first section
            // for a plain open -- falling back to the content itself.
            const sections = listSettingsSections();
            const id = sections.some((s) => s.id === initialSectionId) ? initialSectionId : sections[0]?.id;
            const target = id ? contentRef.current?.querySelector<HTMLElement>(`[data-testid="settings-nav-${id}"]`) : null;
            (target ?? contentRef.current)?.focus();
          }}
          onEscapeKeyDown={(e) => e.preventDefault()}
          onCloseAutoFocus={(e) => {
            e.preventDefault();
            openerRef.current?.focus();
          }}
        >
          <DialogTitle className="sr-only">Settings</DialogTitle>
          <SettingsScreen
            key={openNonce}
            variant="modal"
            client={client}
            events={events}
            onNavigateBack={onClose}
            initialSectionId={initialSectionId}
          />
        </DialogPrimitive.Content>
      </DialogPortal>
    </Dialog>
  );
}
