// Shared look of the composer toolbar's label-less pill triggers (agent,
// provider/model, thinking, permission): the input card is the border, so the
// trigger's own chrome is nothing. The label (a DIRECT child span) and the
// trailing chevron hide below sm, leaving the 44px icon-only button.
export const PILL_TRIGGER_CLASS =
  "size-11 shrink-0 justify-center gap-1.5 rounded-xl border-0 bg-transparent px-0 text-ui-base font-normal text-foreground-muted hover:bg-hover hover:text-foreground data-[state=open]:bg-hover sm:h-11 sm:w-auto sm:justify-start sm:px-2 md:h-7 md:px-1.5 md:text-ui-sm [&>svg:first-child]:size-4 [&>svg:first-child]:md:size-3.5 [&>svg:last-child]:size-3 [&>svg:last-child]:opacity-60 max-sm:[&>svg:last-child]:hidden max-sm:[&>span]:hidden!";

/**
 * The same pill with its label and chevron kept at every width (the home composer's chips): an auto-width
 * 44px-high button below md, the compact 28px one from md up, exactly like the default from sm up.
 */
export const PILL_LABELLED_TRIGGER_CLASS =
  "h-11 shrink-0 justify-start gap-1.5 rounded-xl border-0 bg-transparent px-2 text-ui-base font-normal text-foreground-muted hover:bg-hover hover:text-foreground data-[state=open]:bg-hover md:h-7 md:px-1.5 md:text-ui-sm [&>svg:first-child]:size-4 [&>svg:first-child]:md:size-3.5 [&>svg:last-child]:size-3 [&>svg:last-child]:opacity-60";

/** The pill's trigger class: icon-only below sm by default, labelled at every width with `showLabel`. */
export function pillTriggerClass(showLabel: boolean): string {
  return showLabel ? PILL_LABELLED_TRIGGER_CLASS : PILL_TRIGGER_CLASS;
}
