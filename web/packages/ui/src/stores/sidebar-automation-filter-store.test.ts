import { beforeEach, describe, expect, it } from "vitest";

import { reloadAllPreferenceStores } from "@/stores/create-preference-store";
import { useSidebarAutomationFilterStore } from "@/stores/sidebar-automation-filter-store";

describe("sidebar automation filter store", () => {
  beforeEach(() => {
    window.localStorage.clear();
    reloadAllPreferenceStores();
  });

  it("defaults to false", () => {
    expect(useSidebarAutomationFilterStore.getState().value).toBe(false);
  });

  it("persists a change and reloads it", () => {
    useSidebarAutomationFilterStore.getState().set(true);
    useSidebarAutomationFilterStore.setState({ value: false });
    useSidebarAutomationFilterStore.getState().reload();
    expect(useSidebarAutomationFilterStore.getState().value).toBe(true);
  });
});
