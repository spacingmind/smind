// DT-UI D1: tab chips, edge fades and the overflow menu. jsdom has no layout, so the scroller's geometry is stubbed.
import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { TabOverflowMenu } from "@/components/tab-overflow-menu";
import { TabLabel, type TabEntry } from "@/components/tab-registry";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { useRevealActiveTab } from "@/hooks/use-reveal-active-tab";
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

describe("TabOverflowMenu titles", () => {
  it("numbers identical titles so the rows can be told apart", async () => {
    const chats = [tab("c1", "Chat"), tab("c2", "Chat"), tab("c3", "Chat"), tab("t1", "Diff")];
    render(<TabOverflowMenu tabs={chats} activeKey="c3" onActivate={() => {}} />);
    fireEvent.pointerDown(screen.getByTestId("tab-overflow-button"), { button: 0, ctrlKey: false });
    const items = await screen.findAllByTestId("tab-overflow-item");
    // The number is its own element (the chip keeps it when the title is cut); the title text is the base title.
    expect(items.map((i) => i.querySelector("[data-tab-title]")?.textContent)).toEqual(["Chat", "Chat", "Chat", "Diff"]);
    expect(items.map((i) => i.querySelector("[data-tab-number]")?.textContent ?? null)).toEqual([null, "2", "3", null]);
  });
});

describe("TabLabel numbers (the strip's chips and the overflow menu share one function)", () => {
  it("chip-shows-the-repeat-number-in-its-own-non-shrinking-span", () => {
    const { container } = render(
      <div>
        <TabLabel entry={tab("c1", "Chat")} number={1} />
        <TabLabel entry={tab("c2", "Chat")} number={2} />
      </div>,
    );
    const numbers = container.querySelectorAll("[data-tab-number]");
    expect(numbers).toHaveLength(1); // the first keeps its plain title
    expect(numbers[0]).toHaveTextContent("2");
    expect(numbers[0].className).toContain("shrink-0"); // never cut: the title (min-w-0 truncate) shrinks first
    for (const t of container.querySelectorAll("[data-tab-title]")) expect(t.className).toContain("truncate");
  });

  it("long-title-keeps-its-number-and-cuts-from-the-start", () => {
    const long = "A very long chat title that cannot fit in a narrow chip";
    const { container } = render(<TabLabel entry={tab("c11", long)} number={11} />);
    const title = container.querySelector("[data-tab-title]") as HTMLElement;
    const number = container.querySelector("[data-tab-number]") as HTMLElement;
    expect(number).toHaveTextContent("11");
    expect(number.className).toContain("shrink-0"); // never shrinks, so it is never cut
    // the cut is from the start: right-to-left box (ellipsis on the left), text-overflow via truncate, characters isolated
    expect(title.getAttribute("dir")).toBe("rtl");
    expect(title.className).toContain("truncate");
    expect(title.className).toContain("min-w-0");
    expect(title.querySelector("bdi")).toHaveTextContent(long); // the full text stays in the DOM, in reading order
    expect(title.nextElementSibling).toBe(number); // the number follows the title
  });

  it("strip-and-menu-call-the-same-numbering-function", () => {
    const app = readFileSync(resolve(__dirname, "../App.tsx"), "utf8");
    const menu = readFileSync(resolve(__dirname, "./tab-overflow-menu.tsx"), "utf8");
    expect(app).toContain("disambiguatedParts(tabs)");
    expect(app).toContain("number={tabParts.get(entry.key)?.n}");
    expect(app).toContain("<TabLabel entry={entry} number={number} />"); // the chip hands it on to the label
    expect(menu).toContain("disambiguatedParts(tabs)");
    expect(menu).toContain("number={parts.get(entry.key)?.n}");
    // and neither re-derives numbers by itself
    expect(app).not.toMatch(/`\$\{[a-z.]*title\} \$\{/);
  });
});

describe("useRevealActiveTab", () => {
  function Strip({ geometry, active }: { geometry: { scrollLeft: number }; active: string }) {
    const ref = useRef<HTMLDivElement>(null);
    useRevealActiveTab(ref, active);
    return (
      <div
        data-testid="strip"
        ref={(el) => {
          (ref as { current: HTMLDivElement | null }).current = el;
          if (!el) return;
          Object.defineProperty(el, "scrollLeft", { configurable: true, get: () => geometry.scrollLeft, set: (v: number) => (geometry.scrollLeft = v) });
          Object.defineProperty(el, "clientWidth", { configurable: true, get: () => 300 });
          Object.defineProperty(el, "scrollWidth", { configurable: true, get: () => 1200 });
          el.getBoundingClientRect = () => ({ left: 0, right: 300, top: 0, bottom: 28, width: 300, height: 28, x: 0, y: 0, toJSON() {} }) as DOMRect;
        }}
      >
        {["a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l"].map((key, index) => (
          <div
            key={key}
            role="tab"
            aria-selected={key === active}
            ref={(el) => {
              if (el) el.getBoundingClientRect = () => ({ left: index * 100 - geometry.scrollLeft, right: index * 100 + 100 - geometry.scrollLeft, top: 0, bottom: 28, width: 100, height: 28, x: 0, y: 0, toJSON() {} }) as DOMRect;
            }}
          />
        ))}
      </div>
    );
  }

  it("leaves the active chip fully inside the strip with clear space for the fade, in either direction", () => {
    const geometry = { scrollLeft: 0 };
    const { rerender } = render(<Strip geometry={geometry} active="a" />);
    rerender(<Strip geometry={geometry} active="e" />); // chip at 400..500, view 0..300
    expect(geometry.scrollLeft).toBe(500 - 300 + 20);
    const chipRight = 500 - geometry.scrollLeft;
    expect(chipRight).toBeLessThanOrEqual(300 - 20);
    rerender(<Strip geometry={geometry} active="b" />); // chip at 100..200, view now 220..520
    expect(geometry.scrollLeft).toBe(100 - 20);
    expect(100 - geometry.scrollLeft).toBeGreaterThanOrEqual(20);
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
