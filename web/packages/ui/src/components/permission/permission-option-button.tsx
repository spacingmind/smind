import { Button, type buttonVariants } from "@/components/ui/button";
import type { VariantProps } from "class-variance-authority";
import type { PendingPermission } from "@/hooks/use-run-timeline";

type ButtonVariant = NonNullable<VariantProps<typeof buttonVariants>["variant"]>;

/**
 * The permission surfaces' shared button size: 44px (WCAG 2.5.5 AAA /
 * Apple HIG's touch minimum) below the Item 21 compact breakpoint, the
 * original dense `h-6` above it. Plain Tailwind responsive classes rather
 * than a JS `isMobile` prop threaded through every card -- `md:` already
 * keys off the same 768px breakpoint `useIsMobile()` does, and `cn`'s
 * `twMerge` resolves the unprefixed/`md:`-prefixed pair without conflict.
 */
export const COMPACT_TOUCH_BUTTON_CLASS = "h-11 px-3 text-sm md:h-6 md:px-2 md:text-xs";

/**
 * The Button variant one option renders with, from its ACP `kind`
 * (`allow_once | allow_always | reject_once | reject_always` -- the wire
 * already carries this, per lib/types.ts, and the pre-Item-11 UI ignored
 * it entirely).
 *
 * `isRecommended` -- true only for the first `allow_*` option in the
 * list -- is what makes the primary action visually obvious rather than
 * every option looking equally weighted; ACP/agent conventions put the
 * suggested choice first, so "first allow" is a reasonable default absent
 * an explicit "recommended" flag on the wire. It renders with the
 * `approval` variant (visual-identity-console Item 4) rather than
 * `default` -- a recommended allow is a confirm action with its own
 * meaning, not a generic primary button. Reject keeps `destructive`,
 * unchanged: that meaning was already correct.
 */
export function optionVariant(kind: string, isRecommended: boolean): ButtonVariant {
  if (kind.startsWith("reject")) return "destructive";
  if (isRecommended) return "approval";
  return "outline";
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
      className={COMPACT_TOUCH_BUTTON_CLASS}
      disabled={disabled}
      data-testid={testId}
      data-option-kind={option.kind}
      onClick={onClick}
    >
      {option.label}
    </Button>
  );
}
