import { useLayoutEffect, useRef, type KeyboardEvent, type Ref } from "react";

import { cn } from "@/lib/utils";

/**
 * The composer textarea's height cap in pixels. Past this it stops growing
 * and scrolls instead, so a pasted essay can't push the timeline off
 * screen (ui-redesign-parity Item 10: "capped in height then scrolling").
 */
export const MAX_COMPOSER_HEIGHT = 320;

/**
 * Sizes `el` to its content up to MAX_COMPOSER_HEIGHT, then lets it
 * scroll. Resetting to "auto" first is what makes it *shrink* again when
 * text is deleted -- without it scrollHeight only ever reports the larger
 * of content and current height, so the box would ratchet upward.
 *
 * Exported so the sizing rule can be unit-tested directly: jsdom has no
 * layout, so a component test can only drive this by stubbing
 * scrollHeight, and asserting the function is clearer than asserting the
 * DOM side effect through three layers of React.
 */
export function autoGrow(el: HTMLTextAreaElement): void {
  el.style.height = "auto";
  const overflowing = el.scrollHeight > MAX_COMPOSER_HEIGHT;
  el.style.height = `${Math.min(el.scrollHeight, MAX_COMPOSER_HEIGHT)}px`;
  el.style.overflowY = overflowing ? "auto" : "hidden";
}

/**
 * True when this keydown should submit: Enter, no Shift, and not part of
 * an IME composition. The composition check is two-pronged deliberately --
 * `isComposing` is the modern signal, but Safari and some Android IMEs
 * only ever report the legacy `keyCode === 229` sentinel, and committing
 * a candidate with Enter must never send the message half-composed.
 */
export function isSubmitKey(e: KeyboardEvent<HTMLTextAreaElement>): boolean {
  if (e.key !== "Enter" || e.shiftKey) return false;
  const native = e.nativeEvent as unknown as { isComposing?: boolean; keyCode?: number };
  if (native.isComposing || native.keyCode === 229) return false;
  return true;
}

/**
 * An autogrowing, IME-safe prompt textarea: Enter submits, Shift+Enter
 * inserts a newline, and the box grows with its content to
 * MAX_COMPOSER_HEIGHT and then scrolls.
 */
export function PromptTextarea({
  value,
  onChange,
  onSubmit,
  onKeyDown,
  placeholder,
  disabled,
  label,
  ref,
  className,
}: {
  value: string;
  onChange: (next: string) => void;
  onSubmit: () => void;
  /** Extra key handling (Escape-to-stop) applied after the submit check. */
  onKeyDown?: (e: KeyboardEvent<HTMLTextAreaElement>) => void;
  placeholder: string;
  disabled?: boolean;
  label: string;
  ref?: Ref<HTMLTextAreaElement>;
  className?: string;
}) {
  const innerRef = useRef<HTMLTextAreaElement | null>(null);

  // Layout effect, not effect: resize before the browser paints, so a
  // value change never shows one frame at the old height.
  useLayoutEffect(() => {
    if (innerRef.current) autoGrow(innerRef.current);
  }, [value]);

  return (
    <textarea
      ref={(node) => {
        innerRef.current = node;
        if (typeof ref === "function") ref(node);
        else if (ref) ref.current = node;
      }}
      aria-label={label}
      rows={1}
      value={value}
      placeholder={placeholder}
      disabled={disabled}
      style={{ maxHeight: MAX_COMPOSER_HEIGHT }}
      onChange={(e) => onChange(e.target.value)}
      onKeyDown={(e) => {
        if (isSubmitKey(e)) {
          e.preventDefault();
          onSubmit();
          return;
        }
        onKeyDown?.(e);
      }}
      className={cn(
        "w-full resize-none rounded-lg border border-input bg-transparent px-2.5 py-1.5 text-sm outline-none transition-colors placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 disabled:cursor-not-allowed disabled:opacity-50",
        className,
      )}
    />
  );
}
