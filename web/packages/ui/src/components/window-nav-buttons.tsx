import { Button } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import type { ActionId } from "@/keyboard/actions";
import { useKeyboard } from "@/keyboard/keyboard-provider";
import { formatChord } from "@/keyboard/shortcut-string";
import { Icons } from "@/lib/icons";

const ICON_BUTTON = "text-foreground-muted hover:text-foreground disabled:opacity-40";

/**
 * The desktop titlebar's back/forward pair (ZCode's top overlay): plain icon
 * buttons that step the app's navigation history. Each tooltip names the
 * direction plus the live chord for `nav.back` / `nav.forward` when one is
 * bound (the actions are desktop-only, so the web build shows the bare label).
 */
export function WindowNavButtons({
  canBack,
  canForward,
  onBack,
  onForward,
}: {
  canBack: boolean;
  canForward: boolean;
  onBack: () => void;
  onForward: () => void;
}) {
  const { bindings, isMac } = useKeyboard();
  const chordFor = (action: ActionId) =>
    bindings.find((b) => b.action === action)?.effectiveCombo ?? null;
  const labelFor = (label: string, action: ActionId) => {
    const combo = chordFor(action);
    return combo === null ? label : `${label} (${formatChord(combo, isMac)})`;
  };

  return (
    // Own provider: the pair is mounted by tests (and the window-level
    // toggle) without main.tsx's app-level one.
    <TooltipProvider>
      <div data-testid="window-nav-buttons" className="flex items-center gap-1">
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              aria-label="Back"
              data-testid="window-nav-back"
              className={ICON_BUTTON}
              disabled={!canBack}
              onClick={onBack}
            >
              <Icons.back aria-hidden />
            </Button>
          </TooltipTrigger>
          <TooltipContent side="bottom">{labelFor("Back", "nav.back")}</TooltipContent>
        </Tooltip>
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              aria-label="Forward"
              data-testid="window-nav-forward"
              className={ICON_BUTTON}
              disabled={!canForward}
              onClick={onForward}
            >
              <Icons.arrowRight aria-hidden />
            </Button>
          </TooltipTrigger>
          <TooltipContent side="bottom">{labelFor("Forward", "nav.forward")}</TooltipContent>
        </Tooltip>
      </div>
    </TooltipProvider>
  );
}
