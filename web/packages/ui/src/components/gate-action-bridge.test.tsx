import { act, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { GateActionBridge } from "@/components/gate-action-bridge";
import { requestClearCooldown } from "@/lib/gate-events";
import { renderHookWithDaemon } from "@/test/render-with-daemon";
import { FakeUsageDaemon } from "@/test/usage-test-client";

function clearCalls(d: FakeUsageDaemon): unknown[] {
  return d.calls.filter((c) => c.method === "provider.clearCooldown").map((c) => c.params);
}

describe("GateActionBridge", () => {
  it("a clear request from any picker calls provider.clearCooldown once", async () => {
    const d = new FakeUsageDaemon([]);
    d.providers = { providers: [{ id: "glm", label: "GLM" }] };
    renderHookWithDaemon(() => GateActionBridge(), d, { initialProps: undefined });
    await act(async () => {
      requestClearCooldown("glm");
      await Promise.resolve();
    });
    await waitFor(() => expect(clearCalls(d)).toEqual([{ provider: "glm" }]));
  });

  it("stops listening once unmounted", async () => {
    const d = new FakeUsageDaemon([]);
    d.providers = { providers: [] };
    const view = renderHookWithDaemon(() => GateActionBridge(), d, { initialProps: undefined });
    view.unmount();
    await act(async () => {
      requestClearCooldown("glm");
      await Promise.resolve();
    });
    expect(clearCalls(d)).toEqual([]);
  });
});
