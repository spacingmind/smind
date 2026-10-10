import { useWindowState } from "@/hooks/use-window-state";
import { desktopOS, desktopWindow, isDesktop } from "@/lib/platform";
import { hasCaptionButtons } from "@/lib/window-chrome";
import { cn } from "@/lib/utils";

/** Caption-button glyphs, drawn at the 10px grid Windows' own buttons use so they read as native. */
function MaximizeGlyph() {
  return (
    <svg viewBox="0 0 10 10" className="size-2.5" fill="none" stroke="currentColor" strokeWidth="1" aria-hidden>
      <rect x="0.5" y="0.5" width="9" height="9" />
    </svg>
  );
}

function RestoreGlyph() {
  return (
    <svg viewBox="0 0 10 10" className="size-2.5" fill="none" stroke="currentColor" strokeWidth="1" aria-hidden>
      <rect x="0.5" y="2.5" width="7" height="7" />
      <path d="M2.5 2.5V0.5h7v7h-2" />
    </svg>
  );
}

function MinimizeGlyph() {
  return (
    <svg viewBox="0 0 10 10" className="size-2.5" fill="none" stroke="currentColor" strokeWidth="1" aria-hidden>
      <path d="M0 5.5h10" />
    </svg>
  );
}

function CloseGlyph() {
  return (
    <svg viewBox="0 0 10 10" className="size-2.5" fill="none" stroke="currentColor" strokeWidth="1" aria-hidden>
      <path d="M0.5 0.5l9 9M9.5 0.5l-9 9" />
    </svg>
  );
}

const BUTTON =
  "inline-flex h-full w-11.5 shrink-0 items-center justify-center text-foreground/80 outline-none transition-colors hover:text-foreground focus-visible:bg-accent";

/**
 * The drawn minimize / maximize-restore / close buttons at the right end of
 * the header row on Windows and Linux (the window is undecorated there).
 * Renders nothing in a browser build or on macOS, where the native traffic
 * lights stay. Close goes through the window's close-request, so the shell's
 * existing hide-to-tray handler runs -- it does not quit.
 */
export function DesktopWindowControls() {
  const { maximized } = useWindowState();
  if (!isDesktop || !hasCaptionButtons(desktopOS)) return null;

  const run = (op: () => Promise<void>) => () => {
    op().catch(() => {});
  };

  return (
    <div data-testid="desktop-window-controls" className="-my-2 -mr-2 ml-1 flex h-12 shrink-0 items-stretch">
      <button
        type="button"
        data-testid="window-control-minimize"
        aria-label="Minimize"
        className={cn(BUTTON, "hover:bg-accent")}
        onClick={run(() => desktopWindow.minimize())}
      >
        <MinimizeGlyph />
      </button>
      <button
        type="button"
        data-testid="window-control-maximize"
        data-maximized={maximized}
        aria-label={maximized ? "Restore" : "Maximize"}
        className={cn(BUTTON, "hover:bg-accent")}
        onClick={run(() => desktopWindow.toggleMaximize())}
      >
        {maximized ? <RestoreGlyph /> : <MaximizeGlyph />}
      </button>
      <button
        type="button"
        data-testid="window-control-close"
        aria-label="Close"
        className={cn(BUTTON, "hover:bg-destructive hover:text-white")}
        onClick={run(() => desktopWindow.close())}
      >
        <CloseGlyph />
      </button>
    </div>
  );
}
