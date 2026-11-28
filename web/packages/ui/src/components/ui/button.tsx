import * as React from "react"
import { cva, type VariantProps } from "class-variance-authority"
import { Slot } from "radix-ui"
import { Button as AstryxButton } from "@astryxdesign/core/Button"

import { cn } from "@/lib/utils"

const buttonVariants = cva(
  "group/button inline-flex shrink-0 items-center justify-center rounded-lg border border-transparent bg-clip-padding text-ui-base font-medium whitespace-nowrap transition outline-none select-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 active:not-aria-[haspopup]:translate-y-px disabled:pointer-events-none disabled:opacity-50 aria-invalid:border-destructive aria-invalid:ring-3 aria-invalid:ring-destructive/20 dark:aria-invalid:border-destructive/50 dark:aria-invalid:ring-destructive/40 [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
  {
    variants: {
      variant: {
        default: "bg-primary text-primary-foreground hover:bg-primary/80",
        outline:
          "border-border bg-background hover:bg-muted hover:text-foreground aria-expanded:bg-muted aria-expanded:text-foreground dark:border-input-border dark:bg-input/30 dark:hover:bg-input/50",
        secondary:
          "bg-secondary text-secondary-foreground hover:bg-[color-mix(in_oklch,var(--secondary),var(--foreground)_5%)] aria-expanded:bg-secondary aria-expanded:text-secondary-foreground",
        ghost:
          "hover:bg-muted hover:text-foreground aria-expanded:bg-muted aria-expanded:text-foreground dark:hover:bg-muted/50",
        destructive:
          "bg-destructive/10 text-destructive hover:bg-destructive/20 focus-visible:border-destructive/40 focus-visible:ring-destructive/20 dark:bg-destructive/20 dark:hover:bg-destructive/30 dark:focus-visible:ring-destructive/40",
        link: "text-primary underline-offset-4 hover:underline",
        // disabled:bg-muted/text-muted-foreground override the warning tint: the shared
        // disabled:opacity-50 above already halves this variant's already-translucent
        // bg-warning/10..20, compounding to a near-invisible, hue-shifted smudge (reads
        // as brown, not a dimmed orange) over a dark panel -- falling back to the flat
        // neutral muted color when disabled reads as a normal disabled button instead.
        // The dark:disabled: pair repeats the same override explicitly (reference-visual-
        // parity P3 screenshot review): Tailwind v4 sorts the `dark:` and `disabled:`
        // variants independently of source order, so a bare `disabled:bg-muted` loses
        // the cascade to `dark:bg-warning/20` once both the .dark ancestor and :disabled
        // apply at once -- the disabled Send button rendered the warning tint (translucent
        // orange, reading brown) in dark mode despite this same-looking rule.
        execute:
          "bg-warning/10 text-warning-text hover:bg-warning/20 focus-visible:border-warning/40 focus-visible:ring-warning/20 disabled:bg-muted disabled:text-muted-foreground dark:bg-warning/20 dark:hover:bg-warning/30 dark:focus-visible:ring-warning/40 dark:disabled:bg-muted dark:disabled:text-muted-foreground",
        approval:
          "bg-success/10 text-success hover:bg-success/20 focus-visible:border-success/40 focus-visible:ring-success/20 dark:bg-success/20 dark:hover:bg-success/30 dark:focus-visible:ring-success/40",
        quiet:
          "text-foreground-muted hover:text-foreground aria-expanded:text-foreground",
      },
      size: {
        default:
          "h-8 gap-1.5 px-2.5 has-data-[icon=inline-end]:pr-2 has-data-[icon=inline-start]:pl-2",
        xs: "h-6 gap-1 px-2 text-ui-sm in-data-[slot=button-group]:rounded-lg has-data-[icon=inline-end]:pr-1.5 has-data-[icon=inline-start]:pl-1.5 [&_svg:not([class*='size-'])]:size-3",
        sm: "h-7 gap-1 px-2.5 text-ui-sm in-data-[slot=button-group]:rounded-lg has-data-[icon=inline-end]:pr-1.5 has-data-[icon=inline-start]:pl-1.5 [&_svg:not([class*='size-'])]:size-3.5",
        lg: "h-9 gap-1.5 px-2.5 has-data-[icon=inline-end]:pr-2 has-data-[icon=inline-start]:pl-2",
        icon: "size-8",
        "icon-xs":
          "size-6 in-data-[slot=button-group]:rounded-lg [&_svg:not([class*='size-'])]:size-3",
        "icon-sm":
          "size-7 in-data-[slot=button-group]:rounded-lg",
        "icon-lg": "size-9",
      },
    },
    defaultVariants: {
      variant: "default",
      size: "default",
    },
  }
)

// Astryx Button (Wave 0) renders every non-asChild button. Mapping:
//   default -> primary, outline/secondary -> secondary, ghost -> ghost,
//   destructive -> destructive.
// Astryx has no link / execute / approval / quiet variants: those render the
// Astryx "ghost" element with our Tailwind classes on top (same classes as
// buttonVariants). Astryx has only sm/md/lg heights: xs/sm -> "sm", default ->
// "md", lg -> "lg"; xs and icon-xs keep their Tailwind height/size utilities.
// asChild cannot be expressed (Astryx renders its own <button>/<a>), so that
// path keeps the previous Radix Slot implementation.
const ASTRYX_VARIANT = {
  default: "primary",
  outline: "secondary",
  secondary: "secondary",
  ghost: "ghost",
  destructive: "destructive",
} as const

const ASTRYX_SIZE = {
  default: "md",
  xs: "sm",
  sm: "sm",
  lg: "lg",
  icon: "md",
  "icon-xs": "sm",
  "icon-sm": "sm",
  "icon-lg": "lg",
} as const

const TAILWIND_ONLY_VARIANTS = new Set(["link", "execute", "approval", "quiet"])
const SIZE_OVERRIDE_CLASS: Record<string, string> = {
  xs: "h-6",
  "icon-xs": "size-6",
  "icon-sm": "size-7",
}

type ButtonProps = React.ComponentProps<"button"> &
  VariantProps<typeof buttonVariants> & {
    asChild?: boolean
  }

function Button({ asChild = false, ...props }: ButtonProps) {
  if (asChild) {
    const { className, variant = "default", size = "default", ...rest } = props
    return (
      <Slot.Root
        data-slot="button"
        data-variant={variant}
        data-size={size}
        className={cn(buttonVariants({ variant, size, className }))}
        {...rest}
      />
    )
  }
  return <AstryxBackedButton {...props} />
}

function AstryxBackedButton({
  className,
  variant = "default",
  size = "default",
  ...props
}: Omit<ButtonProps, "asChild">) {
  const v = variant ?? "default"
  const sz = size ?? "default"
  const { children, disabled, type, ref, onClick, ...rest } = props as typeof props & {
    ref?: React.Ref<HTMLButtonElement>
  }
  const iconOnly = String(sz).startsWith("icon")
  const tailwindOnly = TAILWIND_ONLY_VARIANTS.has(v as string)
  const aria = (rest as { "aria-label"?: string })["aria-label"]
  const label =
    aria ?? (typeof children === "string" || typeof children === "number" ? String(children) : "")

  // Astryx renders an always-present hidden role="status" live region that only
  // speaks while isLoading (never used here). Mark it presentational so it does
  // not add a stray "status" landmark next to every button.
  const localRef = React.useRef<HTMLButtonElement | null>(null)
  const setRef = React.useCallback(
    (node: HTMLButtonElement | null) => {
      localRef.current = node
      if (typeof ref === "function") ref(node)
      else if (ref) (ref as React.RefObject<HTMLButtonElement | null>).current = node
    },
    [ref]
  )
  React.useLayoutEffect(() => {
    localRef.current
      ?.querySelectorAll(':scope > [role="status"]')
      .forEach((n) => n.setAttribute("role", "presentation"))
  })

  return (
    <AstryxButton
      {...(rest as object)}
      ref={setRef}
      data-slot="button"
      data-variant={v}
      data-size={sz}
      label={label}
      variant={tailwindOnly ? "ghost" : ASTRYX_VARIANT[v as keyof typeof ASTRYX_VARIANT]}
      size={ASTRYX_SIZE[sz as keyof typeof ASTRYX_SIZE]}
      type={type ?? "button"}
      // Astryx disables with aria-disabled (still focusable); the native
      // attribute is forwarded too so `disabled` keeps today's semantics.
      isDisabled={disabled}
      {...({ disabled } as object)}
      onClick={onClick}
      isIconOnly={iconOnly}
      icon={iconOnly ? children : undefined}
      className={cn(
        tailwindOnly && buttonVariants({ variant: v, size: sz }),
        SIZE_OVERRIDE_CLASS[sz as string],
        className
      )}
    >
      {iconOnly ? undefined : children}
    </AstryxButton>
  )
}

export { Button, buttonVariants }
