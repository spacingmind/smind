import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ConfigSwitchRow } from "@/components/settings/config-switch-row";
import { CONFIG_CONFIRM, type ConfigKeyView, type WritableConfigKey } from "@/lib/config-keys";
import { renderWithDaemon } from "@/test/render-with-daemon";
import { FakeUsageDaemon } from "@/test/usage-test-client";

function view(key: string, over: Partial<ConfigKeyView> = {}): ConfigKeyView {
  return { key, value: "false", default: "false", source: "file", restart: false, writable: true, ...over };
}

/** A daemon whose config.get is scripted from a mutable table and whose config.set is a hook; every call is recorded. */
function daemonWith(initial: ConfigKeyView[], onSet?: (params: { key: string; value: string }) => void) {
  const daemon = new FakeUsageDaemon([]);
  let keys = initial;
  const base = daemon.client.call.bind(daemon.client);
  const log: [string, unknown][] = [];
  (daemon.client as { call: unknown }).call = async (method: string, params?: unknown) => {
    if (method === "config.get") {
      log.push([method, params]);
      if (keys.length === 0) throw new Error("config.get: daemon is restarting");
      return { keys };
    }
    if (method === "config.set") {
      log.push([method, params]);
      const p = params as { key: string; value: string };
      onSet?.(p);
      keys = keys.map((k) => (k.key === p.key ? { ...k, value: p.value } : k));
      return { key: p.key, value: p.value, restart: false };
    }
    return base(method, params);
  };
  return {
    daemon,
    log,
    sets: () => log.filter((c) => c[0] === "config.set"),
    setKeys: (next: ConfigKeyView[]) => (keys = next),
  };
}

function mount(d: ReturnType<typeof daemonWith>, key: WritableConfigKey, testId = "row") {
  return renderWithDaemon(<ConfigSwitchRow configKey={key} label="Label" description="Described." testId={testId} />, d.daemon);
}

async function ready(testId = "row") {
  await waitFor(() => expect(screen.getByTestId(`${testId}-switch`)).toBeInTheDocument());
}

afterEach(() => {
  delete process.env.SMIND_LEARNING;
});

describe("ConfigSwitchRow", () => {
  it("config_switch_shown_only_when_writable", async () => {
    const d = daemonWith([view("plugins.enabled", { writable: true })]);
    mount(d, "plugins.enabled");
    await ready();
    expect(screen.getByTestId("row-switch")).toHaveAttribute("aria-checked", "false");
    expect(screen.queryByTestId("row-read-only")).toBeNull();
  });

  it("config_read_only_copy_when_not_writable", async () => {
    const d = daemonWith([view("plugins.enabled", { writable: false, value: "true" })]);
    mount(d, "plugins.enabled");
    await waitFor(() => expect(screen.getByTestId("row-read-only")).toBeInTheDocument());
    expect(screen.getByTestId("row-read-only")).toHaveTextContent("Change this on the daemon's machine.");
    expect(screen.getByTestId("row-state")).toHaveTextContent("On");
    expect(screen.queryByTestId("row-switch")).toBeNull();
  });

  it("config_no_call_attempted_from_a_read_only_row", async () => {
    const d = daemonWith([view("automations.paused", { writable: false, value: "true" })]);
    mount(d, "automations.paused");
    await waitFor(() => expect(screen.getByTestId("row-state")).toHaveTextContent("Paused"));
    for (const id of ["row-state", "row-read-only"]) fireEvent.click(screen.getByTestId(id));
    expect(screen.queryByRole("switch")).toBeNull();
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(d.log.map((c) => c[0])).toEqual(["config.get"]);
  });

  it("config_env_overridden_switch_disabled_without_the_value", async () => {
    process.env.SMIND_LEARNING = "hunter2-value-xyz";
    const d = daemonWith([view("learning.enabled", { writable: false, value: "false", source: "env", env: "SMIND_LEARNING", restart: true })]);
    const { container } = mount(d, "learning.enabled");
    await ready();
    expect(screen.getByTestId("row-switch")).toBeDisabled();
    expect(screen.getByTestId("row-env")).toHaveTextContent("SMIND_LEARNING");
    expect(container.textContent).not.toContain("hunter2-value-xyz");
    expect(document.body.textContent).not.toContain("hunter2-value-xyz");
    fireEvent.click(screen.getByTestId("row-switch"));
    expect(d.sets()).toHaveLength(0);
  });

  it("config_confirm_before_set_no_call_before_the_confirm", async () => {
    const d = daemonWith([view("plugins.enabled")]);
    mount(d, "plugins.enabled");
    await ready();
    fireEvent.click(screen.getByTestId("row-switch"));
    const dialog = await screen.findByTestId("row-dialog");
    expect(dialog).toHaveTextContent(CONFIG_CONFIRM["plugins.enabled"]);
    expect(d.sets()).toHaveLength(0);
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.queryByTestId("row-dialog")).toBeNull());
    expect(d.sets()).toHaveLength(0);
    fireEvent.click(screen.getByTestId("row-switch"));
    fireEvent.click(within(await screen.findByTestId("row-dialog")).getByRole("button", { name: "Turn on" }));
    await waitFor(() => expect(d.sets()).toHaveLength(1));
    await waitFor(() => expect(screen.getByTestId("row-switch")).toHaveAttribute("aria-checked", "true"));
    expect(d.sets()).toHaveLength(1);
  });

  it("config_set_sends_exactly_key_and_value", async () => {
    const d = daemonWith([view("plugins.enabled")]);
    mount(d, "plugins.enabled");
    await ready();
    fireEvent.click(screen.getByTestId("row-switch"));
    fireEvent.click(within(await screen.findByTestId("row-dialog")).getByRole("button", { name: "Turn on" }));
    await waitFor(() => expect(d.sets()).toHaveLength(1));
    expect(d.sets()[0]).toEqual(["config.set", { key: "plugins.enabled", value: "true" }]);
  });

  it("config_turning_off_and_pausing_need_no_confirm", async () => {
    const off = daemonWith([view("automations.enabled", { value: "true" })]);
    const a = mount(off, "automations.enabled");
    await ready();
    fireEvent.click(screen.getByTestId("row-switch"));
    await waitFor(() => expect(off.sets()).toEqual([["config.set", { key: "automations.enabled", value: "false" }]]));
    expect(screen.queryByTestId("row-dialog")).toBeNull();
    a.unmount();

    const pause = daemonWith([view("automations.paused", { value: "false" })]);
    mount(pause, "automations.paused");
    await ready();
    fireEvent.click(screen.getByTestId("row-switch"));
    await waitFor(() => expect(pause.sets()).toEqual([["config.set", { key: "automations.paused", value: "true" }]]));
    expect(screen.queryByTestId("row-dialog")).toBeNull();
    await waitFor(() => expect(screen.getByTestId("row-switch")).toHaveAttribute("aria-checked", "true"));

    // Resuming is the risky direction: it asks first.
    fireEvent.click(screen.getByTestId("row-switch"));
    const dialog = await screen.findByTestId("row-dialog");
    expect(pause.sets()).toHaveLength(1);
    expect(within(dialog).getByRole("button", { name: "Resume" })).toBeInTheDocument();
  });

  it("config_restart_notice_for_fleet_and_learning", async () => {
    const all: [WritableConfigKey, boolean][] = [
      ["fleet.enabled", true],
      ["learning.enabled", true],
      ["plugins.enabled", false],
      ["automations.enabled", false],
    ];
    for (const [key, restart] of all) {
      const d = daemonWith([view(key, { restart })]);
      const r = mount(d, key);
      await ready();
      if (restart) expect(screen.getByTestId("row-restart")).toHaveTextContent("Takes effect after the daemon restarts.");
      else expect(screen.queryByTestId("row-restart")).toBeNull();
      r.unmount();
    }
  });

  it("config_refusal_shown_as_sentence_and_switch_returns", async () => {
    const d = daemonWith([view("learning.enabled", { value: "false", restart: true })], () => {
      // The daemon refuses, and its real state is an env override.
      d.setKeys([view("learning.enabled", { value: "false", writable: false, source: "env", env: "SMIND_LEARNING", restart: true })]);
      throw new Error("config.set: overridden by SMIND_LEARNING, unset it first");
    });
    mount(d, "learning.enabled");
    await ready();
    fireEvent.click(screen.getByTestId("row-switch"));
    fireEvent.click(within(await screen.findByTestId("row-dialog")).getByRole("button", { name: "Turn on" }));
    const dialog = screen.getByTestId("row-dialog");
    await waitFor(() => expect(dialog).toHaveTextContent("Overridden by SMIND_LEARNING, unset it first."));
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.queryByTestId("row-dialog")).toBeNull());
    await waitFor(() => expect(screen.getByTestId("row-switch")).toBeDisabled());
    expect(screen.getByTestId("row-switch")).toHaveAttribute("aria-checked", "false");
    expect(screen.getByTestId("row-env")).toHaveTextContent("SMIND_LEARNING");
    expect(d.log.filter((c) => c[0] === "config.get").length).toBeGreaterThanOrEqual(2);
  });

  it("config_refusal_without_a_dialog_shows_an_alert", async () => {
    const d = daemonWith([view("automations.enabled", { value: "true" })], () => {
      throw new Error("config.set: not allowed over relay");
    });
    mount(d, "automations.enabled");
    await ready();
    fireEvent.click(screen.getByTestId("row-switch"));
    await waitFor(() => expect(screen.getByTestId("row-error")).toHaveTextContent("Not allowed over relay."));
    expect(screen.getByTestId("row-switch")).toHaveAttribute("aria-checked", "true");
  });

  it("config_saved_restart_notice_opens_daemon_settings", async () => {
    const d = daemonWith([view("fleet.enabled", { restart: true })]);
    mount(d, "fleet.enabled");
    await ready();
    fireEvent.click(screen.getByTestId("row-switch"));
    fireEvent.click(within(await screen.findByTestId("row-dialog")).getByRole("button", { name: "Turn on" }));
    await waitFor(() => expect(screen.getByTestId("row-restart")).toHaveTextContent("Saved. Takes effect after the daemon restarts."));
    const seen = vi.fn();
    window.addEventListener("smind:open-settings", seen);
    fireEvent.click(screen.getByTestId("row-open-daemon"));
    window.removeEventListener("smind:open-settings", seen);
    expect((seen.mock.calls[0]![0] as CustomEvent).detail).toEqual({ sectionId: "daemon" });
  });

  it("config_get_error_shows_a_read_only_sentence", async () => {
    const d = daemonWith([]);
    mount(d, "plugins.enabled");
    await waitFor(() => expect(screen.getByTestId("row-load-error")).toHaveTextContent("Could not read this setting: Daemon is restarting."));
    expect(screen.queryByTestId("row-switch")).toBeNull();
    expect(d.sets()).toHaveLength(0);
  });
});
