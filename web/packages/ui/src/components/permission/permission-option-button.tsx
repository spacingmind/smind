import { Button, type buttonVariants } from "@/components/ui/button";
import type { VariantProps } from "class-variance-authority";
import type { PendingPermission } from "@/hooks/use-run-timeline";
import { cn } from "@/lib/utils";

type ButtonVariant = NonNullable<VariantProps<typeof buttonVariants>["variant"]>;

/**
 * The permission surfaces' shared button size: 44px (WCAG 2.5.5 AAA /
 * Apple HIG's touch minimum) below the Item 21 compact breakpoint, 32px
 * (`h-8`, 14px text) above it (desktop design review: the old dense
 * `h-6` was too small a target for the decision these buttons carry).
 * Plain Tailwind responsive classes rather than a JS `isMobile` prop
 * threaded through every card -- `md:` already keys off the same 768px
 * breakpoint `useIsMobile()` does, and `cn`'s `twMerge` resolves the
 * unprefixed/`md:`-prefixed pair without conflict.
 */
export const COMPACT_TOUCH_BUTTON_CLASS = "h-11 px-3 text-ui-base md:h-8 md:px-3 md:text-ui-base";

/**
 * The Button variant one option renders with, from its ACP `kind`
 * (`allow_once | allow_always | reject_once | reject_always` -- the wire
 * already carries this, per lib/types.ts, and the pre-Item-11 UI ignored
 * it entirely).
 *
 * `isRecommended` -- true only for the first `allow_*` option in the
 * list -- is what makes the primary action visually obvious; ACP/agent
 * conventions put the suggested choice first, so "first allow" is a
 * reasonable default absent an explicit "recommended" flag on the wire.
 * It renders with the `approval` variant (visual-identity-console Item
 * 4) rather than `default` -- a recommended allow is a confirm action
 * with its own meaning, not a generic primary button. Reject renders
 * outlined like other allows but with destructive text
 * (`optionClassName`): Deny is clearly present but quieter than Allow
 * (desktop design review), not a second filled button of equal weight.
 */
export function optionVariant(kind: string, isRecommended: boolean): ButtonVariant {
  if (kind.startsWith("reject")) return "outline";
  if (isRecommended) return "approval";
  return "outline";
}

/** The extra classes an option's Button needs beyond its variant: destructive text for reject kinds, nothing otherwise. */
export function optionClassName(kind: string): string {
  return kind.startsWith("reject") ? "text-destructive hover:text-destructive" : "";
}

/** The first allow-kind option's id, or undefined if there is none -- see optionVariant's doc comment. */
export function recommendedOptionId(options: PendingPermission["options"]): string | undefined {
  return options.find((o) => o.kind.startsWith("allow"))?.id;
}

export function PermissionOptionButton({
  option,
  isRecommended,
  disabled,
  onClick,
  testId,
}: {
  option: { id: string; label: string; kind: string };
  isRecommended: boolean;
  disabled: boolean;
  onClick: () => void;
  testId?: string;
}) {
  return (
    <Button
      type="button"
      variant={optionVariant(option.kind, isRecommended)}
      size="sm"
      className={cn(COMPACT_TOUCH_BUTTON_CLASS, optionClassName(option.kind))}
      disabled={disabled}
      data-testid={testId}
      data-option-kind={option.kind}
      onClick={onClick}
    >
      {option.label}
    </Button>
  );
}
