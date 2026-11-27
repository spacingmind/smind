import type { ComponentProps, ReactNode } from "react";

import { cn } from "@/lib/utils";

export function SettingsGroup({
  title,
  description,
  actions,
  children,
  className,
  testId,
}: {
  title?: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
  children: ReactNode;
  className?: string;
  testId?: string;
}) {
  return (
    <section
      data-testid={testId}
      data-slot="settings-group"
      className={cn("flex flex-col gap-2", className)}
    >
      {(title || description || actions) && (
        <div className="flex items-start justify-between gap-3">
          <div className="flex min-w-0 flex-col gap-1">
            {title && <h3 className="text-ui-base font-medium text-foreground">{title}</h3>}
            {description && <p className="text-ui-sm text-foreground-muted">{description}</p>}
          </div>
          {actions && <div className="shrink-0">{actions}</div>}
        </div>
      )}
      <div className="overflow-hidden rounded-xl border border-border bg-card [&>*+*]:border-t [&>*+*]:border-border">
        {children}
      </div>
    </section>
  );
}

export function SettingsRow({
  label,
  description,
  control,
  children,
  className,
  testId,
}: {
  label: ReactNode;
  description?: ReactNode;
  control?: ReactNode;
  children?: ReactNode;
  className?: string;
  testId?: string;
}) {
  return (
    <div
      data-testid={testId}
      data-slot="settings-row"
      className={cn(
        "flex flex-col gap-2 px-4 py-3 sm:flex-row sm:items-center sm:justify-between sm:gap-4",
        className
      )}
    >
      <div className="min-w-0">
        <p className="text-ui-base text-foreground">{label}</p>
        {description && <p className="text-ui-sm text-foreground-muted">{description}</p>}
      </div>
      {control && <div className="shrink-0">{control}</div>}
      {children && <div className="w-full">{children}</div>}
    </div>
  );
}

export function SettingsEmptyCard({
  title,
  description,
  actions,
  testId,
}: {
  title: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
  testId?: string;
}) {
  return (
    <div
      data-testid={testId}
      data-slot="settings-empty-card"
      className="flex flex-col items-center gap-2 rounded-xl border border-dashed border-border px-4 py-8 text-center"
    >
      <p className="text-ui-base font-medium text-foreground">{title}</p>
      {description && (
        <p className="max-w-sm text-ui-sm text-foreground-muted">{description}</p>
      )}
      {actions && <div className="mt-2 flex flex-wrap justify-center gap-2">{actions}</div>}
    </div>
  );
}

export function SettingsStatStrip({
  stats,
  testId,
}: {
  stats: { label: string; value: ReactNode }[];
  testId?: string;
}) {
  return (
    <div
      data-testid={testId}
      data-slot="settings-stat-strip"
      className="overflow-hidden rounded-xl border border-border bg-card"
    >
      <div className="-m-px grid grid-cols-2 sm:grid-cols-[repeat(auto-fit,minmax(0,1fr))]">
        {stats.map((stat) => (
          <div key={stat.label} className="border-b border-r border-border px-4 py-3">
            <p className="text-ui-sm text-foreground-muted">{stat.label}</p>
            <p className="text-ui-lg font-semibold tabular-nums text-foreground">{stat.value}</p>
          </div>
        ))}
      </div>
    </div>
  );
}

/** One list card for a Settings section: the rows are `<li>`s divided by hairlines, so a list reads as a single card instead of one card per row. */
export function SettingsList({
  children,
  className,
  testId,
  ...rest
}: {
  children: ReactNode;
  className?: string;
  testId?: string;
} & Omit<ComponentProps<"ul">, "className" | "children">) {
  return (
    <ul
      data-testid={testId}
      data-slot="settings-list"
      className={cn(
        "m-0 list-none overflow-hidden rounded-xl border border-border bg-card p-0 [&>*+*]:border-t [&>*+*]:border-border",
        className,
      )}
      {...rest}
    >
      {children}
    </ul>
  );
}

export function SettingsListRow({
  children,
  className,
  testId,
  ...rest
}: {
  children: ReactNode;
  className?: string;
  testId?: string;
} & Omit<ComponentProps<"li">, "className" | "children">) {
  return (
    <li
      data-testid={testId}
      data-slot="settings-list-row"
      className={cn("px-4 py-3", className)}
      {...rest}
    >
      {children}
    </li>
  );
}
