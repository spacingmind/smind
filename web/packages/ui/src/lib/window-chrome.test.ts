import { describe, expect, it } from "vitest";

import {
  SIDEBAR_HANDLE_PX,
  TRAFFIC_LIGHT_CLUSTER_PX,
  hasCaptionButtons,
  hasTrafficLights,
  headerLeftInsetPx,
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

  it("the header pads for the traffic lights exactly as far as the sidebar leaves them uncovered", () => {
    // Expanded: the sidebar's own top strip holds them.
    expect(headerLeftInsetPx("macos", false, 192)).toBe(0);
    expect(headerLeftInsetPx("macos", false, 512)).toBe(0);
    // Collapsed 48px rail: the cluster overhangs it and the handle.
    expect(headerLeftInsetPx("macos", false, 48)).toBe(TRAFFIC_LIGHT_CLUSTER_PX - 48 - SIDEBAR_HANDLE_PX);
    // Sidebar hidden (mobile sheet): the header is at the window edge.
    expect(headerLeftInsetPx("macos", false, 0)).toBe(TRAFFIC_LIGHT_CLUSTER_PX);
    // No lights, no inset.
    expect(headerLeftInsetPx("macos", true, 0)).toBe(0);
    expect(headerLeftInsetPx("windows", false, 0)).toBe(0);
    expect(headerLeftInsetPx(null, false, 0)).toBe(0);
  });
});
