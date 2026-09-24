import type { ComponentProps, ReactNode } from "react";
import { AlertCircle, AlertTriangle, CheckCircle2, Info } from "lucide-react";

import { cn } from "@/lib/utils";

export type AlertVariant = "default" | "info" | "success" | "warning" | "error";

const VARIANT_ICON: Record<AlertVariant, typeof Info> = {
  default: Info,
  info: Info,
  success: CheckCircle2,
  warning: AlertTriangle,
  error: AlertCircle,
};

const VARIANT_CLASSES: Record<AlertVariant, string> = {
  default: "border-border text-foreground",
  info: "border-status-running/40 text-status-running",
  success: "border-status-success/40 text-status-success",
  warning: "border-status-warning/40 text-status-warning",
  error: "border-status-danger/40 text-status-danger",
};

/**
 * A page-level alert (refs/paseo/docs/design.md §11): a 1px tinted
 * border, transparent background, a small variant-tinted icon, the title
 * in the variant accent, the description in the muted foreground. Actions
 * (a retry button, a link) go in `children` as `<Button variant="outline"
 * size="sm">` -- low-frequency recovery actions stay quiet next to the
 * alert's own accent, per the same section. One `<Alert>` per region.
 *
 * `error` gets `role="alert"` (assertive -- something just went wrong and
 * needs the user's attention now); every other variant gets `role="status"`
 * (polite -- informational, doesn't interrupt).
 */
export function Alert({
  variant = "default",
  title,
  description,
  children,
  className,
  testId,
  ...props
}: {
  variant?: AlertVariant;
  title?: ReactNode;
  description?: ReactNode;
  children?: ReactNode;
  className?: string;
  testId?: string;
} & Omit<ComponentProps<"div">, "className" | "title">) {
  const Icon = VARIANT_ICON[variant];
  return (
    <div
      data-testid={testId}
      data-slot="alert"
      data-variant={variant}
      role={variant === "error" ? "alert" : "status"}
      className={cn(
        "flex gap-2 rounded-lg border bg-transparent px-3 py-2 text-ui-sm",
        VARIANT_CLASSES[variant],
        className,
      )}
      {...props}
    >
      <Icon className="mt-0.5 size-4 shrink-0" />
      <div className="min-w-0 flex-1">
        {title && <div className="font-medium">{title}</div>}
        {description && <div className="text-foreground-muted">{description}</div>}
        {children && <div className="mt-2 flex items-center gap-2">{children}</div>}
      </div>
    </div>
  );
}
