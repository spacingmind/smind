import { describe, expect, it } from "vitest";

import { baseTabForKind, chatTab, fileTab, pluginTab } from "@/components/tab-registry";
import { pluginTabsToClose } from "@/lib/plugin-tabs";

const ACME_USAGE = { plugin: "acme", id: "usage", title: "Acme usage" } as const;

describe("pluginTabsToClose", () => {
  it("returns a plugin tab whose panel is absent from the list", () => {
    const tab = pluginTab(1, ACME_USAGE);
    expect(pluginTabsToClose([tab], [{ plugin: "other", id: "usage" }])).toEqual([tab]);
    expect(pluginTabsToClose([tab], [])).toEqual([tab]);
  });

  it("keeps a plugin tab whose (pluginId, panelId) pair is present", () => {
    const tab = pluginTab(1, ACME_USAGE);
    expect(pluginTabsToClose([tab], [{ plugin: "acme", id: "usage" }])).toEqual([]);
    // The pair is what matters: another plugin's panel with the same id does not save it.
    expect(pluginTabsToClose([tab], [{ plugin: "acme", id: "other" }])).toEqual([tab]);
  });

  it("never returns a non-plugin tab", () => {
    const tabs = [chatTab(1, { ID: 7, Title: "Chat" }), baseTabForKind(1, "diff"), fileTab(1, "a.ts")];
    expect(pluginTabsToClose(tabs, [])).toEqual([]);
  });

  it("with an empty panel list, every plugin tab is returned", () => {
    const a = pluginTab(1, ACME_USAGE);
    const b = pluginTab(1, { plugin: "zeta", id: "board", title: "Board" });
    expect(pluginTabsToClose([a, b], [])).toEqual([a, b]);
  });
});
