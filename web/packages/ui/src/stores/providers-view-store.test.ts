import { beforeEach, describe, expect, it } from "vitest";
import { selectProvider, useProvidersViewStore } from "@/stores/providers-view-store";

describe("useProvidersViewStore", () => {
  beforeEach(() => {
    useProvidersViewStore.getState().reset();
  });

  it("selecting a provider opens its detail and clears the query", () => {
    const store = useProvidersViewStore.getState();
    store.setQuery("claude");
    store.setFilter("attention");
    store.select("anthropic");
    const after = useProvidersViewStore.getState();
    expect(after.selectedProviderId).toBe("anthropic");
    expect(after.mobileView).toBe("detail");
    expect(after.query).toBe("");
    expect(after.filter).toBe("all");
  });

  it("back returns to the list without losing the selection", () => {
    selectProvider("openai");
    useProvidersViewStore.getState().back();
    const state = useProvidersViewStore.getState();
    expect(state.mobileView).toBe("list");
    expect(state.selectedProviderId).toBe("openai");
  });

  it("requestConnect selects the provider and flags a connect", () => {
    useProvidersViewStore.getState().requestConnect("zai");
    expect(useProvidersViewStore.getState().selectedProviderId).toBe("zai");
    expect(useProvidersViewStore.getState().connectProviderId).toBe("zai");
    useProvidersViewStore.getState().clearConnect();
    expect(useProvidersViewStore.getState().connectProviderId).toBeNull();
  });

  it("requestRelogin selects the provider and flags the account", () => {
    useProvidersViewStore.getState().requestRelogin(7, "claude-native");
    const state = useProvidersViewStore.getState();
    expect(state.selectedProviderId).toBe("claude-native");
    expect(state.mobileView).toBe("detail");
    expect(state.reloginAccountId).toBe(7);
    useProvidersViewStore.getState().clearRelogin();
    expect(useProvidersViewStore.getState().reloginAccountId).toBeNull();
  });

  it("reset restores the defaults", () => {
    const store = useProvidersViewStore.getState();
    store.requestConnect("openai");
    store.requestRelogin(3, "kimi");
    store.setQuery("q");
    store.setFilter("active");
    store.setSort("name");
    store.reset();
    expect(useProvidersViewStore.getState()).toMatchObject({
      selectedProviderId: null,
      mobileView: "list",
      query: "",
      filter: "all",
      sort: "status",
      connectProviderId: null,
      reloginAccountId: null,
    });
  });

  it("selecting a provider clears connect and relogin requests", () => {
    const store = useProvidersViewStore.getState();
    store.requestConnect("zai");
    store.requestRelogin(5, "claude-native");
    store.select("openai");
    const after = useProvidersViewStore.getState();
    expect(after.connectProviderId).toBeNull();
    expect(after.reloginAccountId).toBeNull();
  });

  it("connect and relogin requests are mutually exclusive", () => {
    useProvidersViewStore.getState().requestConnect("zai");
    useProvidersViewStore.getState().requestRelogin(5, "claude-native");
    expect(useProvidersViewStore.getState().connectProviderId).toBeNull();
    expect(useProvidersViewStore.getState().reloginAccountId).toBe(5);
    useProvidersViewStore.getState().requestConnect("kimi");
    expect(useProvidersViewStore.getState().connectProviderId).toBe("kimi");
    expect(useProvidersViewStore.getState().reloginAccountId).toBeNull();
  });

  it("select resets filter, sort and query", () => {
    const store = useProvidersViewStore.getState();
    store.setFilter("active");
    store.setSort("name");
    store.setQuery("q");
    store.select("openai");
    expect(useProvidersViewStore.getState()).toMatchObject({ filter: "all", sort: "status", query: "" });
  });
});
