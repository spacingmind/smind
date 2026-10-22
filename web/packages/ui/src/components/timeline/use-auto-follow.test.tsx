import { act, render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { useAutoFollow } from "./use-auto-follow";

let observers: { callback: () => void; targets: Element[]; disconnected: boolean }[] = [];
const RealResizeObserver = window.ResizeObserver;

beforeEach(() => {
  observers = [];
  window.ResizeObserver = class {
    private entry: (typeof observers)[number];
    constructor(callback: () => void) {
      this.entry = { callback, targets: [], disconnected: false };
      observers.push(this.entry);
    }
    observe(target: Element): void {
      this.entry.targets.push(target);
    }
    unobserve(): void {}
    disconnect(): void {
      this.entry.disconnected = true;
    }
  } as unknown as typeof ResizeObserver;
});

afterEach(() => {
  window.ResizeObserver = RealResizeObserver;
});

function Harness({ revision, probe }: { revision: number; probe: (f: ReturnType<typeof useAutoFollow<HTMLDivElement>>) => void }) {
  const follow = useAutoFollow<HTMLDivElement>(revision);
  probe(follow);
  return (
    <div ref={follow.ref} onScroll={follow.onScroll} data-testid="scroller">
      <div data-testid="column" />
    </div>
  );
}

/** jsdom has no layout: fake the three numbers distanceFromBottom reads. */
function setLayout(el: HTMLElement, { scrollHeight, clientHeight }: { scrollHeight: number; clientHeight: number }) {
  Object.defineProperty(el, "scrollHeight", { configurable: true, value: scrollHeight });
  Object.defineProperty(el, "clientHeight", { configurable: true, value: clientHeight });
}

describe("useAutoFollow layout growth", () => {
  it("re-pins to the tail when the content grows without a new item", () => {
    let follow!: ReturnType<typeof useAutoFollow<HTMLDivElement>>;
    const { getByTestId } = render(<Harness revision={1} probe={(f) => (follow = f)} />);
    const scroller = getByTestId("scroller");
    setLayout(scroller, { scrollHeight: 1000, clientHeight: 600 });
    scroller.scrollTop = 400;

    // An image decodes and the column grows by 36px; same revision.
    setLayout(scroller, { scrollHeight: 1036, clientHeight: 600 });
    act(() => observers.forEach((o) => o.callback()));

    expect(scroller.scrollTop).toBe(1036);
    expect(follow.following).toBe(true);
  });

  it("observes the scroller and its content column, and disconnects on unmount", () => {
    const { getByTestId, unmount } = render(<Harness revision={1} probe={() => {}} />);
    expect(observers).toHaveLength(1);
    expect(observers[0]!.targets).toEqual([getByTestId("scroller"), getByTestId("column")]);
    unmount();
    expect(observers[0]!.disconnected).toBe(true);
  });

  it("leaves the user where they scrolled when they are not following", () => {
    let follow!: ReturnType<typeof useAutoFollow<HTMLDivElement>>;
    const { getByTestId } = render(<Harness revision={1} probe={(f) => (follow = f)} />);
    const scroller = getByTestId("scroller");
    setLayout(scroller, { scrollHeight: 1000, clientHeight: 600 });
    scroller.scrollTop = 100;
    act(() => {
      scroller.dispatchEvent(new Event("scroll", { bubbles: true }));
    });
    expect(follow.following).toBe(false);

    setLayout(scroller, { scrollHeight: 1036, clientHeight: 600 });
    act(() => observers.forEach((o) => o.callback()));

    expect(scroller.scrollTop).toBe(100);
  });
});
