import { act } from "react";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { AutomationsOffCard } from "@/components/automations/automations-off-card";
import { AutomationsPage } from "@/components/automations/automations-page";
import { listSettingsSections } from "@/components/settings/settings-registry";
import "@/components/settings/automations-link-section";
import type { ConfigKeyView } from "@/lib/config-keys";
import { automation, AutomationDaemon } from "@/test/automation-fixtures";
import { renderWithDaemon } from "@/test/render-with-daemon";

function view(key: string, over: Partial<ConfigKeyView> = {}): ConfigKeyView {
  return {
    key,
    value: "false",
    default: "false",
    source: "file",
    restart: false,
    writable: true,
    ...over,
  };
}

/** Wraps a daemon's client so config.get answers a mutable table and config.set applies; every config call is recorded. */
function withConfig<D extends { client: unknown }>(daemon: D, initial: ConfigKeyView[]) {
  let keys = initial;
  const log: { method: string; params: unknown }[] = [];
  const holder = daemon.client as {
    call: (m: string, p?: unknown) => Promise<unknown>;
  };
  const inner = holder.call.bind(holder);
  holder.call = async (method, params) => {
    if (method === "config.get") {
      log.push({ method, params });
      return { keys };
    }
    if (method === "config.set") {
      log.push({ method, params });
      const p = params as { key: string; value: string };
      keys = keys.map((k) => (k.key === p.key ? { ...k, value: p.value } : k));
      return { key: p.key, value: p.value, restart: false };
    }
    return inner(method, params);
  };
  return {
    daemon,
    sets: () => log.filter((c) => c.method === "config.set").map((c) => c.params),
  };
}

async function flush(): Promise<void> {
  await act(async () => {
    for (let i = 0; i < 12; i++) await Promise.resolve();
  });
}

function pageDaemon(listing: Partial<AutomationDaemon["listing"]>, keys: ConfigKeyView[]) {
  const d = new AutomationDaemon();
  d.listing = {
    enabled: true,
    paused: false,
    automations: [automation()],
    ...listing,
  };
  return withConfig(d, keys);
}

async function renderPage(h: ReturnType<typeof pageDaemon>) {
  renderWithDaemon(<AutomationsPage onNavigateBack={() => {}} closeSettings={() => {}} />, h.daemon);
  await flush();
}

const PAUSED_KEYS = [view("automations.enabled", { value: "true" }), view("automations.paused")];

describe("config homes: automations", () => {
  it("automations_settings_group_has_enable_and_pause_switches", async () => {
    const h = withConfig(new AutomationDaemon(), PAUSED_KEYS);
    const section = listSettingsSections().find((s) => s.id === "automations");
    if (!section) throw new Error("automations section did not register");
    renderWithDaemon(
      <>
        {section.render({
          client: null,
          events: null,
          closeSettings: () => {},
        })}
      </>,
      h.daemon,
    );
    await waitFor(() => expect(screen.getByTestId("config-automations-enabled-switch")).toBeInTheDocument());
    const enable = screen.getByTestId("config-automations-enabled-switch");
    const pause = await screen.findByTestId("config-automations-paused-switch");
    const open = screen.getByTestId("settings-open-automations");
    expect(enable).toHaveAttribute("aria-checked", "true");
    expect(pause).toHaveAttribute("aria-checked", "false");
    for (const el of [enable, pause]) {
      expect(el.closest('[data-slot="settings-group"]')).not.toBeNull();
      // The two switches sit above the open-page row.
      expect(el.compareDocumentPosition(open) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    }
  });

  it("automations_page_pause_and_resume", async () => {
    // Running page: Pause sends exactly one config.set, with no confirm.
    const running = pageDaemon({}, PAUSED_KEYS);
    await renderPage(running);
    await waitFor(() => expect(screen.getByTestId("automations-pause")).toBeInTheDocument());
    expect(running.sets()).toEqual([]);
    fireEvent.click(screen.getByTestId("automations-pause"));
    await flush();
    expect(running.sets()).toEqual([{ key: "automations.paused", value: "true" }]);
    expect(screen.queryByRole("alertdialog")).toBeNull();
  });

  it("automations_page_resume_confirms_first", async () => {
    const paused = pageDaemon({ paused: true }, [view("automations.enabled", { value: "true" }), view("automations.paused", { value: "true" })]);
    await renderPage(paused);
    const sw = await screen.findByTestId("config-automations-paused-switch");
    expect(screen.queryByTestId("automations-pause")).toBeNull();
    expect(screen.getByTestId("automations-paused")).toHaveTextContent("Signals are recorded but nothing fires.");
    fireEvent.click(sw);
    await flush();
    const dialog = await screen.findByTestId("config-automations-paused-dialog");
    expect(dialog).toBeInTheDocument();
    expect(paused.sets()).toEqual([]);
    fireEvent.click(screen.getByRole("button", { name: "Resume" }));
    await flush();
    expect(paused.sets()).toEqual([{ key: "automations.paused", value: "false" }]);
  });

  it("automations_page_read_only_shows_neither_pause_nor_resume", async () => {
    const running = pageDaemon({}, [view("automations.enabled", { value: "true", writable: false }), view("automations.paused", { writable: false })]);
    await renderPage(running);
    await flush();
    expect(screen.getByTestId("automations-screen")).toBeInTheDocument();
    expect(screen.queryByTestId("automations-pause")).toBeNull();
    const paused = pageDaemon({ paused: true }, [
      view("automations.enabled", { value: "true", writable: false }),
      view("automations.paused", { value: "true", writable: false }),
    ]);
    renderWithDaemon(<AutomationsPage onNavigateBack={() => {}} closeSettings={() => {}} />, paused.daemon);
    await flush();
    await waitFor(() => expect(screen.getAllByTestId("config-automations-paused-read-only").length).toBe(1));
    expect(screen.queryByTestId("config-automations-paused-switch")).toBeNull();
    expect(screen.queryByTestId("automations-pause")).toBeNull();
  });

  it("automations_off_card_has_the_switch", async () => {
    const writable = withConfig(new AutomationDaemon(), [view("automations.enabled")]);
    const first = renderWithDaemon(<AutomationsOffCard />, writable.daemon);
    expect(await screen.findByTestId("config-automations-enabled-switch")).toBeInTheDocument();
    await flush();
    expect(screen.queryByText("smind config set automations.enabled true")).toBeNull();
    expect(screen.queryByTestId("automations-off-copy")).toBeNull();
    expect(screen.getByTestId("automations-off-card")).not.toHaveTextContent("config.yaml");
    first.unmount();

    const readOnly = withConfig(new AutomationDaemon(), [view("automations.enabled", { writable: false })]);
    renderWithDaemon(<AutomationsOffCard />, readOnly.daemon);
    await waitFor(() => expect(screen.getByTestId("automations-off-copy")).toBeInTheDocument());
    expect(screen.getByTestId("automations-off-card")).toHaveTextContent("smind config set automations.enabled true");
    expect(screen.queryByTestId("config-automations-enabled-switch")).toBeNull();
  });
});
