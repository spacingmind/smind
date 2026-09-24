import { afterEach, describe, expect, it } from "vitest";

import {
  applyChatHighlights,
  clearChatHighlights,
  findChatMatches,
  restyleChatHighlights,
} from "@/components/timeline/chat-find-dom";
import { installCssHighlightStub } from "@/test/css-highlight-stub";

function markedDiv(text: string): HTMLDivElement {
  const div = document.createElement("div");
  div.setAttribute("data-chat-find-text", "true");
  div.textContent = text;
  return div;
}

describe("findChatMatches", () => {
  it("finds every match across multiple containers, in document order", () => {
    const root = document.createElement("div");
    root.append(markedDiv("hello world"), markedDiv("say hello again"));

    const matches = findChatMatches(root, "hello");
    expect(matches).toHaveLength(2);
    expect(matches[0]!.node.parentElement).toBe(root.children[0]);
    expect(matches[1]!.node.parentElement).toBe(root.children[1]);
  });

  it("finds multiple matches inside the same text node", () => {
    const root = document.createElement("div");
    root.append(markedDiv("cat cat cat"));

    expect(findChatMatches(root, "cat")).toHaveLength(3);
  });

  it("ignores content outside any [data-chat-find-text] container", () => {
    const root = document.createElement("div");
    const plain = document.createElement("div");
    plain.textContent = "hello there";
    root.append(plain);

    expect(findChatMatches(root, "hello")).toHaveLength(0);
  });

  it("an empty query gives zero matches", () => {
    const root = document.createElement("div");
    root.append(markedDiv("hello world"));

    expect(findChatMatches(root, "")).toHaveLength(0);
    expect(findChatMatches(root, "   ")).toHaveLength(0);
  });

  it("returns nothing for a null root", () => {
    expect(findChatMatches(null, "hello")).toHaveLength(0);
  });
});

describe("applyChatHighlights / restyleChatHighlights / clearChatHighlights", () => {
  let stub: ReturnType<typeof installCssHighlightStub>;

  afterEach(() => {
    stub?.restore();
  });

  it("never mutates the DOM -- only registers Ranges to paint", () => {
    stub = installCssHighlightStub();
    const root = document.createElement("div");
    root.append(markedDiv("cat cat cat"));
    const textBefore = root.innerHTML;

    const matches = findChatMatches(root, "cat");
    applyChatHighlights(matches, 1);

    expect(root.innerHTML).toBe(textBefore);
    expect(root.querySelectorAll("mark")).toHaveLength(0);
  });

  it("registers every match under the all-matches highlight, and only the active one under the active highlight", () => {
    stub = installCssHighlightStub();
    const root = document.createElement("div");
    root.append(markedDiv("cat cat cat"));

    const matches = findChatMatches(root, "cat");
    const ranges = applyChatHighlights(matches, 1);

    expect(ranges).toHaveLength(3);
    ranges.forEach((range) => expect(range.toString()).toBe("cat"));

    const all = stub.registry.get("smind-chat-find");
    expect(all?.ranges).toEqual(ranges);

    const active = stub.registry.get("smind-chat-find-active");
    expect(active?.ranges).toEqual([ranges[1]]);
  });

  it("restyles (repaints the active highlight) without recomputing ranges", () => {
    stub = installCssHighlightStub();
    const root = document.createElement("div");
    root.append(markedDiv("cat cat cat"));
    const ranges = applyChatHighlights(findChatMatches(root, "cat"), 0);

    restyleChatHighlights(ranges, 2);

    expect(stub.registry.get("smind-chat-find-active")?.ranges).toEqual([ranges[2]]);
    // Still the exact same Range objects -- no re-walk happened.
    expect(stub.registry.get("smind-chat-find")?.ranges).toEqual(ranges);
  });

  it("clears both highlights", () => {
    stub = installCssHighlightStub();
    const root = document.createElement("div");
    root.append(markedDiv("cat cat cat"));
    applyChatHighlights(findChatMatches(root, "cat"), 0);
    expect(stub.registry.size).toBe(2);

    clearChatHighlights();

    expect(stub.registry.size).toBe(0);
  });

  it("degrades to a no-op paint when the Highlight API isn't available (no stub installed)", () => {
    const root = document.createElement("div");
    root.append(markedDiv("cat cat cat"));

    // No installCssHighlightStub() here -- this exercises the real jsdom
    // environment, which has neither `Highlight` nor `CSS.highlights`.
    expect(() => applyChatHighlights(findChatMatches(root, "cat"), 0)).not.toThrow();
    expect(() => clearChatHighlights()).not.toThrow();
    expect(root.querySelectorAll("mark")).toHaveLength(0);
  });
});
