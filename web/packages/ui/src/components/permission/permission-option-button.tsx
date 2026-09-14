import { Button, type buttonVariants } from "@/components/ui/button";
import type { VariantProps } from "class-variance-authority";
import type { PendingPermission } from "@/hooks/use-run-timeline";

type ButtonVariant = NonNullable<VariantProps<typeof buttonVariants>["variant"]>;

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
 * an explicit "recommended" flag on the wire.
 */
export function optionVariant(kind: string, isRecommended: boolean): ButtonVariant {
  if (kind.startsWith("reject")) return "destructive";
  if (isRecommended) return "default";
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
      className="h-6 px-2 text-xs"
      disabled={disabled}
      data-testid={testId}
      data-option-kind={option.kind}
      onClick={onClick}
    >
      {option.label}
    </Button>
  );
}
