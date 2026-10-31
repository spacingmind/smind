import { describe, expect, it } from "vitest";

import { Icons } from "@/lib/icons";
import {
  iconForGroup,
  PALETTE_GROUP_ICON,
  paletteCategory,
} from "@/palette/categories";

describe("palette categories", () => {
  it("paletteCategory maps groups to chips", () => {
    for (const group of ["Tasks", "Workspaces", "Chats"]) {
      expect(paletteCategory(group)).toBe("tasks");
    }
    expect(paletteCategory("Files")).toBe("files");
    for (const group of ["Actions", "Open", "Settings", "Agents", "Commands"]) {
      expect(paletteCategory(group)).toBe("actions");
    }
  });

  it("unknown groups fall into Actions", () => {
    expect(paletteCategory("Elsewhere")).toBe("actions");
    expect(paletteCategory("zzz")).toBe("actions");
  });

  it("iconForGroup returns a registry key for every known group", () => {
    const registryKeys = Object.keys(Icons);
    for (const name of Object.values(PALETTE_GROUP_ICON)) {
      expect(registryKeys).toContain(name);
    }
    expect(registryKeys).toContain(iconForGroup("zzz"));
  });
});
