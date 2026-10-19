import { useRef } from "react";

/**
 * The latest non-null value passed in. A dialog whose target is cleared on close (`target = null`) is still
 * mounted while Radix plays its exit animation: reading the target through this keeps its name on screen until
 * the dialog is actually gone, instead of flashing "Delete ?" mid-animation.
 */
export function useLastDefined<T>(value: T | null | undefined): T | null {
  const last = useRef<T | null>(null);
  if (value !== null && value !== undefined) last.current = value;
  return last.current;
}
