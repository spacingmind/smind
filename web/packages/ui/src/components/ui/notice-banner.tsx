import type { ComponentProps, ReactNode } from "react";

import { Button } from "@/components/ui/button";
import { Icons } from "@/lib/icons";
import { cn } from "@/lib/utils";

export type NoticeTone = "info" | "warning" | "error";

const TONE_CLASS: Record<NoticeTone, string> = {
  info: "border-border bg-muted/40",
  warning: "border-warning/40 bg-warning/10",
  error: "border-destructive/40 bg-destructive/10",
};

const TONE_ICON_CLASS: Record<NoticeTone, string> = {
  info: "text-foreground-muted",
  warning: "text-warning",
  error: "text-destructive",
};

const TONE_ICON = { info: Icons.info, warning: Icons.warning, error: Icons.error } as const;

/** A full-width notice row: tone icon, text, pill actions, optional dismiss; `footer` sits below the top line. */
export function NoticeBanner({
  tone = "info",
  testId,
  children,
  actions,
  footer,
  onDismiss,
  dismissLabel = "Dismiss",
  dismissTitle,
  dismissTestId,
  dismissDisabled,
  className,
}: {
  tone?: NoticeTone;
  testId?: string;
  children: ReactNode;
  actions?: ReactNode;
  footer?: ReactNode;
  onDismiss?: () => void;
  dismissLabel?: string;
  dismissTitle?: string;
  dismissTestId?: string;
  dismissDisabled?: boolean;
  className?: string;
}) {
  const ToneIcon = TONE_ICON[tone];
  return (
    <div
      data-testid={testId}
      data-slot="notice-banner"
      data-tone={tone}
      role={tone === "error" ? "alert" : "status"}
      className={cn("flex flex-col gap-1.5 border-b px-4 py-2 text-ui-base", TONE_CLASS[tone], className)}
    >
      <div className="flex min-h-9 items-center gap-2.5">
        <ToneIcon aria-hidden className={cn("shrink-0", TONE_ICON_CLASS[tone])} />
        <span className="min-w-0 flex-1">{children}</span>
        {actions && <div className="flex shrink-0 items-center gap-2">{actions}</div>}
        {onDismiss && (
          <Button
            size="icon-xs"
            variant="ghost"
            aria-label={dismissLabel}
            title={dismissTitle}
            data-testid={dismissTestId}
            disabled={dismissDisabled}
            onClick={onDismiss}
          >
            <Icons.close aria-hidden />
          </Button>
        )}
      </div>
      {footer}
    </div>
  );
}

/** A pill-shaped banner action: "primary" is the filled call to action, "secondary" the outlined alternative. */
export function NoticeAction({
  variant = "primary",
  className,
  ...buttonProps
}: Omit<ComponentProps<typeof Button>, "variant" | "size"> & { variant?: "primary" | "secondary" }) {
  return <Button size="sm" variant={variant === "primary" ? "default" : "outline"} className={cn("rounded-full", className)} {...buttonProps} />;
}
