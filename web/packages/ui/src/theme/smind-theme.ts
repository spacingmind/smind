/**
 * The sMind theme for Astryx (ADR-0064, Wave 0, AC4).
 *
 * Extends the neutral Astryx theme and overrides only what sMind's tokens
 * (src/index.css `:root` and `.dark`, vocabulary in docs/design.md) have an
 * Astryx slot for. Every colour is a [light, dark] tuple. The app never
 * compiles this at runtime: scripts/build-smind-theme.mjs builds it into
 * src/theme/smind-theme.css.
 *
 * Token -> Astryx slot
 * ---------------------------------------------------------------------------
 * --background              -> --color-background-body
 * --card                    -> --color-background-card
 * --popover                 -> --color-background-popover
 * --panel                   -> --color-background-surface
 * --muted                   -> --color-background-muted
 * --foreground              -> --color-text-primary
 * --muted-foreground        -> --color-text-secondary, --color-icon-secondary
 * --foreground-subtlest     -> --color-text-disabled, --color-icon-disabled
 * --primary                 -> --color-accent, --color-text-accent, --color-icon-accent
 * --primary-foreground      -> --color-on-accent
 * --secondary               -> --color-accent-muted
 * --border                  -> --color-border
 * --border-hover            -> --color-border-emphasized
 * --hover                   -> --color-overlay-hover
 * --selected                -> --color-overlay-pressed
 * --success                 -> --color-success, --color-text-success
 * --success-foreground      -> --color-on-success
 * --success-surface         -> --color-success-muted
 * --success-surface-foreground -> --color-on-success-muted
 * --warning                 -> --color-warning
 * --warning-foreground      -> --color-on-warning
 * --warning-surface         -> --color-warning-muted
 * --warning-surface-foreground -> --color-on-warning-muted
 * --warning-text            -> --color-text-warning
 * --destructive             -> --color-error, --color-text-error
 * --destructive-foreground  -> --color-on-error
 * --destructive-surface     -> --color-error-muted
 * --destructive-surface-foreground -> --color-on-error-muted
 * Tailwind rounded-md       -> --radius-inner      (0.375rem)
 * Tailwind rounded-lg       -> --radius-element    (0.5rem, basic controls)
 * Tailwind rounded-xl       -> --radius-container  (0.75rem, first container)
 * Tailwind rounded-2xl      -> --radius-page       (1rem, dialogs)
 * --font-sans               -> body, heading font family (Geist Variable,
 *                              loaded by @fontsource-variable/geist)
 *
 * Not mapped (no Astryx slot, or out of Wave 0 scope; they stay in index.css):
 *   --ring, --sidebar-ring  Astryx has no focus-ring colour token. Its
 *                           components show focus with :focus-visible plus a
 *                           shared last-input state, so the ring stays
 *                           keyboard-only (AC7); nothing to set here.
 *   --input, --input-bg, --input-bg-focused, --input-border,
 *   --input-border-hover, --input-border-focused
 *                           (field chrome: Astryx takes it from
 *                           --color-border / --color-background-*)
 *   --card-foreground, --popover-foreground, --secondary-foreground,
 *   --accent, --accent-foreground, --muted-foreground's sidebar twins,
 *   --sidebar*, --chart-*, --surface, --surface-hover, --card-selected,
 *   --card-border, --popover-header, --popover-border, --menu, --menu-hover,
 *   --header, --tab*, --tag, --toast, --tooltip*, --foreground-subtle,
 *   --foreground-inverse, --foreground-muted, --brand, --diff-*, --git-*,
 *   --find-highlight*, --interaction-*, --progress-surface*, --idle-task*,
 *   --terminal-*, --syntax-*, --trajectory-*, --user-message-bubble,
 *   --usage-*, --context-breakdown-*, --background-alt,
 *   --background-win-alt    (sMind-specific or later-wave tokens)
 *   --elevation-shadow-*    (shadows stay sMind's; Wave 0 leaves Astryx's)
 *   --duration-*, --ui-font-size, --content-size
 *                           (motion and type scale: not in Wave 0)
 */
import { defineTheme } from "@astryxdesign/core/theme";
import { neutralTheme } from "@astryxdesign/theme-neutral";

type Pair = [light: string, dark: string];

/** Tailwind v4 neutral-800 / neutral-300, which index.css uses for --foreground. */
const foreground: Pair = ["oklch(0.269 0 0)", "oklch(0.87 0 0)"];

/** Colour tokens, [light, dark]: values copied from index.css :root and .dark. */
export const smindColorTokens = {
  "--color-background-body": ["#f8f8f8", "#161616"],
  "--color-background-card": ["#ffffff", "#2b2b2b"],
  "--color-background-popover": ["#ffffff", "#2b2b2b"],
  "--color-background-surface": ["#ffffff", "#202020"],
  "--color-background-muted": ["oklch(0.97 0 0)", "oklch(0.269 0 0)"],

  "--color-text-primary": foreground,
  "--color-text-secondary": ["oklch(0.5 0 0)", "oklch(0.708 0 0)"],
  "--color-text-disabled": [
    "color-mix(in oklab, oklch(0.269 0 0) 40%, transparent)",
    "color-mix(in oklab, oklch(0.87 0 0) 30%, transparent)",
  ],
  "--color-icon-primary": foreground,
  "--color-icon-secondary": ["oklch(0.5 0 0)", "oklch(0.708 0 0)"],
  "--color-icon-disabled": [
    "color-mix(in oklab, oklch(0.269 0 0) 40%, transparent)",
    "color-mix(in oklab, oklch(0.87 0 0) 30%, transparent)",
  ],

  "--color-accent": ["#000000", "#ffffff"],
  "--color-text-accent": ["#000000", "#ffffff"],
  "--color-icon-accent": ["#000000", "#ffffff"],
  "--color-on-accent": ["#ffffff", "#000000"],
  "--color-accent-muted": ["#e6e6e6", "#363636"],

  "--color-border": ["rgba(13, 13, 13, 0.1)", "rgba(255, 255, 255, 0.1)"],
  "--color-border-emphasized": ["rgba(13, 13, 13, 0.15)", "rgba(255, 255, 255, 0.15)"],
  "--color-overlay-hover": ["rgba(13, 13, 13, 0.05)", "rgba(255, 255, 255, 0.05)"],
  "--color-overlay-pressed": ["rgba(13, 13, 13, 0.05)", "rgba(255, 255, 255, 0.1)"],

  "--color-success": ["#167333", "#46bf72"],
  "--color-text-success": ["#167333", "#46bf72"],
  "--color-on-success": ["#ffffff", "#000000"],
  "--color-success-muted": ["#eaf7ee", "rgba(70, 191, 114, 0.16)"],
  "--color-on-success-muted": ["#166b32", "#87d9a4"],

  "--color-warning": ["#e07b00", "#ff8a30"],
  "--color-on-warning": ["#ffffff", "#000000"],
  "--color-warning-muted": [
    "color-mix(in srgb, #e07b00 12%, transparent)",
    "color-mix(in srgb, #ff8a30 16%, transparent)",
  ],
  "--color-on-warning-muted": ["#914e00", "#ffa159"],
  "--color-text-warning": ["#a84a00", "#ff8a30"],

  "--color-error": ["#ba2424", "#ff5c5c"],
  "--color-text-error": ["#ba2424", "#ff5c5c"],
  "--color-on-error": ["#ffffff", "#ffffff"],
  "--color-error-muted": [
    "color-mix(in srgb, #ba2424 10%, transparent)",
    "color-mix(in srgb, #ff5c5c 16%, transparent)",
  ],
  "--color-on-error-muted": ["#b42020", "#ff8585"],
} satisfies Record<string, Pair>;

const geistFallbacks = "sans-serif";

export const smindTheme = defineTheme({
  name: "smind",
  extends: neutralTheme,
  typography: {
    body: { family: "Geist Variable", fallbacks: geistFallbacks },
    heading: { family: "Geist Variable", fallbacks: geistFallbacks },
  },
  tokens: {
    ...smindColorTokens,
    "--radius-inner": "0.375rem",
    "--radius-element": "0.5rem",
    "--radius-container": "0.75rem",
    "--radius-page": "1rem",
  },
});
