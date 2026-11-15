import { describe, expect, it } from "vitest";

import { COMPOSER_INPUT_SELECTOR, isComposerInput } from "@/lib/composer-focus";

describe("isComposerInput", () => {
  it("a textarea carrying the composer marker is a composer input", () => {
    const el = document.createElement("textarea");
    el.setAttribute("data-composer-input", "");
    document.body.appendChild(el);
    expect(isComposerInput(el)).toBe(true);
    expect(document.querySelector(COMPOSER_INPUT_SELECTOR)).toBe(el);
  });

  it("a plain textarea, an input and null are not composer inputs", () => {
    const textarea = document.createElement("textarea");
    const input = document.createElement("input");
    document.body.append(textarea, input);
    expect(isComposerInput(textarea)).toBe(false);
    expect(isComposerInput(input)).toBe(false);
    expect(isComposerInput(null)).toBe(false);
  });
});
