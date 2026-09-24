import { useCallback, useState, type FocusEvent } from "react";

/**
 * Tracks whether keyboard focus is anywhere inside the element these
 * handlers are spread onto -- what gates each pane's `pane.find` handler
 * (`keyboard/keyboard-provider.tsx`'s `useActionHandler`) so `Mod+F` opens
 * *this* pane's Find bar only when this pane is the one actually focused,
 * per `docs/plans/active/web-find.md`'s "applies to whichever pane has
 * focus" decision.
 *
 * No ref is needed: `onBlur`'s `event.currentTarget` is already the
 * element the handlers are attached to, and React's `onFocus`/`onBlur`
 * bubble (unlike the native `focus`/`blur` events), so a real DOM node
 * xterm or CodeMirror creates *inside* that element still fires them --
 * this is plain focus-within tracking, not a focus-scope classifier like
 * `keyboard/focus-scope.ts` (that one decides which *binding* is allowed to
 * fire at all; this one decides which mounted pane instance currently owns
 * the ones that are).
 *
 * Since at most one element can hold `document.activeElement`, at most one
 * mounted pane's `focused` is ever true -- so two file tabs open side by
 * side in a split layout never race over which one's `pane.find` handler
 * wins; whichever one the user actually clicked into does.
 */
export function usePaneFocusWithin(): {
  focused: boolean;
  onFocus: (event: FocusEvent<HTMLElement>) => void;
  onBlur: (event: FocusEvent<HTMLElement>) => void;
} {
  const [focused, setFocused] = useState(false);

  const onFocus = useCallback(() => setFocused(true), []);
  const onBlur = useCallback((event: FocusEvent<HTMLElement>) => {
    if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setFocused(false);
  }, []);

  return { focused, onFocus, onBlur };
}
