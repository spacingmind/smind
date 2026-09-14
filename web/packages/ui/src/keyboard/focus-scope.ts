import type { FocusScope } from "@/keyboard/actions";

/**
 * Marks a subtree as an editor surface for shortcut routing. Put it on any
 * wrapper whose descendants swallow typing but aren't `<input>`/`<textarea>`
 * -- CodeMirror's content div is `contenteditable` (already covered), but
 * a custom key-capturing widget is not.
 */
export const EDITOR_SURFACE_ATTR = "data-keyboard-editor";

/** Marks a subtree as a terminal surface. `terminal-pane.tsx`'s xterm mount carries it. */
export const TERMINAL_SURFACE_ATTR = "data-keyboard-terminal";

/**
 * Candidate elements to inspect for a scope: the event target, its parent
 * (a text node target has no `closest`), and the document's active element.
 *
 * `document.activeElement` is in the list because a keydown dispatched
 * programmatically -- every `fireEvent.keyDown(document)` in this repo's
 * tests, and the window-level listener the dispatcher uses in production --
 * targets the document rather than the focused control, so target alone
 * would report "other" while the user is demonstrably typing in a field.
 */
function candidates(target: EventTarget | null): Element[] {
  const found: Element[] = [];
  const push = (el: Element | null | undefined) => {
    if (el && !found.includes(el)) found.push(el);
  };

  if (target instanceof Element) push(target);
  else if (target instanceof Node) push(target.parentElement);

  if (typeof document !== "undefined" && document.activeElement instanceof Element) {
    // `document.body` is what activeElement reports when nothing is focused;
    // treating that as a real candidate would make every `closest()` below
    // scan the whole tree for a marker it can't meaningfully be inside of.
    if (document.activeElement !== document.body) push(document.activeElement);
  }

  return found;
}

function isEditableElement(el: Element): boolean {
  if (el instanceof HTMLElement && el.isContentEditable) return true;
  // jsdom doesn't implement `isContentEditable` (it has no editing host
  // concept at all), so the attribute is checked too. In a real browser
  // the two agree; in tests only this branch fires.
  const attr = el.getAttribute("contenteditable");
  if (attr !== null && attr !== "false") return true;
  const tag = el.tagName.toLowerCase();
  return tag === "input" || tag === "textarea" || tag === "select";
}

/**
 * Where focus is for a given key event.
 *
 * `modalOpen` is passed in rather than sniffed from the DOM: the shell
 * knows which of its own dialogs are open, and a Radix portal's content
 * lives outside the React tree the event came from, so a DOM probe would
 * be both slower and less reliable than the state that opened it.
 *
 * Precedence is terminal → editable → modal → other. Terminal wins over
 * editable because xterm's helper textarea *is* a `<textarea>`, and the
 * terminal rules are the stricter of the two.
 */
export function resolveFocusScope(target: EventTarget | null, modalOpen: boolean): FocusScope {
  const elements = candidates(target);

  for (const el of elements) {
    if (el.closest(`[${TERMINAL_SURFACE_ATTR}]`) || el.closest(".xterm")) return "terminal";
  }
  if (modalOpen) return "modal";
  for (const el of elements) {
    if (isEditableElement(el) || el.closest(`[${EDITOR_SURFACE_ATTR}]`) || el.closest(".cm-editor")) {
      return "editable";
    }
  }
  return "other";
}
