import { act } from "react";
import { render } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { applyChatHighlights, clearChatHighlights, findChatMatches } from "@/components/timeline/chat-find-dom";
import { installCssHighlightStub } from "@/test/css-highlight-stub";

/**
 * Regression coverage for a review finding on the original `<mark>`-based
 * implementation: `Range.surroundContents` (and `clearChatHighlights`'s
 * `parentNode.normalize()`) mutated the very Text nodes React's own fibers
 * reference for the live transcript. While Find was open and the last
 * assistant item was still streaming, React would commit an update or
 * removal against one of those mutated nodes -- either silently keeping
 * stale text (React writing into a node no longer connected, or no longer
 * the one actually on screen) or throwing outright:
 * `Failed to execute 'removeChild' on 'Node': The node to be removed is
 * not a child of this node.`
 *
 * Both failure modes reproduced against the pre-fix implementation with
 * the two cases below (confirmed while diagnosing this: the "update"
 * case silently kept the stale "hello" instead of "hello world", and the
 * "removal" case threw the exact NotFoundError above). The fix
 * (`chat-find-dom.ts`) replaced all DOM mutation with the CSS Custom
 * Highlight API, which paints over live Ranges without ever touching the
 * DOM tree -- these now pass because there is nothing left for React's
 * reconciliation to trip over.
 */

let stub: ReturnType<typeof installCssHighlightStub>;
afterEach(() => {
  stub?.restore();
});

/**
 * `<p>foo {chunk} bar</p>` gives the middle expression its own
 * independently-tracked HostText fiber (JSX doesn't concatenate adjacent
 * string/expression children into one node) -- the shape a real streamed
 * assistant chunk sitting among other text takes, and the one that
 * exposed the bug (a lone `<p>{text}</p>` self-heals via React DOM's own
 * "single Text child" fast path regardless of what Find did to it, which
 * is why that narrower shape didn't reproduce anything during triage).
 */
function MixedChildren({ chunk, show }: { chunk: string; show: boolean }) {
  return (
    <div data-chat-find-text="true">
      <p>foo {show ? chunk : null} bar</p>
    </div>
  );
}

function highlightAndClear(container: HTMLElement, query: string): void {
  const matches = findChatMatches(container, query);
  expect(matches.length).toBeGreaterThan(0);
  applyChatHighlights(matches, 0);
  clearChatHighlights();
}

describe("chat Find highlighting vs React reconciliation", () => {
  it("does not corrupt a later update to a highlighted-then-cleared text child", () => {
    stub = installCssHighlightStub();
    const { container, rerender } = render(<MixedChildren chunk="hello" show />);

    act(() => highlightAndClear(container, "hello"));
    act(() => rerender(<MixedChildren chunk="hello world" show />));

    expect(container.textContent).toBe("foo hello world bar");
  });

  it("does not crash when React removes a highlighted-then-cleared text child", () => {
    stub = installCssHighlightStub();
    const { container, rerender } = render(<MixedChildren chunk="hello" show />);

    act(() => highlightAndClear(container, "hello"));

    expect(() => {
      act(() => rerender(<MixedChildren chunk="hello" show={false} />));
    }).not.toThrow();
    expect(container.textContent).toBe("foo  bar");
  });

  it("also holds with no Highlight API available (the degraded, paint-nothing path)", () => {
    // No stub installed -- exercises the real "unavailable" branch.
    const { container, rerender } = render(<MixedChildren chunk="hello" show />);

    act(() => highlightAndClear(container, "hello"));

    expect(() => {
      act(() => rerender(<MixedChildren chunk="hello" show={false} />));
    }).not.toThrow();
    expect(container.textContent).toBe("foo  bar");
  });
});
