import * as React from "react"
import { Checkbox as CheckboxPrimitive } from "radix-ui"

import { Icons } from "@/lib/icons"
import { cn } from "@/lib/utils"

/**
 * The themed checkbox: native ones paint in the OS accent color. A checked
 * and disabled box keeps its primary fill (just dimmed) so an always-on,
 * locked option reads as "on", not as an empty grey box; `hint` says why it is locked.
 */
function Checkbox({
  className,
  hint,
  disabled,
  ...props
}: React.ComponentProps<typeof CheckboxPrimitive.Root> & { hint?: string }) {
  return (
    <CheckboxPrimitive.Root
      data-slot="checkbox"
      disabled={disabled}
      title={disabled ? hint : undefined}
      className={cn(
        "peer inline-flex size-4 shrink-0 items-center justify-center rounded-md border border-input-border bg-background text-primary-foreground transition-colors outline-none hover:border-input-border-hover focus-visible:ring-3 focus-visible:ring-ring/50 disabled:cursor-not-allowed disabled:opacity-50 data-[state=checked]:border-primary data-[state=checked]:bg-primary",
        className
      )}
      {...props}
    >
      <CheckboxPrimitive.Indicator className="flex items-center justify-center">
        <Icons.check aria-hidden className="size-3" />
      </CheckboxPrimitive.Indicator>
    </CheckboxPrimitive.Root>
  )
}

export { Checkbox }
