/**
 * Class strings shared by the task composer and the home landing composer, so the two
 * surfaces stay visually identical. Send buttons are circular: 44px touch targets below md
 * (Item 21), 32px from md up.
 */
export const COMPOSER_CARD_CLASS =
  "relative flex flex-col rounded-2xl border border-input-border bg-input transition-colors hover:border-input-border-hover focus-within:border-input-border-focused focus-within:bg-input-focused";

export const COMPOSER_ACTION_BUTTON_CLASS = "size-11 rounded-full md:size-8";
