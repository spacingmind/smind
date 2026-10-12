import { act, fireEvent, render, screen } from "@testing-library/react";
import { useRef, useState } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { SIDEBAR_SNAP_WIDTH, useSidebarResize } from "@/hooks/use-sidebar-resize";
import { SIDEBAR_MAX_WIDTH, SIDEBAR_MIN_WIDTH } from "@/hooks/use-sidebar-width";

let renders = 0;
const commits: number[] = [];
const collapses: number[] = [];

function Probe({ initial = 256 }: { initial?: number }) {
  const [width, setWidth] = useState(initial);
  const panelRef = useRef<HTMLDivElement>(null);
  renders += 1;
  const handlers = useSidebarResize({
    panelRef,
    width,
    onCommit: (next) => {
      commits.push(next);
      setWidth(next);
    },
    onCollapse: () => collapses.push(1),
  });
  return (
    <div data-slot="sidebar-wrapper" data-testid="wrapper" style={{ "--sidebar-width": `${width}px` } as React.CSSProperties}>
      <div>
        <div ref={panelRef} data-testid="panel" />
        <div role="separator" tabIndex={0} data-testid="handle" aria-valuenow={width} {...handlers} />
      </div>
    </div>
  );
}

let rafCallbacks: FrameRequestCallback[] = [];
function flushFrames() {
  const pending = rafCallbacks;
  rafCallbacks = [];
  act(() => pending.forEach((cb) => cb(0)));
}

const cssVar = () => screen.getByTestId("wrapper").style.getPropertyValue("--sidebar-width");

beforeEach(() => {
  renders = 0;
  commits.length = 0;
  collapses.length = 0;
  rafCallbacks = [];
  vi.stubGlobal("requestAnimationFrame", (cb: FrameRequestCallback) => rafCallbacks.push(cb));
  vi.stubGlobal("cancelAnimationFrame", () => {
    rafCallbacks = [];
  });
});

afterEach(() => {
  vi.unstubAllGlobals();
  document.body.className = "";
});

describe("useSidebarResize", () => {
  it("dragging sets data-resizing and writes --sidebar-width without a React commit", () => {
    render(<Probe />);
    const handle = screen.getByTestId("handle");
    const rendersBefore = renders;

    fireEvent.pointerDown(handle, { button: 0, clientX: 256, pointerId: 1 });
    expect(screen.getByTestId("panel")).toHaveAttribute("data-resizing", "true");
    expect(document.body).toHaveClass("cursor-col-resize", "select-none", "sidebar-resizing");

    fireEvent.pointerMove(handle, { clientX: 300, pointerId: 1 });
    fireEvent.pointerMove(handle, { clientX: 316, pointerId: 1 });
    // Coalesced: one frame is scheduled, nothing written yet.
    expect(rafCallbacks).toHaveLength(1);
    flushFrames();

    expect(cssVar()).toBe("316px");
    expect(renders).toBe(rendersBefore);
    expect(commits).toEqual([]);
    expect(window.localStorage.getItem("smind:sidebar-width")).toBeNull();
  });

  it("release commits the clamped width", () => {
    render(<Probe />);
    const handle = screen.getByTestId("handle");

    fireEvent.pointerDown(handle, { button: 0, clientX: 256, pointerId: 1 });
    fireEvent.pointerUp(handle, { clientX: 256 + 1000, pointerId: 1 });
    expect(commits).toEqual([SIDEBAR_MAX_WIDTH]);
    expect(cssVar()).toBe(`${SIDEBAR_MAX_WIDTH}px`);
    expect(screen.getByTestId("panel")).not.toHaveAttribute("data-resizing");
    expect(document.body).not.toHaveClass("cursor-col-resize");

    fireEvent.pointerDown(handle, { button: 0, clientX: 512, pointerId: 1 });
    fireEvent.pointerUp(handle, { clientX: 512 - 350, pointerId: 1 });
    // raw 162 is above the snap width, below min: settles at min.
    expect(commits.at(-1)).toBe(SIDEBAR_MIN_WIDTH);
    expect(collapses).toEqual([]);
  });

  it("release below the snap width collapses and restores the committed width", () => {
    render(<Probe />);
    const handle = screen.getByTestId("handle");

    fireEvent.pointerDown(handle, { button: 0, clientX: 256, pointerId: 1 });
    fireEvent.pointerMove(handle, { clientX: 100, pointerId: 1 });
    flushFrames();
    expect(cssVar()).toBe(`${SIDEBAR_MIN_WIDTH}px`);
    fireEvent.pointerUp(handle, { clientX: 256 - (256 - (SIDEBAR_SNAP_WIDTH - 1)), pointerId: 1 });

    expect(collapses).toHaveLength(1);
    expect(commits).toEqual([]);
    expect(cssVar()).toBe("256px");
    expect(document.body).not.toHaveClass("select-none");
  });

  it("keyboard resize: arrows, Home, End", () => {
    render(<Probe />);
    const handle = screen.getByTestId("handle");

    fireEvent.keyDown(handle, { key: "ArrowRight" });
    fireEvent.keyDown(handle, { key: "ArrowRight" });
    fireEvent.keyDown(handle, { key: "ArrowLeft" });
    fireEvent.keyDown(handle, { key: "Home" });
    fireEvent.keyDown(handle, { key: "End" });
    fireEvent.keyDown(handle, { key: "a" });
    expect(commits).toEqual([272, 288, 272, SIDEBAR_MIN_WIDTH, SIDEBAR_MAX_WIDTH]);
    expect(handle).toHaveAttribute("aria-valuenow", String(SIDEBAR_MAX_WIDTH));
  });

  it("pointercancel restores state and body class", () => {
    render(<Probe />);
    const handle = screen.getByTestId("handle");

    fireEvent.pointerDown(handle, { button: 0, clientX: 256, pointerId: 1 });
    fireEvent.pointerMove(handle, { clientX: 400, pointerId: 1 });
    flushFrames();
    expect(cssVar()).toBe("400px");

    fireEvent.pointerCancel(handle, { pointerId: 1 });
    expect(cssVar()).toBe("256px");
    expect(screen.getByTestId("panel")).not.toHaveAttribute("data-resizing");
    expect(document.body).not.toHaveClass("cursor-col-resize");
    expect(commits).toEqual([]);
    expect(collapses).toEqual([]);
  });

  it("unmounting mid-drag removes the body classes", () => {
    const { unmount } = render(<Probe />);
    fireEvent.pointerDown(screen.getByTestId("handle"), { button: 0, clientX: 256, pointerId: 1 });
    expect(document.body).toHaveClass("select-none");
    unmount();
    expect(document.body).not.toHaveClass("select-none");
    expect(document.body).not.toHaveClass("sidebar-resizing");
  });
});
