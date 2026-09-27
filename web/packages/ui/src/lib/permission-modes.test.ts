import { describe, expect, it } from "vitest";

import {
  defaultModeFor,
  describePermission,
  permissionModeLabel,
  providerModes,
  supportsAutoAccept,
  supportsLiveModeSwitch,
} from "@/lib/permission-modes";
import type { ProviderInfo } from "@/lib/types";

const served: ProviderInfo[] = [
  {
    id: "glm",
    modes: [
      { id: "default", label: "Default" },
      { id: "accept_edits", label: "Accept Edits" },
    ],
    defaultMode: "default",
    modesDiscovered: true,
    supportsAutoAccept: true,
    liveModeSwitch: true,
  },
  { id: "codex-native", modes: [{ id: "auto", label: "Default Permissions" }], defaultMode: "auto" },
];

describe("permission-modes", () => {
  it("prefers the daemon's served catalog", () => {
    expect(providerModes(served, "glm").map((m) => m.id)).toEqual(["default", "accept_edits"]);
    expect(permissionModeLabel(served, "glm", "accept_edits")).toBe("Accept Edits");
  });

  it("falls back to the static catalog before provider.list answers", () => {
    expect(providerModes([], "claude-native").map((m) => m.id)).toEqual(["acceptEdits", "default", "plan", "bypassPermissions"]);
    expect(defaultModeFor([], "claude-native")).toBe("acceptEdits");
    expect(defaultModeFor([], "codex-native")).toBe("auto");
  });

  it("resolves an empty mode to the provider default", () => {
    expect(permissionModeLabel([], "claude-native", "")).toBe("Accept File Edits");
  });

  it("shows an unknown mode id verbatim", () => {
    expect(permissionModeLabel(served, "glm", "yolo_mode")).toBe("yolo_mode");
  });

  it("knows which providers take autoAccept and live switches", () => {
    expect(supportsAutoAccept(served, "glm")).toBe(true);
    expect(supportsAutoAccept([], "kimi")).toBe(true);
    expect(supportsAutoAccept([], "claude-native")).toBe(false);
    expect(supportsLiveModeSwitch(served, "codex-native")).toBe(false);
    expect(supportsLiveModeSwitch([], "codex-native")).toBe(false);
    expect(supportsLiveModeSwitch([], "claude-native")).toBe(true);
  });

  it("describes mode plus auto-accept", () => {
    expect(describePermission(served, "glm", "", true)).toBe("Default · Auto-accept");
    expect(describePermission([], "claude-native", "plan", false)).toBe("Plan Mode");
  });
});
