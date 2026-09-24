import { useEffect, useState } from "react";
import { AlertCircle, AlertTriangle, CheckCircle2, Info } from "lucide-react";

import { cn } from "@/lib/utils";
import type { AlertVariant } from "@/components/ui/alert";

export interface ToastOptions {
  id?: string;
  variant?: AlertVariant;
  title: string;
  description?: string;
  /** Auto-dismiss after this many ms; 0 disables auto-dismiss. Defaults to 4000. */
  durationMs?: number;
}

interface ToastItem {
  id: string;
  variant: AlertVariant;
  title: string;
  description?: string;
  durationMs: number;
}

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

/*
 * A module-level pub-sub, not React state -- `toast(...)` is meant to be
 * callable from anywhere (an RPC success handler, a keyboard action, a
 * hook that has no JSX of its own), the same shape as every other
 * fire-and-forget toast API. `<Toaster />` (mounted once, near main.tsx's
 * root) is the sole subscriber that turns this into rendered DOM; nothing
 * about this primitive depends on where in the tree it's called from.
 *
 * No consumer wires this up yet (see docs/plans/active/ui-redesign-parity.md's
 * Item 2 Decisions) -- it's infrastructure for later items (commit/PR
 * success, composer errors) to adopt without inventing their own toast
 * shape first.
 */
let toasts: ToastItem[] = [];
const listeners = new Set<(items: ToastItem[]) => void>();
let nextId = 0;

function emit() {
  for (const listener of listeners) listener(toasts);
}

/** Queues a toast; returns its id (for an early `dismissToast` call). */
export function toast(options: ToastOptions): string {
  const id = options.id ?? `toast-${++nextId}`;
  toasts = [
    ...toasts,
    {
      id,
      variant: options.variant ?? "default",
      title: options.title,
      description: options.description,
      durationMs: options.durationMs ?? 4000,
    },
  ];
  emit();
  return id;
}

export function dismissToast(id: string): void {
  toasts = toasts.filter((t) => t.id !== id);
  emit();
}

/** Test-only: clears every queued toast so one test's `toast()` calls can't leak into the next -- there is no React tree to unmount this module-level queue. */
export function clearToasts(): void {
  toasts = [];
  emit();
}

/**
 * The toast host: subscribes to the module-level queue above and renders
 * whatever's currently queued, bottom-right, auto-dismissing each on its
 * own timer. Mount exactly one of these (main.tsx, alongside
 * ThemeProvider/TooltipProvider) -- a second instance would just render
 * the same queue twice.
 */
export function Toaster() {
  const [items, setItems] = useState<ToastItem[]>(toasts);

  useEffect(() => {
    listeners.add(setItems);
    return () => {
      listeners.delete(setItems);
    };
  }, []);

  useEffect(() => {
    const timers = items
      .filter((t) => t.durationMs > 0)
      .map((t) => setTimeout(() => dismissToast(t.id), t.durationMs));
    return () => {
      for (const timer of timers) clearTimeout(timer);
    };
  }, [items]);

  if (items.length === 0) return null;

  return (
    <div
      data-testid="toast-host"
      className="pointer-events-none fixed bottom-4 right-4 z-50 flex w-80 flex-col gap-2"
    >
      {items.map((t) => {
        const Icon = VARIANT_ICON[t.variant];
        return (
          <div
            key={t.id}
            data-testid="toast"
            data-variant={t.variant}
            role={t.variant === "error" ? "alert" : "status"}
            className={cn(
              "pointer-events-auto flex gap-2 rounded-lg border bg-surface-1 px-3 py-2 text-ui-sm shadow-md",
              VARIANT_CLASSES[t.variant],
            )}
          >
            <Icon className="mt-0.5 size-4 shrink-0" />
            <div className="min-w-0 flex-1">
              <div className="font-medium text-foreground">{t.title}</div>
              {t.description && <div className="text-foreground-muted">{t.description}</div>}
            </div>
          </div>
        );
      })}
    </div>
  );
}
