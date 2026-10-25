import type { ComponentPropsWithoutRef } from "react";

import { cn } from "@/lib/utils";

/**
 * Single-line text that truncates in the middle: the head shrinks to an ellipsis while the last `tail` characters
 * always stay, so long branch names that share a prefix (`smind/task-...-<stamp>`) stay distinguishable. The full
 * text is the tooltip. Text no longer than the tail renders whole. A flex item that may shrink (`min-w-0`).
 */
export function MiddleText({ text, tail = 14, className, ...props }: { text: string; tail?: number } & Omit<ComponentPropsWithoutRef<"span">, "children">) {
  const split = text.length > tail * 1.5;
  return (
    <span title={text} className={cn("flex min-w-0 max-w-full", className)} {...props}>
      {split ? (
        <>
          <span data-part="head" className="min-w-0 truncate">
            {text.slice(0, -tail)}
          </span>
          <span data-part="tail" className="shrink-0">
            {text.slice(-tail)}
          </span>
        </>
      ) : (
        <span className="min-w-0 truncate">{text}</span>
      )}
    </span>
  );
}
