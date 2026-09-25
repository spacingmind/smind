import { clsx, type ClassValue } from "clsx"
import { extendTailwindMerge } from "tailwind-merge"

/**
 * tailwind-merge's default config has no idea `text-ui-xl/lg/base/caption/
 * sm/xs` (index.css's ZCode-derived type scale, zcode-visual-parity plan
 * P1 Step 3) are font-size utilities -- its built-in `font-size` group only
 * recognizes Tailwind's own scale keywords (`xs`, `sm`, `base`, `lg`, ...).
 * Unrecognized `text-{word}` utilities fall through to the `text-color`
 * group instead (its catch-all match), so by default `cn("text-ui-xs",
 * "text-accent-foreground")` -- a font-size class followed by a color
 * class, the exact shape `cva()`'s `className` merge produces everywhere a
 * selected/active variant adds a color on top of a sized control -- gets
 * "conflict"-resolved down to just the color, silently dropping the size
 * and leaving the element to inherit whatever ambient font-size surrounds
 * it. Registering the scale under `font-size` here fixes it at the root:
 * a `text-ui-*` class now only conflicts with another `text-ui-*` class
 * (last one wins, same as Tailwind's own scale), never with a color class.
 * `lib/utils.test.ts` is the regression test.
 */
const twMerge = extendTailwindMerge({
  extend: {
    classGroups: {
      "font-size": [{ text: ["ui-xl", "ui-lg", "ui-base", "ui-caption", "ui-sm", "ui-xs"] }],
    },
  },
})

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}
