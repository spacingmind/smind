import { afterEach, describe, expect, it } from "vitest";

import { useHomeProjectStore } from "@/stores/home-project-store";

describe("home project store", () => {
  afterEach(() => {
    window.localStorage.clear();
    useHomeProjectStore.getState().reload();
  });

  it("persists the chosen workspace id", () => {
    useHomeProjectStore.getState().set(7);
    expect(window.localStorage.getItem("smind:home-project")).toBe("7");

    useHomeProjectStore.setState({ value: null });
    useHomeProjectStore.getState().reload();
    expect(useHomeProjectStore.getState().value).toBe(7);

    useHomeProjectStore.getState().set(null);
    expect(window.localStorage.getItem("smind:home-project")).toBeNull();
  });
});
