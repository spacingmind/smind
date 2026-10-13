import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { useAppUpdate, type AppUpdateController } from "@/lib/app-update";
import { TimelineMarkdown } from "@/components/timeline/timeline-markdown";
import { desktop, isDesktop } from "@/lib/platform";
import { tidyReleaseNotes } from "@/lib/release-notes";
import type { Components } from "react-markdown";

/** Links in the notes open in the OS browser; a plain <a> would navigate the app's own webview. */
const NOTES_COMPONENTS: Components = {
  a: ({ href, children }) => (
    <a
      href={href}
      onClick={(event) => {
        event.preventDefault();
        if (href) void desktop.openExternal(href).catch(() => {});
      }}
    >
      {children}
    </a>
  ),
};

/**
 * ADR-0023 U4's update prompt: versions and release notes with Update &
 * restart / Later, download progress (determinate when the total is known)
 * then "Installing…", and an install error that returns to the prompt with
 * the error inline and Update retryable. Esc, the close button and Later
 * all dismiss; dismissing during download only hides it. Renders nothing
 * (and calls nothing) outside a desktop build.
 */
export function DesktopUpdateDialog() {
  return <DesktopUpdateDialogView update={useAppUpdate()} />;
}

/** The dialog over an update controller owned elsewhere (the app shell shares it with the header badge). */
export function DesktopUpdateDialogView({ update }: { update: AppUpdateController }) {
  const { state, confirm, dismiss } = update;

  if (!isDesktop || state.stage === "idle") return null;
  const { info } = state;
  const open = true;
  const downloading = state.stage === "downloading";
  const installing = state.stage === "installing";
  const percent =
    downloading && state.progress.stage === "downloading" && state.progress.total
      ? Math.round((state.progress.downloaded / state.progress.total) * 100)
      : null;

  return (
    <Dialog open={open} onOpenChange={(next) => { if (!next) dismiss(); }}>
      <DialogContent data-testid="desktop-update-dialog">
        <DialogHeader>
          <DialogTitle>Update available</DialogTitle>
          <DialogDescription>
            smind v{info.version} is available (you have v{info.currentVersion})
          </DialogDescription>
        </DialogHeader>
        <div className="max-h-60 overflow-y-auto rounded-xl bg-muted p-3 text-ui-base text-foreground">
          {info.notes ? (
            <TimelineMarkdown
              data-testid="desktop-update-dialog-notes"
              content={tidyReleaseNotes(info.notes)}
              components={NOTES_COMPONENTS}
            />
          ) : (
            <p data-testid="desktop-update-dialog-notes">No release notes.</p>
          )}
        </div>
        {(downloading || installing) && (
          <div data-testid="desktop-update-dialog-progress" role="status">
            {installing ? (
              <div className="text-ui-base text-muted-foreground">Installing…</div>
            ) : percent !== null ? (
              <>
                <div className="mb-1 flex justify-between text-ui-base text-muted-foreground">
                  <span>Downloading…</span>
                  <span data-testid="desktop-update-dialog-percent">{percent}%</span>
                </div>
                <div className="h-2 overflow-hidden rounded-full bg-muted" role="progressbar" aria-valuenow={percent} aria-valuemin={0} aria-valuemax={100}>
                  <div className="h-full rounded-full bg-primary transition-[width]" style={{ width: `${percent}%` }} />
                </div>
              </>
            ) : (
              <div className="text-ui-base text-muted-foreground" data-testid="desktop-update-dialog-indeterminate">Downloading…</div>
            )}
          </div>
        )}
        {state.stage === "prompt" && state.error && (
          <p role="alert" data-testid="desktop-update-dialog-error" className="text-ui-base text-destructive">
            {state.error}
          </p>
        )}
        <DialogFooter>
          <Button variant="ghost" data-testid="desktop-update-dialog-later" onClick={dismiss}>
            Later
          </Button>
          <Button
            data-testid="desktop-update-dialog-confirm"
            onClick={confirm}
            disabled={downloading || installing}
          >
            Update &amp; restart
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
