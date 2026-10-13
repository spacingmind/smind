import { describe, expect, it } from "vitest";

import {
  HEADER_BASE_PADDING_PX,
  SIDEBAR_TOGGLE_SIZE_PX,
  TRAFFIC_LIGHT_CLUSTER_PX,
  hasCaptionButtons,
  hasTrafficLights,
  headerLeftInsetPx,
  toggleLeftPx,
  toggleZoneEndPx,
} from "@/lib/window-chrome";

describe("window chrome layout maths", () => {
  it("traffic lights exist on macOS only, and not in fullscreen", () => {
    expect(hasTrafficLights("macos", false)).toBe(true);
    expect(hasTrafficLights("macos", true)).toBe(false);
    expect(hasTrafficLights("windows", false)).toBe(false);
    expect(hasTrafficLights("linux", false)).toBe(false);
    expect(hasTrafficLights(null, false)).toBe(false);
  });

  it("caption buttons exist on Windows and Linux only", () => {
    expect(hasCaptionButtons("windows")).toBe(true);
    expect(hasCaptionButtons("linux")).toBe(true);
    expect(hasCaptionButtons("macos")).toBe(false);
    expect(hasCaptionButtons(null)).toBe(false);
  });

  it("without the window toggle the header pads only as far as the lights need", () => {
    // Expanded: the sidebar's own top strip holds them.
    expect(headerLeftInsetPx("macos", false, 192)).toBe(0);
    expect(headerLeftInsetPx("macos", false, 512)).toBe(0);
    // Collapsed 48px rail: the cluster overhangs it.
    expect(headerLeftInsetPx("macos", false, 48)).toBe(TRAFFIC_LIGHT_CLUSTER_PX - 48);
    // Sidebar hidden (mobile sheet, Settings): the header is at the window edge.
    expect(headerLeftInsetPx("macos", false, 0)).toBe(TRAFFIC_LIGHT_CLUSTER_PX);
    // No lights, no inset.
    expect(headerLeftInsetPx("macos", true, 0)).toBe(0);
    expect(headerLeftInsetPx("windows", false, 0)).toBe(0);
    expect(headerLeftInsetPx(null, false, 0)).toBe(0);
  });

  it("the sidebar toggle sits at one fixed x per platform, right after the lights on macOS", () => {
    expect(toggleLeftPx("macos", false)).toBe(TRAFFIC_LIGHT_CLUSTER_PX);
    // No lights (Windows/Linux, macOS fullscreen): centred in the 48px rail.
    expect(toggleLeftPx("windows", false)).toBe(10);
    expect(toggleLeftPx("linux", false)).toBe(10);
    expect(toggleLeftPx("macos", true)).toBe(10);
    expect(toggleZoneEndPx("macos", false)).toBe(TRAFFIC_LIGHT_CLUSTER_PX + SIDEBAR_TOGGLE_SIZE_PX + 8);
    expect(toggleZoneEndPx("windows", false)).toBe(46);
  });

  it("with the window toggle the header clears the toggle zone and never pads when the sidebar already does", () => {
    const zone = toggleZoneEndPx("macos", false);
    // Collapsed rail: content starts exactly at the end of the toggle zone.
    expect(48 + HEADER_BASE_PADDING_PX + headerLeftInsetPx("macos", false, 48, true)).toBe(zone);
    // Expanded: the sidebar's strip holds the toggle.
    expect(headerLeftInsetPx("macos", false, 192, true)).toBe(0);
    // No lights: the toggle fits in the rail, the header needs nothing.
    expect(headerLeftInsetPx("windows", false, 48, true)).toBe(0);
    expect(headerLeftInsetPx("macos", true, 48, true)).toBe(0);
  });
});
