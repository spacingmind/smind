// DT-UI B follow-up: at narrow container widths (the 800 px window minimum with the app sidebar beside it) the settings
// section nav shrinks to an icon rail. The width is the screen's own, not the window's, so ResizeObserver is driven here.
import { act, fireEvent, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { COMPACT_NAV_BELOW_PX, SettingsScreen } from "@/components/settings/settings-screen";
import { listSettingsSections } from "@/components/settings/settings-registry";
import { renderWithClientProp } from "@/test/render-with-daemon";

let width = 1000;
let observers: Array<(entries: Array<{ contentRect: { width: number } }>) => void> = [];

beforeEach(() => {
  width = 1000;
  observers = [];
  vi.stubGlobal(
    "ResizeObserver",
    class {
      constructor(cb: (entries: Array<{ contentRect: { width: number } }>) => void) {
        observers.push(cb);
      }
      observe() {}
      disconnect() {}
      unobserve() {}
    },
  );
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(() => ({ width, height: 600, top: 0, left: 0, right: width, bottom: 600, x: 0, y: 0, toJSON() {} }) as DOMRect);
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const mount = () => renderWithClientProp(<SettingsScreen client={null} events={null} onNavigateBack={() => {}} />);
const resizeTo = (w: number) => act(() => { width = w; for (const cb of observers) cb([{ contentRect: { width: w } }]); });

describe("SettingsScreen compact nav", () => {
  it("shows the full nav (labels, group headings) at a wide container", () => {
    mount();
    expect(screen.getByTestId("settings-screen")).toHaveAttribute("data-nav", "full");
    const first = listSettingsSections()[0]!;
    expect(within(screen.getByTestId(`settings-nav-${first.id}`)).getByText(first.label)).toBeInTheDocument();
    expect(screen.getByTestId("settings-sidebar")).toHaveClass("w-64");
  });

  it("shrinks to an icon rail below the threshold: no visible labels, each button named, sidebar narrow", () => {
    width = 540;
    mount();
    expect(screen.getByTestId("settings-screen")).toHaveAttribute("data-nav", "rail");
    expect(screen.getByTestId("settings-sidebar")).toHaveClass("w-12");
    for (const section of listSettingsSections()) {
      const button = screen.getByTestId(`settings-nav-${section.id}`);
      expect(button).toHaveAttribute("aria-label", section.label); // not only the title: screen readers read the label
      expect(button).toHaveAccessibleName(section.label);
      expect(button).toHaveAttribute("title", section.label);
      expect(button).not.toHaveTextContent(section.label);
    }
    expect(screen.getByTestId("settings-back-button")).toHaveAccessibleName("Back to app");
    expect(screen.getByTestId("settings-back-button")).not.toHaveTextContent("Back to app");
  });

  it("the rail still selects a section and marks it current", () => {
    width = 540;
    mount();
    const sections = listSettingsSections();
    const target = sections[1]!;
    fireEvent.click(screen.getByTestId(`settings-nav-${target.id}`));
    expect(screen.getByTestId(`settings-nav-${target.id}`)).toHaveAttribute("aria-current", "true");
    expect(screen.getByTestId("settings-page-title")).toHaveTextContent(target.label);
  });

  it("follows the container width both ways, switching exactly at the threshold", () => {
    mount();
    resizeTo(COMPACT_NAV_BELOW_PX - 1);
    expect(screen.getByTestId("settings-screen")).toHaveAttribute("data-nav", "rail");
    resizeTo(COMPACT_NAV_BELOW_PX);
    expect(screen.getByTestId("settings-screen")).toHaveAttribute("data-nav", "full");
    resizeTo(400);
    expect(screen.getByTestId("settings-screen")).toHaveAttribute("data-nav", "rail");
  });

  it("keeps the selected section when the nav changes shape", () => {
    mount();
    const target = listSettingsSections()[2]!;
    fireEvent.click(screen.getByTestId(`settings-nav-${target.id}`));
    resizeTo(500);
    expect(screen.getByTestId(`settings-nav-${target.id}`)).toHaveAttribute("aria-current", "true");
  });
});
