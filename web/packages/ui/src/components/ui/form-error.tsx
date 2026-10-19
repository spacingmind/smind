import { cn } from "@/lib/utils";

/**
 * An inline form error message: `role="alert"` so screen readers announce it
 * when a submit fails, in the destructive text token. Renders nothing while
 * the message is empty so callers can pass error state directly.
 */
export function FormError({
  message,
  className,
  testId,
  size = "base",
}: {
  message: string | null | undefined;
  className?: string;
  testId?: string;
  size?: "base" | "sm";
}) {
  if (!message) return null;
  return (
    <p role="alert" data-testid={testId} className={cn(size === "sm" ? "text-ui-sm" : "text-ui-base", "text-destructive", className)}>
      {message}
    </p>
  );
}
