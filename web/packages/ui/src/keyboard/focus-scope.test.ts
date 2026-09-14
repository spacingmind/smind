import { afterEach, describe, expect, it } from "vitest";

import {
  EDITOR_SURFACE_ATTR,
  resolveFocusScope,
  TERMINAL_SURFACE_ATTR,
} from "@/keyboard/focus-scope";

function mount(html: string): HTMLElement {
  const host = document.createElement("div");
  host.innerHTML = html;
  document.body.appendChild(host);
  return host;
}

afterEach(() => {
  document.body.innerHTML = "";
});

describe("resolveFocusScope", () => {
  it("reports `other` for a plain element", () => {
    const host = mount(`<div id="plain">x</div>`);
    expect(resolveFocusScope(host.querySelector("#plain"), false)).toBe("other");
  });

  it("reports `editable` for inputs, textareas and contenteditable", () => {
    const host = mount(`
      <input id="i" />
      <textarea id="t"></textarea>
      <div id="c" contenteditable="true"></div>
    `);
    for (const id of ["#i", "#t", "#c"]) {
      expect(resolveFocusScope(host.querySelector(id), false)).toBe("editable");
    }
  });

  it("reports `editable` inside CodeMirror and any marked editor surface", () => {
    const host = mount(`
      <div class="cm-editor"><span id="cm">code</span></div>
      <div ${EDITOR_SURFACE_ATTR}><span id="marked">x</span></div>
    `);
    expect(resolveFocusScope(host.querySelector("#cm"), false)).toBe("editable");
    expect(resolveFocusScope(host.querySelector("#marked"), false)).toBe("editable");
  });

  it("reports `terminal` inside xterm, winning over the textarea xterm itself mounts", () => {
    const host = mount(`<div class="xterm"><textarea id="helper"></textarea></div>`);
    expect(resolveFocusScope(host.querySelector("#helper"), false)).toBe("terminal");

    const marked = mount(`<div ${TERMINAL_SURFACE_ATTR}><span id="s">x</span></div>`);
    expect(resolveFocusScope(marked.querySelector("#s"), false)).toBe("terminal");
  });

  it("reports `modal` when a dialog owns the keyboard, but terminal still wins", () => {
    const host = mount(`<div id="plain">x</div><div class="xterm"><span id="t">x</span></div>`);
    expect(resolveFocusScope(host.querySelector("#plain"), true)).toBe("modal");
    expect(resolveFocusScope(host.querySelector("#t"), true)).toBe("terminal");
  });

  it("falls back to document.activeElement when the event targets the document", () => {
    // Every fireEvent.keyDown(document) in this repo's tests -- and the
    // provider's own window-level listener -- lands here.
    const host = mount(`<input id="i" />`);
    const input = host.querySelector<HTMLInputElement>("#i")!;
    expect(resolveFocusScope(document, false)).toBe("other");
    input.focus();
    expect(resolveFocusScope(document, false)).toBe("editable");
  });

  it("does not treat an unfocused document.body as a candidate", () => {
    expect(resolveFocusScope(null, false)).toBe("other");
    expect(resolveFocusScope(document.body, false)).toBe("other");
  });
});
