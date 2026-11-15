/** The composer prompt textarea's marker (DT5 C): every prompt box (the task composer and the home landing composer) carries it. */
export const COMPOSER_INPUT_SELECTOR = "[data-composer-input]";

/** True when `el` is one of the composer prompt textareas; a plain input or textarea is not. */
export function isComposerInput(el: Element | null): boolean {
  return el !== null && el.matches(COMPOSER_INPUT_SELECTOR);
}
