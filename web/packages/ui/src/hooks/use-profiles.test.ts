import { act } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { FALLBACK_PROVIDERS } from "@/lib/provider-catalog-fallback";
import { useProviderCatalog } from "@/hooks/use-provider-catalog";
import { useCreateProfile, useDeleteProfile, useProfiles, useUpdateProfile } from "@/hooks/use-profiles";

import type { ProviderInfo } from "@/lib/types";
import { renderHookWithDaemon } from "@/test/render-with-daemon";
import { FakeUsageDaemon } from "@/test/usage-test-client";

const PROVIDERS: ProviderInfo[] = [{ id: "claude-native", label: "Claude" }, { id: "glm" }];

async function flush() {
  await act(async () => {
    await new Promise((r) => setTimeout(r, 0));
  });
}

const profile = (id: number, name: string) => ({
  ID: id,
  Name: name,
  Provider: "claude-native",
  PermissionMode: "",
  AutoAccept: false,
  ThinkingLevel: "",
  Notes: "",
  CreatedAt: "",
  UpdatedAt: "",
});

describe("useProviderCatalog", () => {
  it("many consumers share one provider.list fetch", async () => {
    const daemon = new FakeUsageDaemon([]);
    daemon.providers = { providers: PROVIDERS };
    const a = renderHookWithDaemon(() => useProviderCatalog(), daemon, { initialProps: undefined });
    const b = renderHookWithDaemon(() => useProviderCatalog(), daemon, { initialProps: undefined, queryClient: a.queryClient() });
    await flush();
    expect(daemon.countCalls("provider.list")).toBe(1);
    expect(a.result.current.providers.map((p) => p.id)).toEqual(["claude-native", "glm"]);
    expect(b.result.current.providersOrFallback).toEqual(PROVIDERS);
    expect(b.result.current.loaded).toBe(true);
    a.unmount();
    b.unmount();
  });

  it("while loading and after a failure it serves the fallback and never throws", async () => {
    const daemon = new FakeUsageDaemon([]);
    const error = vi.spyOn(console, "error").mockImplementation(() => {});
    try {
      // Empty list (the daemon found nothing): loaded but unusable -> fallback.
      daemon.providers = { providers: [] };
      const first = renderHookWithDaemon(() => useProviderCatalog(), daemon, { initialProps: undefined });
      await flush();
      expect(first.result.current.loaded).toBe(true);
      expect(first.result.current.providers).toEqual([]);
      expect(first.result.current.providersOrFallback).toEqual(FALLBACK_PROVIDERS);
      first.unmount();

      // A failed list: fallback again, no thrown error to the UI.
      daemon.failNonUsageWith = new Error("nope");
      daemon.providers = { providers: PROVIDERS };
      const second = renderHookWithDaemon(() => useProviderCatalog(), daemon, { initialProps: undefined });
      await flush();
      expect(second.result.current.loaded).toBe(false);
      expect(second.result.current.error).toBeInstanceOf(Error);
      expect(second.result.current.providersOrFallback).toEqual(FALLBACK_PROVIDERS);
      second.unmount();
    } finally {
      error.mockRestore();
    }
  });
});

describe("useProfiles", () => {
  it("loads once and mutations keep the cache live", async () => {
    const daemon = new FakeUsageDaemon([]);
    const { result } = renderHookWithDaemon(
      () => ({
        profiles: useProfiles(),
        create: useCreateProfile(),
        update: useUpdateProfile(),
        remove: useDeleteProfile(),
      }),
      daemon,
      { initialProps: undefined },
    );
    await flush();
    expect(daemon.countCalls("profile.list")).toBe(1);
    expect(result.current.profiles.data).toEqual([]);

    await act(async () => {
      result.current.create.mutateAsync({ name: "Quick", provider: "claude-native", permissionMode: "", autoAccept: false, thinkingLevel: "", model: "", notes: "" });
    });
    expect(result.current.profiles.data?.map((p) => p.Name)).toEqual(["Quick"]);

    await act(async () => {
      result.current.update.mutateAsync({ id: 1, name: "Quicker", provider: "claude-native", permissionMode: "", autoAccept: false, thinkingLevel: "", model: "", notes: "" });
    });
    expect(result.current.profiles.data?.map((p) => p.Name)).toEqual(["Quicker"]);

    await act(async () => {
      result.current.remove.mutateAsync(1);
    });
    expect(result.current.profiles.data).toEqual([]);
    expect(daemon.countCalls("profile.list")).toBe(1);
  });

  it("update passes the seat-contract and failover params through to profile.update", async () => {
    const daemon = new FakeUsageDaemon([]);
    daemon.profiles = [profile(1, "One")];
    const { result } = renderHookWithDaemon(() => useUpdateProfile(), daemon, { initialProps: undefined });
    await flush();
    const params = {
      id: 1,
      name: "One",
      provider: "claude-native",
      permissionMode: "",
      autoAccept: false,
      thinkingLevel: "",
      model: "",
      notes: "",
      onRateLimit: "fallback",
      fallbackProfileId: 2,
      orchestrationTools: true,
      instructions: "be brief",
      skills: ["smind-peer"],
      childPolicy: "profiles",
      childProfileIds: [2, 3],
    };
    await act(async () => {
      await result.current.mutateAsync(params);
    });
    expect(daemon.calls.find((c) => c.method === "profile.update")?.params).toEqual(params);
  });

  it("profile events from another tab reach the cache", async () => {
    const daemon = new FakeUsageDaemon([]);
    daemon.profiles = [profile(1, "One")];
    const { result } = renderHookWithDaemon(() => useProfiles(), daemon, { initialProps: undefined });
    await flush();
    await act(async () => {
      daemon.emit("profile.created", { profile: profile(2, "Two") });
      daemon.emit("profile.deleted", { id: 1 });
    });
    await flush();
    expect(result.current.data?.map((p) => p.Name)).toEqual(["Two"]);
  });
});
