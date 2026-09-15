import { afterEach, describe, expect, it } from "vitest";

import { listSettingsSections, registerSettingsSection } from "@/components/settings/settings-registry";

const registered: (() => void)[] = [];
function register(...args: Parameters<typeof registerSettingsSection>): void {
  registered.push(registerSettingsSection(...args));
}

afterEach(() => {
  while (registered.length > 0) registered.pop()!();
});

describe("settings registry", () => {
  it("returns sections sorted by order, ties broken by registration order", () => {
    register({ id: "c", label: "C", order: 300, render: () => null });
    register({ id: "a", label: "A", order: 100, render: () => null });
    register({ id: "b1", label: "B1", order: 200, render: () => null });
    register({ id: "b2", label: "B2", order: 200, render: () => null });

    expect(listSettingsSections().map((s) => s.id)).toEqual(["a", "b1", "b2", "c"]);
  });

  it("unregister removes exactly the one section, leaving the others in place", () => {
    register({ id: "keep", label: "Keep", order: 100, render: () => null });
    const unregisterTemp = registerSettingsSection({ id: "temp", label: "Temp", order: 200, render: () => null });

    expect(listSettingsSections().map((s) => s.id)).toContain("temp");
    unregisterTemp();
    expect(listSettingsSections().map((s) => s.id)).not.toContain("temp");
    expect(listSettingsSections().map((s) => s.id)).toContain("keep");
  });

  it("returns a fresh array each call, so a caller can't mutate the live registry through it", () => {
    register({ id: "x", label: "X", order: 100, render: () => null });
    const first = listSettingsSections();
    first.pop();
    expect(listSettingsSections().map((s) => s.id)).toContain("x");
  });
});
