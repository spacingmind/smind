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
  as: Tag = "p",
  silent = false,
}: {
  message: string | null | undefined;
  className?: string;
  testId?: string;
  size?: "base" | "sm";
  /** `span` where the message sits inside a `<label>` (a `<p>` there is invalid markup). */
  as?: "p" | "span";
  /** No `role="alert"`: for live validation while typing, which must not interrupt a screen reader. */
  silent?: boolean;
}) {
  if (!message) return null;
  return (
    <Tag
      role={silent ? undefined : "alert"}
      data-testid={testId}
      className={cn(size === "sm" ? "text-ui-sm" : "text-ui-base", "text-destructive", className)}
    >
      {message}
    </Tag>
  );
}
