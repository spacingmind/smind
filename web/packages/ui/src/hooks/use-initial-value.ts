import { useRef } from "react";

/**
 * Freezes `value` the first time it's seen for a given `resetKey`, ignoring
 * later changes to `value` until `resetKey` itself changes.
 *
 * Exists for `react-resizable-panels`' `Panel.defaultSize` prop: the library
 * treats it as the *initial* size only, but re-registers the panel (and
 * fights an in-progress drag) any time the prop value changes -- so it must
 * never be fed a value that updates on every `onResize` frame. This hook
 * lets a component still read its "current" size from the same live state
 * (for CSS vars, persistence, etc.) while handing `defaultSize` something
 * that only moves when the thing it's initializing for (e.g. a task id)
 * actually changes.
 */
export function useInitialValue<T>(value: T, resetKey: unknown): T {
  const keyRef = useRef(resetKey);
  const valueRef = useRef(value);

  if (keyRef.current !== resetKey) {
    keyRef.current = resetKey;
    valueRef.current = value;
  }

  return valueRef.current;
}
