import { afterEach, describe, expect, it } from "vitest";

import type { RunConfigState } from "@/components/composer/run-config-toolbar";
import { useHomeRunConfigStore } from "@/stores/home-run-config-store";

const config: RunConfigState = {
  baseAgentId: null,
  custom: true,
  provider: "claude-native",
  permissionMode: "plan",
  autoAccept: false,
  thinkingLevel: "standard",
  model: "",
};

describe("home run-config store", () => {
  afterEach(() => {
    window.localStorage.clear();
    useHomeRunConfigStore.getState().reload();
  });

  it("round trips and rejects garbage", () => {
    useHomeRunConfigStore.getState().set(config);
    useHomeRunConfigStore.setState({ value: null });
    useHomeRunConfigStore.getState().reload();
    expect(useHomeRunConfigStore.getState().value).toEqual(config);

    window.localStorage.setItem("smind:home-run-config", "{nope");
    useHomeRunConfigStore.getState().reload();
    expect(useHomeRunConfigStore.getState().value).toBeNull();

    window.localStorage.setItem("smind:home-run-config", JSON.stringify({ provider: 3 }));
    useHomeRunConfigStore.getState().reload();
    expect(useHomeRunConfigStore.getState().value).toBeNull();

    useHomeRunConfigStore.getState().set(null);
    expect(window.localStorage.getItem("smind:home-run-config")).toBeNull();
  });
});
