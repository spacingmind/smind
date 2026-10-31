import { act, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { useProviderCatalog } from "@/hooks/use-provider-catalog";
import { useAddFromRecipe, useProviderRecipes, useProviderSecret } from "@/hooks/use-provider-recipes";
import type { AddFromRecipeResult, ProviderInfo, ProviderRecipe } from "@/lib/types";
import { renderWithDaemon } from "@/test/render-with-daemon";
import { FakeUsageDaemon } from "@/test/usage-test-client";

/** The write-only key the tests paste; never a real-looking credential. */
const KEY = "sk-test-SENTINEL-0001";

function recipe(): ProviderRecipe {
  return {
    id: "zai-glm",
    label: "GLM Coding Plan",
    description: "GLM's Anthropic-compatible endpoint",
    harness: "claude-code",
    endpointKind: "anthropic",
    vendor: "zai",
    env: [{ name: "ANTHROPIC_AUTH_TOKEN", kind: "secret", value: "" }],
    models: [{ id: "glm-4.7", label: "GLM-4.7", default: true }],
    modelsRequired: false,
    disallowedTools: [],
    thinking: "supported",
    notes: [],
    docs: "https://docs.z.ai",
    source: "vendor-docs",
    verifiedAt: "2026-01-01",
    unverified: false,
    unverifiedAspects: [],
    needsBaseUrl: false,
    needsKey: true,
  };
}

function instance(over: Partial<ProviderInfo> = {}): ProviderInfo {
  return { id: "glm", label: "GLM", custom: true, runner: "claude-native", secrets: [{ name: "api-key", env: "ANTHROPIC_AUTH_TOKEN", set: false }], ...over };
}

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

function paramsOf(d: FakeUsageDaemon, method: string): unknown[] {
  return d.calls.filter((c) => c.method === method).map((c) => c.params);
}

describe("useProviderRecipes", () => {
  it("useProviderRecipes lists the recipes the daemon serves and stays idle while disabled", async () => {
    const d = new FakeUsageDaemon([]);
    d.recipes = { recipes: [recipe()] };
    let enabled = false;
    const seen: { recipes: ProviderRecipe[]; loaded: boolean; error: string | null }[] = [];
    function Probe() {
      seen.push(useProviderRecipes(enabled));
      return null;
    }
    const view = renderWithDaemon(<Probe />, d);
    await flush();
    expect(d.countCalls("provider.recipes")).toBe(0);
    expect(seen.at(-1)).toEqual({ recipes: [], loaded: false, error: null });
    enabled = true;
    await act(async () => {
      view.rerenderWith(d, <Probe />);
    });
    await waitFor(() => expect(seen.at(-1)?.loaded).toBe(true));
    expect(d.countCalls("provider.recipes")).toBe(1);
    expect(seen.at(-1)?.recipes).toHaveLength(1);
    expect(seen.at(-1)?.recipes[0]).toMatchObject({ id: "zai-glm", needsKey: true });
    expect(seen.at(-1)?.error).toBeNull();
  });

  it("useProviderRecipes surfaces the daemon's error text", async () => {
    const d = new FakeUsageDaemon([]);
    d.recipesError = new Error("provider.recipes: the catalog did not load");
    let error: string | null = null;
    function Probe() {
      ({ error } = useProviderRecipes(true));
      return null;
    }
    renderWithDaemon(<Probe />, d);
    await waitFor(() => expect(error).toBe("provider.recipes: the catalog did not load"));
  });
});

describe("useAddFromRecipe", () => {
  it("useAddFromRecipe sends the params, invalidates provider.list and exposes the daemon error text", async () => {
    const d = new FakeUsageDaemon([]);
    const adder: { add?: ReturnType<typeof useAddFromRecipe> } = {};
    function Probe() {
      useProviderCatalog(); // keeps provider.list an active query, so the success invalidation re-reads it
      adder.add = useAddFromRecipe();
      return null;
    }
    renderWithDaemon(<Probe />, d);
    await flush();
    expect(d.countCalls("provider.list")).toBe(1);

    let added: AddFromRecipeResult | undefined;
    await act(async () => {
      adder.add!.add({ recipe: "zai-glm", id: "glm", label: "GLM", secrets: { "api-key": KEY } }, (r) => (added = r));
    });
    await flush();
    expect(paramsOf(d, "provider.addFromRecipe")).toEqual([{ recipe: "zai-glm", id: "glm", label: "GLM", secrets: { "api-key": KEY } }]);
    expect(added).toMatchObject({ id: "glm", created: true, secrets: [{ name: "api-key", set: true }] });
    expect(d.providers.providers.map((p) => p.id)).toContain("glm");
    await waitFor(() => expect(d.countCalls("provider.list")).toBeGreaterThanOrEqual(2));
    expect(adder.add!.error).toBeNull();

    d.addFromRecipeError = new Error("id already used");
    await act(async () => {
      adder.add!.add({ recipe: "zai-glm", id: "glm", secrets: { "api-key": KEY } });
    });
    await flush();
    expect(adder.add!.error).toBe("id already used");
  });
});

describe("useProviderSecret", () => {
  it("useProviderSecret sets and clears by instance and name", async () => {
    const d = new FakeUsageDaemon([]);
    d.providers = { providers: [instance()] };
    const secrets: { set?: ReturnType<typeof useProviderSecret> } = {};
    let done = 0;
    function Probe() {
      secrets.set = useProviderSecret();
      return null;
    }
    renderWithDaemon(<Probe />, d);
    await flush();

    await act(async () => {
      secrets.set!.set({ instanceId: "glm", name: "api-key", value: KEY }, () => (done += 1));
    });
    await flush();
    expect(paramsOf(d, "provider.setSecret")).toEqual([{ instanceId: "glm", name: "api-key", value: KEY }]);
    expect(d.providers.providers[0]?.secrets).toEqual([{ name: "api-key", env: "ANTHROPIC_AUTH_TOKEN", set: true, fingerprint: "0002" }]);
    expect(done).toBe(1);
    expect(secrets.set!.error).toBeNull();

    await act(async () => {
      secrets.set!.clear({ instanceId: "glm", name: "api-key" }, () => (done += 1));
    });
    await flush();
    expect(paramsOf(d, "provider.clearSecret")).toEqual([{ instanceId: "glm", name: "api-key" }]);
    expect(d.providers.providers[0]?.secrets).toEqual([{ name: "api-key", env: "ANTHROPIC_AUTH_TOKEN", set: false }]);
    expect(done).toBe(2);
  });
});
