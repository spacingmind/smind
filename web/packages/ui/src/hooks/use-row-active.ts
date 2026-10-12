import { useCallback, useMemo, useState, type FocusEvent } from "react";

/**
 * Whether a sidebar row's hover-only controls (the ⋯ trigger) should be
 * mounted right now: the pointer is on THIS row, keyboard focus is within it,
 * or one of its menus is open.
 *
 * Why state instead of CSS `group-hover`: a row's `li` also contains its
 * descendants' rows (a parent task's nested children list, a space's tasks),
 * so a CSS group on that `li` reveals every nested row's controls when any of
 * them is hovered. The handlers below go on a wrapper around the row's own
 * line only, and conditional mounting also keeps the controls out of the tab
 * order and the a11y tree while they are not shown (ZCode's TaskListItem /
 * WorkspaceSidebarItem do the same) and is deterministic under jsdom.
 *
 * Focus only counts when it is keyboard focus (`:focus-visible`): clicking a
 * row to select it focuses it, and the controls must not stay pinned open
 * after the pointer leaves.
 */
export function useRowActive() {
  const [hovered, setHovered] = useState(false);
  const [focused, setFocused] = useState(false);
  const [menuOpen, setMenuOpen] = useState(false);

  const onFocus = useCallback((e: FocusEvent<HTMLElement>) => {
    setFocused(isKeyboardFocus(e.target));
  }, []);
  const onBlur = useCallback((e: FocusEvent<HTMLElement>) => {
    if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setFocused(false);
  }, []);
  const onMouseEnter = useCallback(() => setHovered(true), []);
  const onMouseLeave = useCallback(() => setHovered(false), []);

  const rowProps = useMemo(
    () => ({ onMouseEnter, onMouseLeave, onFocus, onBlur }),
    [onMouseEnter, onMouseLeave, onFocus, onBlur],
  );

  return { active: hovered || focused || menuOpen, rowProps, setMenuOpen };
}

function isKeyboardFocus(el: EventTarget): boolean {
  try {
    return (el as Element).matches(":focus-visible");
  } catch {
    // An engine without :focus-visible: treat any focus as keyboard focus.
    return true;
  }
}
