import { act } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { createActivePromptPublisher, type OutlinePrompt } from "./chat-outline";
import { ChatOutlineRail } from "./chat-outline-rail";

const PROMPTS: OutlinePrompt[] = [
  { id: "r1", preview: "first prompt" },
  { id: "r2", preview: "second prompt" },
  { id: "r3", preview: "third prompt" },
  { id: "r4", preview: "fourth prompt" },
];

function setup(overrides: Partial<Parameters<typeof ChatOutlineRail>[0]> = {}) {
  const activePrompt = createActivePromptPublisher();
  const onJump = vi.fn();
  const utils = render(
    <ChatOutlineRail prompts={PROMPTS} activePrompt={activePrompt} onJump={onJump} {...overrides} />,
  );
  return { activePrompt, onJump, ...utils };
}

function stubSlots(rail: HTMLElement, top: number, slotHeight: number) {
  Array.from(rail.children).forEach((slot, i) => {
    const y = top + i * slotHeight;
    slot.getBoundingClientRect = () =>
      ({ top: y, height: slotHeight, bottom: y + slotHeight, left: 0, right: 36, width: 36, x: 0, y }) as DOMRect;
  });
}

describe("ChatOutlineRail", () => {
  it('renders one tab per prompt with an "N of M: preview" label', () => {
    setup();
    const tabs = screen.getAllByRole("tab");
    expect(tabs).toHaveLength(4);
    expect(tabs[0]).toHaveAttribute("aria-label", "1 of 4: first prompt");
    expect(tabs[3]).toHaveAttribute("aria-label", "4 of 4: fourth prompt");
  });

  it("renders nothing for fewer than two prompts", () => {
    setup({ prompts: [PROMPTS[0]] });
    expect(screen.queryByTestId("chat-outline-rail")).toBeNull();
  });

  it("marks the active prompt aria-selected and only it is tabbable", () => {
    const { activePrompt } = setup();
    // No active run yet: the first tick is the tab stop.
    expect(screen.getByTestId("chat-outline-tick-r1")).toHaveAttribute("tabindex", "0");
    act(() => activePrompt.publish("r3"));
    const tabs = screen.getAllByRole("tab");
    expect(tabs.map((t) => t.getAttribute("aria-selected"))).toEqual([
      "false",
      "false",
      "true",
      "false",
    ]);
    expect(tabs.map((t) => t.getAttribute("tabindex"))).toEqual(["-1", "-1", "0", "-1"]);
  });

  it("ArrowDown/ArrowUp/Home/End move focus between ticks", () => {
    setup();
    const tabs = screen.getAllByRole("tab");
    act(() => tabs[0].focus());
    fireEvent.keyDown(tabs[0], { key: "ArrowDown" });
    expect(tabs[1]).toHaveFocus();
    fireEvent.keyDown(tabs[1], { key: "ArrowDown" });
    expect(tabs[2]).toHaveFocus();
    fireEvent.keyDown(tabs[2], { key: "ArrowUp" });
    expect(tabs[1]).toHaveFocus();
    fireEvent.keyDown(tabs[1], { key: "End" });
    expect(tabs[3]).toHaveFocus();
    fireEvent.keyDown(tabs[3], { key: "Home" });
    expect(tabs[0]).toHaveFocus();
  });

  it("Enter on a focused tick calls onJump with its run id", () => {
    const { onJump } = setup();
    const tab = screen.getByTestId("chat-outline-tick-r2");
    act(() => tab.focus());
    // Native buttons turn Enter/Space into a click with detail 0.
    fireEvent.click(tab, { detail: 0 });
    expect(onJump).toHaveBeenCalledWith("r2");
  });

  it("focusing a tick shows the preview with the prompt's first line", () => {
    setup();
    expect(screen.queryByTestId("chat-outline-preview")).toBeNull();
    act(() => screen.getByTestId("chat-outline-tick-r3").focus());
    expect(screen.getByTestId("chat-outline-preview")).toHaveTextContent("third prompt");
    act(() => screen.getByTestId("chat-outline-tick-r3").blur());
    expect(screen.queryByTestId("chat-outline-preview")).toBeNull();
  });

  it("pointer drag across the rail jumps to each run it crosses", () => {
    const { onJump } = setup();
    const rail = screen.getByTestId("chat-outline-rail");
    stubSlots(rail, 100, 100);
    // 4 slots of 100px each.
    fireEvent.pointerDown(rail, { clientY: 120, button: 0 });
    fireEvent.pointerMove(rail, { clientY: 130, buttons: 1 });
    fireEvent.pointerMove(rail, { clientY: 230, buttons: 1 });
    fireEvent.pointerMove(rail, { clientY: 480, buttons: 1 });
    fireEvent.pointerUp(rail, { clientY: 480 });
    fireEvent.pointerMove(rail, { clientY: 130 });
    expect(onJump.mock.calls.map((c) => c[0])).toEqual(["r1", "r2", "r4"]);
  });

  it("pointer jumps map over the slots' extent, not the rail's height", () => {
    const { onJump } = setup();
    const rail = screen.getByTestId("chat-outline-rail");
    // The rail is 400px tall but its 4 centred slots only span 150..250 (25px each).
    rail.getBoundingClientRect = () =>
      ({ top: 100, height: 400, left: 0, width: 36, right: 36, bottom: 500, x: 0, y: 100 }) as DOMRect;
    stubSlots(rail, 150, 25);
    fireEvent.pointerDown(rail, { clientY: 212, button: 0 });
    expect(onJump.mock.calls.map((c) => c[0])).toEqual(["r3"]);
  });

  it("a mouse press does not take focus, so the preview does not stay pinned to the tick", () => {
    setup();
    const rail = screen.getByTestId("chat-outline-rail");
    stubSlots(rail, 100, 100);
    const notPrevented = fireEvent.pointerDown(rail, { clientY: 120, button: 0 });
    expect(notPrevented).toBe(false);
  });

  it("releasing the button outside the rail ends the drag", () => {
    const { onJump } = setup();
    const rail = screen.getByTestId("chat-outline-rail");
    stubSlots(rail, 100, 100);
    fireEvent.pointerDown(rail, { clientY: 120, button: 0 });
    fireEvent.pointerMove(rail, { clientY: 230, buttons: 0 });
    fireEvent.pointerMove(rail, { clientY: 330, buttons: 0 });
    expect(onJump.mock.calls.map((c) => c[0])).toEqual(["r1"]);
  });

  it("does not re-render ticks when only the active prompt of a sibling changes", () => {
    const onTickRender = vi.fn();
    const { activePrompt } = setup({ onTickRender });
    act(() => activePrompt.publish("r1"));
    onTickRender.mockClear();
    act(() => activePrompt.publish("r2"));
    // Only the tick that lost and the tick that gained the active state re-render.
    expect(onTickRender.mock.calls.map((c) => c[0]).sort()).toEqual(["r1", "r2"]);
  });
});
