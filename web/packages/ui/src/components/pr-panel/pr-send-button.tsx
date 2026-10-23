import { Button } from "@/components/ui/button";
import { Icons } from "@/lib/icons";

/** The row-level "Send to chat" icon button; a spinner while its log tail is being read. */
export function PRSendButton({
  label,
  onClick,
  disabled,
  pending,
  testId,
}: {
  label: string;
  onClick: () => void;
  disabled?: boolean;
  pending?: boolean;
  testId?: string;
}) {
  return (
    <Button
      type="button"
      variant="ghost"
      size="icon-xs"
      className="text-foreground-subtle"
      aria-label={label}
      title={label}
      disabled={disabled || pending}
      aria-busy={pending || undefined}
      onClick={(e) => {
        e.stopPropagation();
        onClick();
      }}
      data-testid={testId}
    >
      {pending ? <Icons.spinner aria-hidden className="animate-spin motion-reduce:animate-none" /> : <Icons.mention aria-hidden />}
    </Button>
  );
}
