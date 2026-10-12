import { UsageBody } from "@/components/account-usage/usage-body";
import { UsageControls } from "@/components/account-usage/usage-controls";
import { useUsageSurface } from "@/components/account-usage/use-usage-surface";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";
import { Sheet, SheetContent, SheetDescription, SheetTitle } from "@/components/ui/sheet";
import type { DaemonEvents } from "@/hooks/use-daemon-events";
import { useIsMobile } from "@/hooks/use-mobile";
import { closeUsageModal, useUsageModalOpen } from "@/hooks/use-usage-modal";
import type { WsClient } from "@/lib/ws-client";

/**
 * Plan usage over the current screen (Paseo's UsageModal): a dialog on wide
 * layouts, a bottom sheet (at most 80% of the height, backdrop visible) on
 * narrow ones. Title row = account filter, Refresh all, Settings. It owns no
 * route: opening and closing leave the open task, tabs and scroll alone.
 * While it is mounted it is a touching surface (see useAccountUsage).
 */
function UsageModal({ client, events }: { client: WsClient | null; events: DaemonEvents | null }) {
  const isMobile = useIsMobile();
  const surface = useUsageSurface(client, events, { touch: true });
  const body = (
    <div data-testid="usage-modal-body" className="min-h-0 flex-1 overflow-y-auto px-4 pb-4">
      <UsageBody
        view={surface.view}
        displayAs={surface.displayAs}
        refreshing={surface.snapshot.refreshing}
        failed={surface.snapshot.failed}
        pressedAt={surface.snapshot.pressedAt}
        onRefreshOne={surface.refreshOne}
        onRetry={surface.retry}
      />
    </div>
  );
  const header = (
    <div className="flex min-h-8 flex-wrap items-center justify-between gap-x-3 gap-y-2 px-4 pt-4 pr-12">
      <TitleFor isMobile={isMobile} />
      <UsageControls surface={surface} />
    </div>
  );
  const onOpenChange = (next: boolean) => {
    if (!next) closeUsageModal();
  };

  if (isMobile) {
    return (
      <Sheet open onOpenChange={onOpenChange}>
        <SheetContent
          side="bottom"
          data-testid="usage-modal"
          aria-describedby="usage-modal-description"
          className="max-h-[80vh] gap-3 rounded-t-2xl p-0"
        >
          {header}
          <SheetDescription id="usage-modal-description" className="sr-only">
            Plan usage per account
          </SheetDescription>
          {body}
        </SheetContent>
      </Sheet>
    );
  }
  return (
    <Dialog open onOpenChange={onOpenChange}>
      <DialogContent
        data-testid="usage-modal"
        aria-describedby="usage-modal-description"
        className="flex max-h-[85vh] flex-col gap-3 p-0 sm:max-w-[640px] [&>[data-slot=dialog-close]]:top-[22px]"
      >
        {header}
        <DialogDescription id="usage-modal-description" className="sr-only">
          Plan usage per account
        </DialogDescription>
        {body}
      </DialogContent>
    </Dialog>
  );
}

function TitleFor({ isMobile }: { isMobile: boolean }) {
  return isMobile ? (
    <SheetTitle className="text-ui-lg font-medium text-foreground">Usage</SheetTitle>
  ) : (
    <DialogTitle>Usage</DialogTitle>
  );
}

/**
 * Mounts the modal once, shell-level: it renders only while open (mounts on
 * first open, unmounts after close), so a closed modal costs nothing and
 * sends nothing. Lives outside the shell's `inert` wrapper so it opens
 * above the Settings screen too (Radix portals above its z-40).
 */
export function UsageModalRoot({ client, events }: { client: WsClient | null; events: DaemonEvents | null }) {
  const open = useUsageModalOpen();
  if (!open) return null;
  return <UsageModal client={client} events={events} />;
}
