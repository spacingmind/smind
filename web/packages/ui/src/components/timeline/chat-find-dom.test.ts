import { describe, expect, it } from "vitest";

import {
  applyChatHighlights,
  clearChatHighlights,
  findChatMatches,
  restyleChatHighlights,
} from "@/components/timeline/chat-find-dom";

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
  it("wraps every match in a <mark>, styling only the active one", () => {
    const root = document.createElement("div");
    root.append(markedDiv("cat cat cat"));

    const matches = findChatMatches(root, "cat");
    const marks = applyChatHighlights(matches, 1);

    expect(marks).toHaveLength(3);
    marks.forEach((mark) => expect(mark.tagName).toBe("MARK"));
    expect(marks.map((m) => m.textContent)).toEqual(["cat", "cat", "cat"]);
    expect(marks[1]!.className).toContain("bg-status-warning");
    expect(marks[1]!.className).not.toBe(marks[0]!.className);
    // The container's overall text is unchanged by wrapping.
    expect(root.textContent).toBe("cat cat cat");
  });

  it("restyles without re-wrapping when the active index changes", () => {
    const root = document.createElement("div");
    root.append(markedDiv("cat cat cat"));
    const marks = applyChatHighlights(findChatMatches(root, "cat"), 0);
    const activeClass = marks[0]!.className;

    restyleChatHighlights(marks, 2);

    expect(marks[0]!.className).not.toBe(activeClass);
    expect(marks[2]!.className).toBe(activeClass);
    // Still the same three DOM nodes -- no re-walk happened.
    expect(root.querySelectorAll("mark")).toHaveLength(3);
  });

  it("clears every mark, restoring the container's original text", () => {
    const root = document.createElement("div");
    root.append(markedDiv("cat cat cat"));
    applyChatHighlights(findChatMatches(root, "cat"), 0);

    clearChatHighlights(root);

    expect(root.querySelectorAll("mark")).toHaveLength(0);
    expect(root.textContent).toBe("cat cat cat");
  });

  it("clearing is a no-op for a null root", () => {
    expect(() => clearChatHighlights(null)).not.toThrow();
  });
});
