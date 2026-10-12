import { act, useRef } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { ChatOutlineRail } from "./chat-outline-rail";
import { useChatOutline } from "./use-chat-outline";

type Run = { id: string; prompt: string };

const resizeCallbacks: ResizeObserverCallback[] = [];
const rafCallbacks: FrameRequestCallback[] = [];
let renders = 0;
let lastPrompts: unknown = null;
const promptIdentities = new Set<unknown>();

class FakeResizeObserver {
  constructor(cb: ResizeObserverCallback) {
    resizeCallbacks.push(cb);
  }
  observe() {}
  unobserve() {}
  disconnect() {}
}

function flushResize() {
  act(() => resizeCallbacks.forEach((cb) => cb([], {} as ResizeObserver)));
}
function flushRaf() {
  act(() => {
    const pending = rafCallbacks.splice(0);
    pending.forEach((cb) => cb(0));
  });
}

const geometry = { scrollHeight: 2000, clientHeight: 500, clientWidth: 1000, scrollTop: 0 };
const TOPS: Record<string, number> = { r1: 0, r2: 600, r3: 1200 };

function Harness({ runs }: { runs: Run[] }) {
  renders += 1;
  const ref = useRef<HTMLDivElement>(null);
  const outline = useChatOutline(ref, runs);
  lastPrompts = outline.prompts;
  promptIdentities.add(outline.prompts);
  return (
    <div className="relative">
      <div ref={ref} data-testid="scroll">
        <div>
          <ul>
            {runs.map((run) => (
              <li key={run.id} data-run-id={run.id} data-testid={`li-${run.id}`} />
            ))}
          </ul>
        </div>
      </div>
      {outline.visible && (
        <ChatOutlineRail
          prompts={outline.prompts}
          activePrompt={outline.activePrompt}
          onJump={outline.jump}
          onScrub={outline.scrub}
        />
      )}
    </div>
  );
}

const RUNS: Run[] = [
  { id: "r1", prompt: "one" },
  { id: "r2", prompt: "two" },
  { id: "r3", prompt: "three" },
];

function stubGeometry(scroll: HTMLElement) {
  for (const key of ["scrollHeight", "clientHeight", "clientWidth", "scrollTop"] as const) {
    Object.defineProperty(scroll, key, {
      configurable: true,
      get: () => geometry[key],
      set: (value: number) => {
        geometry[key as "scrollTop"] = value;
      },
    });
  }
  scroll.getBoundingClientRect = () => ({ top: 0 }) as DOMRect;
  for (const li of scroll.querySelectorAll<HTMLElement>("[data-run-id]")) {
    li.getBoundingClientRect = () =>
      ({ top: TOPS[li.dataset.runId!] - geometry.scrollTop }) as DOMRect;
  }
}

function mount(runs = RUNS) {
  const utils = render(<Harness runs={runs} />);
  stubGeometry(screen.getByTestId("scroll"));
  flushResize();
  return utils;
}

describe("useChatOutline", () => {
  beforeEach(() => {
    resizeCallbacks.length = 0;
    rafCallbacks.length = 0;
    renders = 0;
    promptIdentities.clear();
    Object.assign(geometry, { scrollHeight: 2000, clientHeight: 500, clientWidth: 1000, scrollTop: 0 });
    vi.stubGlobal("ResizeObserver", FakeResizeObserver);
    vi.stubGlobal("requestAnimationFrame", (cb: FrameRequestCallback) => rafCallbacks.push(cb));
    vi.stubGlobal("cancelAnimationFrame", () => {});
  });
  afterEach(() => vi.unstubAllGlobals());

  it("rail hidden when content fits, shown when it overflows", () => {
    geometry.scrollHeight = 400;
    // Geometry is stubbed after mount, so the first measure saw jsdom zeros.
    mount();
    expect(screen.queryByTestId("chat-outline-rail")).toBeNull();
    geometry.scrollHeight = 2000;
    flushResize();
    expect(screen.getByTestId("chat-outline-rail")).toBeInTheDocument();
  });

  it("rail hidden below 860px pane width", () => {
    geometry.clientWidth = 600;
    mount();
    expect(screen.queryByTestId("chat-outline-rail")).toBeNull();
    geometry.clientWidth = 900;
    flushResize();
    expect(screen.getByTestId("chat-outline-rail")).toBeInTheDocument();
  });

  it("scroll publishes the active run without re-rendering the pane", () => {
    mount();
    flushRaf();
    const rendersBefore = renders;
    expect(screen.getByTestId("chat-outline-tick-r1")).toHaveAttribute("aria-selected", "true");
    geometry.scrollTop = 700;
    fireEvent.scroll(screen.getByTestId("scroll"));
    flushRaf();
    expect(screen.getByTestId("chat-outline-tick-r2")).toHaveAttribute("aria-selected", "true");
    expect(renders).toBe(rendersBefore);
    geometry.scrollTop = 1500; // scrollHeight - clientHeight
    fireEvent.scroll(screen.getByTestId("scroll"));
    flushRaf();
    expect(screen.getByTestId("chat-outline-tick-r3")).toHaveAttribute("aria-selected", "true");
    expect(renders).toBe(rendersBefore);
  });

  it("click on a tick scrolls the run into view", () => {
    mount();
    const scrollTo = vi.fn();
    screen.getByTestId("scroll").scrollTo = scrollTo;
    const tick = screen.getByTestId("chat-outline-tick-r2");
    // Keyboard-originated click (detail 0); a mouse press jumps on pointerdown instead.
    fireEvent.click(tick, { detail: 0 });
    expect(scrollTo).toHaveBeenCalledWith({ top: 600, behavior: "smooth" });
  });

  it("streaming a chunk does not change the prompts array identity", () => {
    const { rerender } = mount();
    const before = lastPrompts;
    rerender(<Harness runs={RUNS.map((run) => ({ ...run }))} />);
    expect(lastPrompts).toBe(before);
    rerender(<Harness runs={[...RUNS, { id: "r4", prompt: "four" }]} />);
    expect(lastPrompts).not.toBe(before);
  });
});
