import { EditorState } from "@codemirror/state";
import { EditorView } from "@codemirror/view";
import { afterEach, describe, expect, it } from "vitest";

import { FileFindModel } from "@/components/file-editor-find";

/**
 * FileFindModel drives real `@codemirror/search` state -- these tests
 * exercise it against a real (headless) `EditorView`, the same way
 * `code-mirror-editor.tsx`'s own tests reach into a live view rather than
 * mocking CodeMirror, per that file's doc comment on why (jsdom's
 * contentEditable support isn't reliable enough to trust a synthesized
 * keystroke; driving the public `EditorView`/model API is what a real
 * keystroke ultimately reaches anyway).
 */

function createView(doc: string, extra: import("@codemirror/state").Extension[] = []): { view: EditorView; model: FileFindModel } {
  const model = new FileFindModel();
  const view = new EditorView({
    state: EditorState.create({ doc, extensions: [model.extension, ...extra] }),
  });
  return { view, model };
}

let current: EditorView | null = null;
afterEach(() => {
  current?.destroy();
  current = null;
});

describe("FileFindModel", () => {
  it("starts closed, and opening it publishes an open snapshot", () => {
    const { view, model } = createView("hello world");
    current = view;

    expect(model.getSnapshot().open).toBe(false);
    model.open(view);
    expect(model.getSnapshot().open).toBe(true);
  });

  it("counts every match and reports which one is current", () => {
    const { view, model } = createView("hello world hello");
    current = view;

    model.open(view);
    model.setSearch("hello");

    expect(model.getSnapshot().total).toBe(2);
    // setSearch already moves to the first match.
    expect(model.getSnapshot().current).toBe(1);
  });

  it("next/previous move through matches with wraparound", () => {
    const { view, model } = createView("hello world hello");
    current = view;

    model.open(view);
    model.setSearch("hello");
    expect(model.getSnapshot().current).toBe(1);

    model.next();
    expect(model.getSnapshot().current).toBe(2);

    model.next();
    expect(model.getSnapshot().current).toBe(1);

    model.previous();
    expect(model.getSnapshot().current).toBe(2);
  });

  it("an empty query has zero matches", () => {
    const { view, model } = createView("hello world");
    current = view;

    model.open(view);
    model.setSearch("");

    expect(model.getSnapshot().total).toBe(0);
  });

  it("replace swaps the current match's text", () => {
    const { view, model } = createView("hello world hello");
    current = view;

    model.open(view);
    model.setSearch("hello");
    model.setReplacement("hi");
    model.replace();

    expect(view.state.doc.toString()).toBe("hi world hello");
  });

  it("replaceAll swaps every match's text", () => {
    const { view, model } = createView("hello world hello");
    current = view;

    model.open(view);
    model.setSearch("hello");
    model.setReplacement("hi");
    model.replaceAll();

    expect(view.state.doc.toString()).toBe("hi world hi");
  });

  it("reports readOnly from the view's own state, hiding replace for a read-only file", () => {
    const { view, model } = createView("hello world", [EditorState.readOnly.of(true)]);
    current = view;

    model.open(view);
    model.setSearch("hello");

    expect(model.getSnapshot().readOnly).toBe(true);
  });

  it("close closes the panel", () => {
    const { view, model } = createView("hello world");
    current = view;

    model.open(view);
    expect(model.getSnapshot().open).toBe(true);

    model.close();
    expect(model.getSnapshot().open).toBe(false);
  });
});
