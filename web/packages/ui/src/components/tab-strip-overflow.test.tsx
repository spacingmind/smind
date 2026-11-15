// DT-UI D1: tab chips, edge fades and the overflow menu. jsdom has no layout, so the scroller's geometry is stubbed.
import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { TabOverflowMenu } from "@/components/tab-overflow-menu";
import type { TabEntry } from "@/components/tab-registry";
import { useScrollEdges } from "@/hooks/use-scroll-edges";
import { useRef } from "react";

const tab = (key: string, title: string): TabEntry => ({ key, kind: "diff", title, closable: true }) as TabEntry;
const TABS = [tab("t1", "Diff"), tab("t2", "Files"), tab("t3", "Terminal 2")];

function Harness({ geometry }: { geometry: { scrollLeft: number; clientWidth: number; scrollWidth: number } }) {
  const ref = useRef<HTMLDivElement>(null);
  const edges = useScrollEdges(ref);
  return (
    <div>
      <div
        ref={(el) => {
          (ref as { current: HTMLDivElement | null }).current = el;
          if (el) {
            // Getters over the (mutable) geometry object, so a test can scroll the stub between renders.
            Object.defineProperty(el, "scrollLeft", { configurable: true, get: () => geometry.scrollLeft });
            Object.defineProperty(el, "clientWidth", { configurable: true, get: () => geometry.clientWidth });
            Object.defineProperty(el, "scrollWidth", { configurable: true, get: () => geometry.scrollWidth });
          }
        }}
        data-testid="scroller"
      >
        <div />
      </div>
      {edges.start && <i data-testid="fade-start" />}
      {edges.end && <i data-testid="fade-end" />}
    </div>
  );
}

afterEach(() => vi.restoreAllMocks());

describe("useScrollEdges", () => {
  it("shows no fade when the strip does not scroll", () => {
    render(<Harness geometry={{ scrollLeft: 0, clientWidth: 400, scrollWidth: 400 }} />);
    expect(screen.queryByTestId("fade-start")).toBeNull();
    expect(screen.queryByTestId("fade-end")).toBeNull();
  });
  it("shows only the right fade at the start of an overflowing strip", () => {
    render(<Harness geometry={{ scrollLeft: 0, clientWidth: 400, scrollWidth: 900 }} />);
    expect(screen.queryByTestId("fade-start")).toBeNull();
    expect(screen.getByTestId("fade-end")).toBeInTheDocument();
  });
  it("shows both in the middle and re-reads on scroll", () => {
    render(<Harness geometry={{ scrollLeft: 200, clientWidth: 400, scrollWidth: 900 }} />);
    expect(screen.getByTestId("fade-start")).toBeInTheDocument();
    expect(screen.getByTestId("fade-end")).toBeInTheDocument();
  });
  it("shows only the left fade at the end", () => {
    render(<Harness geometry={{ scrollLeft: 500, clientWidth: 400, scrollWidth: 900 }} />);
    expect(screen.getByTestId("fade-start")).toBeInTheDocument();
    expect(screen.queryByTestId("fade-end")).toBeNull();
  });
  it("updates when the scroller scrolls", () => {
    const geometry = { scrollLeft: 0, clientWidth: 400, scrollWidth: 900 };
    render(<Harness geometry={geometry} />);
    expect(screen.queryByTestId("fade-start")).toBeNull();
    geometry.scrollLeft = 300;
    fireEvent.scroll(screen.getByTestId("scroller"));
    expect(screen.getByTestId("fade-start")).toBeInTheDocument();
    expect(screen.getByTestId("fade-end")).toBeInTheDocument();
    geometry.scrollLeft = 500;
    fireEvent.scroll(screen.getByTestId("scroller"));
    expect(screen.queryByTestId("fade-end")).toBeNull();
  });
});

describe("TabOverflowMenu", () => {
  it("lists every tab with the active one marked, and choosing one activates it", async () => {
    const onActivate = vi.fn();
    render(<TabOverflowMenu tabs={TABS} activeKey="t2" onActivate={onActivate} />);
    fireEvent.pointerDown(screen.getByTestId("tab-overflow-button"), { button: 0, ctrlKey: false });
    const items = await screen.findAllByTestId("tab-overflow-item");
    expect(items.map((i) => i.getAttribute("data-tab-key"))).toEqual(["t1", "t2", "t3"]);
    expect(items.map((i) => i.getAttribute("aria-current"))).toEqual([null, "true", null]);
    // The mark is not colour alone: the current tab carries a check.
    expect(screen.getAllByTestId("tab-overflow-current")).toHaveLength(1);
    expect(items[1]!.querySelector('[data-testid="tab-overflow-current"]')).not.toBeNull();
    fireEvent.click(items[2]!);
    expect(onActivate).toHaveBeenCalledWith("t3");
  });
  it("brings the current tab into view when the menu opens", async () => {
    const scrollIntoView = vi.fn();
    Element.prototype.scrollIntoView = scrollIntoView;
    render(<TabOverflowMenu tabs={TABS} activeKey="t3" onActivate={() => {}} />);
    fireEvent.pointerDown(screen.getByTestId("tab-overflow-button"), { button: 0, ctrlKey: false });
    await screen.findAllByTestId("tab-overflow-item");
    expect(scrollIntoView).toHaveBeenCalledWith({ block: "nearest" });
    // Only the current tab asks for it.
    expect(scrollIntoView).toHaveBeenCalledTimes(1);
  });
  it("the button is named for assistive tech", () => {
    render(<TabOverflowMenu tabs={TABS} activeKey={null} onActivate={() => {}} />);
    expect(screen.getByRole("button", { name: "More tabs" })).toBeInTheDocument();
  });
});

describe("tab chips and the strip (source pins on App.tsx)", () => {
  it("tabs are 160px chips that shrink to 64px (96px when active), the list fills the scroller, the overflow menu is conditional", async () => {
    const { readFileSync } = await import("node:fs");
    const app = readFileSync("src/App.tsx", "utf8");
    expect(app).toContain("group-data-[variant=flat]/tabs-list:flex-[0_1_10rem]");
    expect(app).toContain("group-data-[variant=flat]/tabs-list:min-w-16");
    expect(app).toContain("group-data-[variant=flat]/tabs-list:aria-selected:min-w-24");
    expect(app).toContain('<TabsList variant="flat" className="w-full">');
    expect(app).toContain("{(edges.start || edges.end) && <TabOverflowMenu");
    expect(app).toContain("{edges.start && <div aria-hidden data-testid=\"tab-fade-start\"");
    expect(app).toContain("pointer-events-none absolute inset-y-0 left-0 w-4");
  });
});
