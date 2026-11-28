import { fireEvent, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { renderWithDaemon } from "@/test/render-with-daemon";
import { FakeUsageDaemon } from "@/test/usage-test-client";

type Listing = {
  enabled: boolean;
  paused: boolean;
  automations: unknown[];
} | null;

/** The section only reads `useAutomations().data`, so the hook is mocked with a fixed listing. */
async function renderSection(listing: Listing) {
  vi.doMock("@/hooks/use-automations", () => ({
    useAutomations: () => ({ data: listing }),
  }));
  const { listSettingsSections } = await import("@/components/settings/settings-registry");
  await import("@/components/settings/automations-link-section");
  const section = listSettingsSections().find((s) => s.id === "automations");
  if (!section) throw new Error("automations section did not register");
  const closeSettings = vi.fn();
  const daemon = new FakeUsageDaemon([]);
  const call = daemon.client.call.bind(daemon.client);
  (daemon.client as { call: unknown }).call = async (method: string, params?: unknown) =>
    method === "config.get"
      ? {
          keys: ["automations.enabled", "automations.paused"].map((key) => ({
            key,
            value: "false",
            default: "false",
            source: "file",
            restart: false,
            writable: true,
          })),
        }
      : call(method, params);
  renderWithDaemon(<>{section.render({ client: null, events: null, closeSettings })}</>, daemon);
  return { closeSettings };
}

afterEach(() => {
  vi.resetModules();
  vi.restoreAllMocks();
  vi.doUnmock("@/hooks/use-automations");
});

describe("Settings -> Automations link section", () => {
  it("ui_cleanup_b2_automations_is_a_group_and_row", async () => {
    await renderSection({ enabled: true, paused: false, automations: [] });

    const state = screen.getByTestId("settings-automations-state");
    const button = screen.getByTestId("settings-open-automations");
    for (const el of [state, button]) {
      expect(el.closest('[data-slot="settings-row"]')).not.toBeNull();
      expect(el.closest('[data-slot="settings-group"]')).not.toBeNull();
    }
    expect(state).toHaveTextContent("Automations are on");
  });

  it("keeps the loading behaviour: no state line until the listing arrives, the link always works", async () => {
    const { closeSettings } = await renderSection(null);
    expect(screen.queryByTestId("settings-automations-state")).not.toBeInTheDocument();

    const dispatched: string[] = [];
    const spy = vi.spyOn(window, "dispatchEvent").mockImplementation((e: Event) => {
      dispatched.push(e.type);
      return true;
    });
    fireEvent.click(screen.getByTestId("settings-open-automations"));
    expect(dispatched).toEqual(["smind:open-automations"]);
    expect(closeSettings).toHaveBeenCalledTimes(1);
    spy.mockRestore();
  });
});
