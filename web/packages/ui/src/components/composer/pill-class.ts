// Shared look of the composer toolbar's label-less pill triggers (agent,
// provider/model, thinking, permission): the input card is the border, so the
// trigger's own chrome is nothing. The label (a DIRECT child span) and the
// trailing chevron hide below sm, leaving the 44px icon-only button.
export const PILL_TRIGGER_CLASS =
  "size-11 shrink-0 justify-center gap-1.5 rounded-xl border-0 bg-transparent px-0 text-ui-base font-normal text-foreground-muted hover:bg-hover hover:text-foreground data-[state=open]:bg-hover sm:h-11 sm:w-auto sm:justify-start sm:px-2 md:h-7 md:px-1.5 md:text-ui-sm [&>svg:first-child]:size-4 [&>svg:first-child]:md:size-3.5 [&>svg:last-child]:size-3 [&>svg:last-child]:opacity-60 max-sm:[&>svg:last-child]:hidden max-sm:[&>span]:hidden!";
