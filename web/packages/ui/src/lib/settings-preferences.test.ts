import { afterEach, describe, expect, it } from "vitest";

import {
  applyFontSizes,
  DEFAULT_FONT_SIZES,
  readStoredDefaultApprovalPolicy,
  readStoredDefaultProvider,
  readStoredFontSizes,
  writeStoredDefaultApprovalPolicy,
  writeStoredDefaultProvider,
  writeStoredFontSizes,
} from "@/lib/settings-preferences";

afterEach(() => {
  window.localStorage.clear();
  document.documentElement.style.cssText = "";
});

describe("font sizes", () => {
  it("defaults to medium on every axis when nothing is stored", () => {
    expect(readStoredFontSizes()).toEqual(DEFAULT_FONT_SIZES);
  });

  it("round-trips a written value", () => {
    writeStoredFontSizes({ interface: "large", content: "small", code: "large" });
    expect(readStoredFontSizes()).toEqual({ interface: "large", content: "small", code: "large" });
  });

  it("falls back per-axis for a corrupt stored value, rather than blanking every axis", () => {
    window.localStorage.setItem("smind:settings:fontSizes", JSON.stringify({ interface: "huge", content: "small" }));
    expect(readStoredFontSizes()).toEqual({ interface: "medium", content: "small", code: "medium" });
  });

  it("survives malformed JSON entirely", () => {
    window.localStorage.setItem("smind:settings:fontSizes", "{not json");
    expect(readStoredFontSizes()).toEqual(DEFAULT_FONT_SIZES);
  });

  it("applies each axis to its own CSS custom property, scaled by step", () => {
    applyFontSizes({ interface: "small", content: "medium", code: "large" });
    const style = document.documentElement.style;
    expect(style.getPropertyValue("--font-scale-interface")).toBe("0.9");
    expect(style.getPropertyValue("--font-scale-content")).toBe("1");
    expect(style.getPropertyValue("--font-scale-code")).toBe("1.15");
  });
});

describe("default provider / approval policy preferences", () => {
  it("default to null (no preference) when nothing is stored", () => {
    expect(readStoredDefaultProvider()).toBeNull();
    expect(readStoredDefaultApprovalPolicy()).toBeNull();
  });

  it("round-trip a written value", () => {
    writeStoredDefaultProvider("glm");
    expect(readStoredDefaultProvider()).toBe("glm");

    writeStoredDefaultApprovalPolicy("auto-safe");
    expect(readStoredDefaultApprovalPolicy()).toBe("auto-safe");
  });

  it('writing null clears the stored value rather than storing the literal string "null"', () => {
    writeStoredDefaultProvider("glm");
    writeStoredDefaultProvider(null);
    expect(readStoredDefaultProvider()).toBeNull();
    expect(window.localStorage.getItem("smind:settings:defaultProvider")).toBeNull();
  });

  it("falls back to null for a corrupt/unrecognized stored value", () => {
    window.localStorage.setItem("smind:settings:defaultProvider", "not-a-real-provider");
    expect(readStoredDefaultProvider()).toBeNull();

    window.localStorage.setItem("smind:settings:defaultApprovalPolicy", "yolo");
    expect(readStoredDefaultApprovalPolicy()).toBeNull();
  });
});
