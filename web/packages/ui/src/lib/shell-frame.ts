/**
 * Tailwind class fragments for the light-theme framed shell: the content
 * inset (gap between the panel and the window edge) and the panel's rounded
 * border + soft shadow, with the dark-theme resets folded in as `dark:`
 * variants.
 */
export const SHELL_FRAME_INSET_CLASS = "p-1.5 pl-0 dark:p-0";
export const SHELL_FRAME_PANEL_CLASS =
  "rounded-xl border shadow-[0_1px_3px_rgba(0,0,0,0.08),0_4px_12px_rgba(0,0,0,0.04)] dark:rounded-none dark:rounded-tl-xl dark:border-0 dark:border-t dark:border-l dark:shadow-none";
