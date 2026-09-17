import { act } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { ResizableHandle, ResizablePanel, ResizablePanelGroup } from "@/components/ui/resizable";

/**
 * jsdom never lays anything out, so `getBoundingClientRect`/`offsetWidth`
 * etc. are always zero -- react-resizable-panels needs real measurements to
 * compute drag deltas as a ratio of container size. These prototype
 * overrides let each test describe the horizontal layout it wants (via
 * `data-testid`) and have the library's own runtime code compute against
 * it, instead of re-implementing that math in the test.
 */
interface Rect {
  x: number;
  width: number;
}

const rectsByTestId = new Map<string, Rect>();

function setRect(testId: string, rect: Rect) {
  rectsByTestId.set(testId, rect);
}

function lookupRect(el: Element): Rect {
  const testId = el.getAttribute("data-testid");
  return (testId && rectsByTestId.get(testId)) || { x: 0, width: 0 };
}

let originalGetBoundingClientRect: typeof Element.prototype.getBoundingClientRect;
let originalOffsetWidth: PropertyDescriptor | undefined;
let originalOffsetLeft: PropertyDescriptor | undefined;

beforeEach(() => {
  rectsByTestId.clear();
  originalGetBoundingClientRect = Element.prototype.getBoundingClientRect;
  originalOffsetWidth = Object.getOwnPropertyDescriptor(HTMLElement.prototype, "offsetWidth");
  originalOffsetLeft = Object.getOwnPropertyDescriptor(HTMLElement.prototype, "offsetLeft");

  Element.prototype.getBoundingClientRect = function (this: Element) {
    const { x, width } = lookupRect(this);
    return {
      x,
      y: 0,
      width,
      height: 300,
      top: 0,
      left: x,
      right: x + width,
      bottom: 300,
      toJSON: () => {},
    } as DOMRect;
  };
  Object.defineProperty(HTMLElement.prototype, "offsetWidth", {
    configurable: true,
    get(this: Element) {
      return lookupRect(this).width;
    },
  });
  Object.defineProperty(HTMLElement.prototype, "offsetLeft", {
    configurable: true,
    get(this: Element) {
      return lookupRect(this).x;
    },
  });
});

afterEach(() => {
  Element.prototype.getBoundingClientRect = originalGetBoundingClientRect;
  if (originalOffsetWidth) {
    Object.defineProperty(HTMLElement.prototype, "offsetWidth", originalOffsetWidth);
  }
  if (originalOffsetLeft) {
    Object.defineProperty(HTMLElement.prototype, "offsetLeft", originalOffsetLeft);
  }
});

/**
 * react-resizable-panels sets `data-testid`/`id` on every Panel/Separator/
 * Group root itself, to its own resolved `id` -- overwriting a `data-testid`
 * prop passed from outside (App.tsx's `sidebar-resize-handle` usage relies
 * on this same behavior). So `id` is both the lookup key for the rect mocks
 * below and what `screen.getByTestId` finds.
 */

/** A 400px-wide horizontal split at x=200, matching a 200/200 panel pair. */
function layoutAtBoundary(boundaryX: number, totalWidth = 400) {
  setRect("group", { x: 0, width: totalWidth });
  setRect("left", { x: 0, width: boundaryX });
  setRect("handle", { x: boundaryX, width: 1 });
  setRect("right", { x: boundaryX, width: totalWidth - boundaryX });
}

function renderSplit() {
  render(
    <ResizablePanelGroup orientation="horizontal" id="group">
      <ResizablePanel id="left" defaultSize={200} minSize={50}>
        left
      </ResizablePanel>
      <ResizableHandle id="handle" />
      <ResizablePanel id="right" minSize={50}>
        right
      </ResizablePanel>
    </ResizablePanelGroup>,
  );
  return document.getElementById("left") as HTMLElement;
}

/**
 * The library reports its layout as `flexGrow` on a 0-100 scale (matching
 * percent-of-container), set directly as inline style on each Panel's root
 * -- not through `onResize`, which is wired through a real `ResizeObserver`
 * noticing the panel's rendered box actually changed size. jsdom doesn't
 * lay anything out, so that observer (stubbed as a no-op in test/setup.ts,
 * since nothing before this file needed real resize behavior) never fires;
 * reading the committed layout directly is what actually exercises the
 * drag math instead.
 */
function flexGrowOf(panel: HTMLElement): number {
  return parseFloat(panel.style.flexGrow);
}

function dragHandle(from: number, to: number) {
  const handle = screen.getByTestId("handle");
  fireEvent.pointerDown(handle, {
    pointerId: 1,
    pointerType: "mouse",
    button: 0,
    clientX: from,
    clientY: 150,
  });
  // Dispatched on the handle (not `document`) so the event bubbles through
  // it -- jsdom doesn't implement `setPointerCapture`, which is what would
  // route these to the handle in a real browser regardless of where the
  // pointer physically is; bubbling from the handle is the jsdom-faithful
  // way to reach both react-resizable-panels' own document-level listeners
  // and this component's pointer handlers in one dispatch.
  fireEvent.pointerMove(handle, {
    pointerId: 1,
    pointerType: "mouse",
    buttons: 1,
    clientX: to,
    clientY: 150,
  });
  fireEvent.pointerUp(handle, {
    pointerId: 1,
    pointerType: "mouse",
    clientX: to,
    clientY: 150,
  });
}

describe("ResizableHandle drag interaction", () => {
  it("resizes the adjacent panel on a pointer-down/move/up drag", () => {
    layoutAtBoundary(200);
    const leftPanel = renderSplit();
    expect(flexGrowOf(leftPanel)).toBe(50);

    act(() => {
      dragHandle(200, 240);
    });

    // 240/400 -- the left panel grew from 50% to 60% of the 400px group.
    expect(flexGrowOf(leftPanel)).toBe(60);
  });

  it("still resizes correctly on a single fast/large pointer move, not just small increments", () => {
    layoutAtBoundary(200);
    const leftPanel = renderSplit();

    // One big jump simulating a fast drag, rather than many incremental
    // moves -- this is the case pointer capture exists to protect: without
    // it, a move this large risks the pointer leaving the handle's own
    // bounding box entirely and the drag being dropped.
    act(() => {
      dragHandle(200, 350);
    });

    // Dragging to 350/400 (87.5%) is clamped by the right panel's 50px
    // (12.5%) minSize, so the left panel should land exactly at the
    // complementary 87.5% -- not stuck near its starting 50%, and not
    // overshooting past the bound either.
    expect(flexGrowOf(leftPanel)).toBe(87.5);
  });

  it("does not resize when the handle is disabled", () => {
    layoutAtBoundary(200);
    render(
      <ResizablePanelGroup orientation="horizontal" id="group">
        <ResizablePanel id="left" defaultSize={200} minSize={50}>
          left
        </ResizablePanel>
        <ResizableHandle id="handle" disabled />
        <ResizablePanel id="right" minSize={50}>
          right
        </ResizablePanel>
      </ResizablePanelGroup>,
    );
    const leftPanel = document.getElementById("left") as HTMLElement;

    act(() => {
      dragHandle(200, 300);
    });

    expect(flexGrowOf(leftPanel)).toBe(50);
  });
});

describe("ResizableHandle hover-delay highlight", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("does not highlight immediately on hover", () => {
    layoutAtBoundary(200);
    renderSplit();
    const handle = screen.getByTestId("handle");

    act(() => {
      fireEvent.pointerEnter(handle, { pointerId: 1, pointerType: "mouse" });
    });

    expect(handle).not.toHaveAttribute("data-highlighted");
  });

  it("highlights after the hover delay elapses", () => {
    layoutAtBoundary(200);
    renderSplit();
    const handle = screen.getByTestId("handle");

    act(() => {
      fireEvent.pointerEnter(handle, { pointerId: 1, pointerType: "mouse" });
      vi.advanceTimersByTime(150);
    });

    expect(handle).toHaveAttribute("data-highlighted", "");
  });

  it("clears the highlight immediately on pointer leave, cancelling a pending hover timer", () => {
    layoutAtBoundary(200);
    renderSplit();
    const handle = screen.getByTestId("handle");

    act(() => {
      fireEvent.pointerEnter(handle, { pointerId: 1, pointerType: "mouse" });
    });
    act(() => {
      fireEvent.pointerLeave(handle, { pointerId: 1, pointerType: "mouse" });
      vi.advanceTimersByTime(150);
    });

    expect(handle).not.toHaveAttribute("data-highlighted");
  });

  it("highlights immediately on pointer down, without waiting for the hover delay", () => {
    layoutAtBoundary(200);
    renderSplit();
    const handle = screen.getByTestId("handle");

    act(() => {
      fireEvent.pointerDown(handle, {
        pointerId: 1,
        pointerType: "mouse",
        button: 0,
        clientX: 200,
        clientY: 150,
      });
    });

    expect(handle).toHaveAttribute("data-highlighted", "");

    act(() => {
      fireEvent.pointerUp(handle, { pointerId: 1, pointerType: "mouse", clientX: 200, clientY: 150 });
    });

    expect(handle).not.toHaveAttribute("data-highlighted");
  });
});
