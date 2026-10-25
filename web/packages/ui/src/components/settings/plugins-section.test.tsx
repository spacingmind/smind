import { act } from "react";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { listSettingsSections } from "@/components/settings/settings-registry";
import "@/components/settings/plugins-section";
import { takePluginFocus, setPluginFocus } from "@/lib/plugin-focus";
import { renderWithDaemon } from "@/test/render-with-daemon";
import { consent, forbidden, plugin, PluginDaemon } from "@/test/plugin-fixtures";
import { RpcError } from "@/lib/ws-client";

async function flush(): Promise<void> {
  await act(async () => {
    for (let i = 0; i < 6; i++) await Promise.resolve();
  });
}

async function click(el: HTMLElement): Promise<void> {
  fireEvent.click(el);
  await flush();
}

async function type(el: HTMLElement, value: string): Promise<void> {
  fireEvent.change(el, { target: { value } });
  await flush();
}

async function openMenuItem(menuId: string, itemId: string): Promise<void> {
  fireEvent.pointerDown(screen.getByTestId(menuId), { button: 0 });
  fireEvent.click(await screen.findByTestId(itemId));
  await flush();
}

async function renderSection(daemon: PluginDaemon) {
  const section = listSettingsSections().find((s) => s.id === "plugins");
  if (!section) throw new Error("plugins section did not register");
  const view = renderWithDaemon(<>{section.render({ client: daemon.client, events: daemon.events })}</>, daemon);
  await flush();
  return view;
}

const DIR = plugin({ id: "acme", enabled: true, active: true, consented: true });
const LINK = plugin({
  id: "linked",
  name: "Linked",
  consent: { id: "linked", name: "Linked", sourceKind: "link", source: "/dev/linked", linkWarning: "live directory: changes after consent are not re-reviewed", fingerprint: "fp-linked" },
});

function daemonWith(plugins = [DIR], globalEnabled = true): PluginDaemon {
  const d = new PluginDaemon();
  d.listing = { globalEnabled, plugins };
  return d;
}

describe("settings-plugins section", () => {
  afterEach(() => {
    takePluginFocus();
  });

  it("registers as Settings -> Plugins", () => {
    expect(listSettingsSections().find((s) => s.id === "plugins")?.label).toBe("Plugins");
  });

  it("off state: says plugins are off, how to turn them on, and has no control for the switch", async () => {
    await renderSection(daemonWith([DIR], false));
    const off = screen.getByTestId("plugins-off");
    expect(off).toHaveTextContent("Plugins are off");
    expect(off).toHaveTextContent("plugins.enabled");
    expect(off).toHaveTextContent("config.yaml");
    // The page has NO control that flips the global switch: no switch/checkbox, and nothing to "turn on".
    expect(screen.queryByRole("switch")).toBeNull();
    expect(screen.queryAllByRole("checkbox")).toHaveLength(0);
    expect(screen.queryByRole("button", { name: /turn on|enable plugins|plugins on/i })).toBeNull();
    // Everything else is read-only: install and the row's actions are disabled.
    expect(screen.getByTestId("plugin-install-button")).toBeDisabled();
    expect(screen.getByTestId("plugin-disable-acme")).toBeDisabled();
    expect(screen.getByTestId("plugin-status-acme")).toHaveTextContent("Enabled (plugins off)");
  });

  it("empty state names the install command", async () => {
    await renderSection(daemonWith([]));
    expect(screen.getByTestId("plugins-empty-state")).toHaveTextContent("No plugins installed");
    expect(screen.getByTestId("plugins-empty-state")).toHaveTextContent("smind plugin install <path>");
  });

  it("lists plugins with name, id, version, source kind and every status", async () => {
    const pending = plugin({ id: "pend", name: "Pend", enabled: true, active: true, pending: { consent: consent({ id: "pend", version: "2.0.0", fingerprint: "fp2" }) } });
    const broken = plugin({ id: "broken", name: "Broken", lastError: "bad_manifest: smind-plugin.json: id is required" });
    await renderSection(daemonWith([DIR, LINK, pending, broken]));
    expect(screen.getByTestId("plugin-row-acme")).toHaveTextContent("Acme");
    expect(screen.getByTestId("plugin-version-acme")).toHaveTextContent("1.0.0");
    expect(screen.getByTestId("plugin-source-acme")).toHaveTextContent("dir");
    expect(screen.getByTestId("plugin-source-linked")).toHaveTextContent("link");
    expect(screen.getByTestId("plugin-status-acme")).toHaveTextContent("Enabled");
    expect(screen.getByTestId("plugin-status-linked")).toHaveTextContent("Disabled");
    expect(screen.getByTestId("plugin-pending-pend")).toHaveTextContent("Pending update");
    expect(screen.getByTestId("plugin-error-chip-broken")).toHaveTextContent("Error");
    expect(screen.getByTestId("plugin-error-broken")).toHaveTextContent("bad_manifest");
    expect(screen.getByTestId("plugin-link-warning-linked")).toHaveTextContent("Live directory: changes after consent are not re-reviewed");
  });

  it("detail shows the consent summary with escalating agents flagged, contributions with owner badges, and the logs note", async () => {
    const p = plugin({
      id: "esc",
      name: "Esc",
      enabled: true,
      active: true,
      lastError: "reload failed",
      conflicts: [{ plugin: "esc", kind: "mcpServer", name: "docs", reason: "a user row with that name exists" }],
      consent: {
        id: "esc",
        name: "Esc",
        dataOnly: false,
        process: undefined,
        capabilities: ["events.observe"],
        hookEvents: [{ event: "run.done", onError: "open" }],
        mcpServers: [{ name: "docs", transport: "stdio", command: ["npx", "-y", "docs-mcp"] }],
        profiles: [
          { name: "Calm", provider: "claude-native" },
          { name: "Yolo", provider: "claude-native", permissionMode: "bypassPermissions", escalations: ['permission mode "bypassPermissions" skips permission prompts'] },
        ],
        sourceKind: "git",
        source: "git+https://example.com/esc.git@" + "a".repeat(40),
        hash: "deadbeef",
      },
    });
    await renderSection(daemonWith([p]));
    fireEvent.click(screen.getByTestId("plugin-toggle-esc"));
    const detail = screen.getByTestId("plugin-detail-esc");
    expect(within(detail).getByTestId("plugin-consent-esc-runs-code")).toBeInTheDocument();
    expect(within(detail).getByTestId("plugin-consent-esc-capabilities")).toHaveTextContent("events.observe");
    expect(within(detail).getByTestId("plugin-consent-esc-mcp")).toHaveTextContent("npx -y docs-mcp");
    expect(within(detail).getByTestId("plugin-consent-esc-hooks")).toHaveTextContent("run.done");
    expect(within(detail).getByTestId("plugin-consent-esc-profile-Yolo")).toHaveAttribute("data-escalating", "true");
    expect(within(detail).getByTestId("plugin-consent-esc-escalation-Yolo")).toBeInTheDocument();
    expect(within(detail).getByTestId("plugin-consent-esc-profile-Calm")).toHaveAttribute("data-escalating", "false");
    expect(within(detail).queryByTestId("plugin-consent-esc-escalation-Calm")).toBeNull();
    expect(within(detail).getByText("deadbeef")).toBeInTheDocument();
    // contributions carry the owner badge; conflicts and the last error are shown; no invented process log
    expect(within(within(detail).getByTestId("plugin-contributions-esc")).getAllByText("from esc").length).toBe(4);
    expect(within(detail).getByTestId("plugin-conflicts-esc")).toHaveTextContent("a user row with that name exists");
    expect(within(detail).getByTestId("plugin-last-error-esc")).toHaveTextContent("reload failed");
    expect(within(detail).getByTestId("plugin-logs-esc")).toHaveTextContent("Process logs arrive with plugin processes.");
  });

  it("enable sends plugin.enable with the displayed fingerprint exactly once", async () => {
    const d = daemonWith([LINK]);
    await renderSection(d);
    await click(screen.getByTestId("plugin-enable-linked"));
    const dialog = await screen.findByTestId("plugin-enable-dialog");
    expect(d.countCalls("plugin.enable")).toBe(0);
    fireEvent.click(within(dialog).getByTestId("plugin-enable-dialog-confirm"));
    fireEvent.click(within(dialog).getByTestId("plugin-enable-dialog-confirm"));
    await flush();
    const enables = d.calls.filter((c) => c.method === "plugin.enable");
    expect(enables).toHaveLength(1);
    expect(enables[0]!.params).toEqual({ id: "linked", confirm: "fp-linked" });
    await waitFor(() => expect(screen.getByTestId("plugin-status-linked")).toHaveTextContent("Enabled"));
    expect(screen.queryByTestId("plugin-enable-dialog")).toBeNull();
  });

  it("the link warning is shown in the enable confirmation", async () => {
    await renderSection(daemonWith([LINK]));
    await click(screen.getByTestId("plugin-enable-linked"));
    const warning = await screen.findByTestId("plugin-enable-consent-link-warning");
    expect(warning).toHaveTextContent("Live directory: changes after consent are not re-reviewed");
  });

  it("an unlinked plugin's confirmation has no link warning", async () => {
    await renderSection(daemonWith([plugin({ id: "quiet" })]));
    await click(screen.getByTestId("plugin-enable-quiet"));
    await screen.findByTestId("plugin-enable-consent");
    expect(screen.queryByTestId("plugin-enable-consent-link-warning")).toBeNull();
  });

  it("disable calls plugin.disable and the row turns Disabled in place", async () => {
    const d = daemonWith([DIR]);
    await renderSection(d);
    await click(screen.getByTestId("plugin-disable-acme"));
    await flush();
    expect(d.calls.find((c) => c.method === "plugin.disable")?.params).toEqual({ id: "acme" });
    expect(screen.getByTestId("plugin-status-acme")).toHaveTextContent("Disabled");
  });

  it("remove sends keepData false by default and true when checked", async () => {
    const d = daemonWith([DIR, plugin({ id: "other", name: "Other" })]);
    await renderSection(d);
    await openMenuItem("plugin-menu-acme", "plugin-remove-acme");
    const dialog = await screen.findByTestId("plugin-remove-dialog");
    expect(within(dialog).getByTestId("plugin-remove-keep-data")).not.toBeChecked();
    await click(within(dialog).getByTestId("plugin-remove-dialog-confirm"));
    await flush();
    expect(d.calls.find((c) => c.method === "plugin.remove")?.params).toEqual({ id: "acme", keepData: false });
    await waitFor(() => expect(screen.queryByTestId("plugin-row-acme")).toBeNull());

    await openMenuItem("plugin-menu-other", "plugin-remove-other");
    const second = await screen.findByTestId("plugin-remove-dialog");
    await click(within(second).getByTestId("plugin-remove-keep-data"));
    await click(within(second).getByTestId("plugin-remove-dialog-confirm"));
    await flush();
    expect(d.calls.filter((c) => c.method === "plugin.remove")[1]!.params).toEqual({ id: "other", keepData: true });
  });

  it("a pending update shows the capability/command diff and Apply sends plugin.update apply", async () => {
    const pending = plugin({
      id: "pend",
      name: "Pend",
      enabled: true,
      active: true,
      consent: { id: "pend", name: "Pend", capabilities: [], mcpServers: [{ name: "docs", transport: "stdio", command: ["old"] }], dataOnly: false },
      pending: { consent: consent({ id: "pend", name: "Pend", version: "2.0.0", fingerprint: "fp2", dataOnly: false, capabilities: ["events.observe"], mcpServers: [{ name: "docs", transport: "stdio", command: ["new", "--x"] }] }) },
    });
    const d = daemonWith([pending]);
    await renderSection(d);
    fireEvent.click(screen.getByTestId("plugin-toggle-pend"));
    const changes = screen.getByTestId("plugin-pending-changes-pend");
    expect(changes).toHaveTextContent("1.0.0 → 2.0.0");
    expect(changes).toHaveTextContent("events.observe");
    expect(changes).toHaveTextContent("docs: stdio old → stdio new --x");
    await click(screen.getByTestId("plugin-apply-pend"));
    const dialog = await screen.findByTestId("plugin-apply-dialog");
    expect(dialog).toHaveTextContent("1.0.0 → 2.0.0");
    await click(within(dialog).getByTestId("plugin-apply-dialog-confirm"));
    await flush();
    expect(d.calls.find((c) => c.method === "plugin.update")?.params).toEqual({ id: "pend", source: "", apply: true });
  });

  it("the install form calls plugin.install, then shows the new plugin disabled", async () => {
    const d = daemonWith([]);
    const fresh = plugin({ id: "fresh", name: "Fresh" });
    d.onMutation["plugin.install"] = () => fresh;
    await renderSection(d);
    await click(screen.getByTestId("plugin-install-button"));
    const input = screen.getByTestId("plugin-install-source");
    expect(input).toHaveAttribute("placeholder", "path, git+https://…@<sha> or archive#sha256=<hash>");
    await type(input, "/tmp/fresh");
    await click(screen.getByTestId("plugin-install-submit"));
    await flush();
    expect(d.calls.find((c) => c.method === "plugin.install")?.params).toEqual({ source: "/tmp/fresh" });
    expect(d.countCalls("plugin.enable")).toBe(0);
    expect(screen.getByTestId("plugin-status-fresh")).toHaveTextContent("Disabled");
    expect(screen.getByTestId("plugins-notice")).toHaveTextContent("disabled until you enable it");
    expect(screen.getByTestId("plugin-detail-fresh")).toBeInTheDocument();
  });

  it("an install error shows the daemon's typed message inline", async () => {
    const d = daemonWith([]);
    d.refuse = new RpcError("plugin.install: unpinned_ref: git source must be pinned to a 40-hex commit", "unpinned_ref");
    await renderSection(d);
    await click(screen.getByTestId("plugin-install-button"));
    await type(screen.getByTestId("plugin-install-source"), "git+https://x/y.git@main");
    await click(screen.getByTestId("plugin-install-submit"));
    await flush();
    expect(screen.getByTestId("plugin-install-error")).toHaveTextContent("unpinned_ref");
    // an ordinary error is not a refusal: the page is not locked
    expect(screen.queryByTestId("plugins-read-only")).toBeNull();
  });

  it("forbidden is shown inline with the daemon's message (naming the setting), never retried, and the page goes read-only", async () => {
    const msg = "plugin.enable needs a local connection: set plugins.allowRemoteManage: true in config.yaml to manage plugins from a remote connection";
    const d = daemonWith([LINK, plugin({ id: "two", name: "Two" })]);
    d.refuse = forbidden(msg);
    await renderSection(d);
    await click(screen.getByTestId("plugin-enable-linked"));
    const dialog = await screen.findByTestId("plugin-enable-dialog");
    await click(within(dialog).getByTestId("plugin-enable-dialog-confirm"));
    await flush();
    expect(within(dialog).getByTestId("plugin-enable-dialog-error")).toHaveTextContent("plugins.allowRemoteManage");
    expect(screen.getByTestId("plugins-read-only")).toHaveTextContent("plugins.allowRemoteManage");
    expect(d.countCalls("plugin.enable")).toBe(1);
    // no refetch storm either: the list was read once
    expect(d.countCalls("plugin.list")).toBe(1);
    await click(within(dialog).getByTestId("plugin-enable-dialog-cancel"));
    await waitFor(() => expect(screen.queryByTestId("plugin-enable-dialog")).toBeNull());
    expect(screen.getByTestId("plugin-install-button")).toBeDisabled();
    expect(screen.getByTestId("plugin-enable-two")).toBeDisabled();
    expect(screen.getByTestId("plugin-enable-linked")).toBeDisabled();
  });

  it("relay session: the first refusal turns the whole page read-only with the explanation", async () => {
    const d = daemonWith([DIR]);
    d.refuse = forbidden("plugin.disable is only available on a direct connection, not over the relay");
    await renderSection(d);
    await click(screen.getByTestId("plugin-disable-acme"));
    await flush();
    expect(screen.getByTestId("plugins-read-only")).toHaveTextContent("not over the relay");
    expect(screen.getByTestId("plugin-row-error-acme")).toHaveTextContent("not over the relay");
    expect(screen.getByTestId("plugin-disable-acme")).toBeDisabled();
    expect(screen.getByTestId("plugin-install-button")).toBeDisabled();
  });

  it("live: plugin.updated refetches and rows change in place (same element, status chip changes)", async () => {
    const d = daemonWith([plugin({ id: "acme" }), plugin({ id: "zed", name: "Zed" })]);
    await renderSection(d);
    const row = screen.getByTestId("plugin-row-acme");
    const zed = screen.getByTestId("plugin-row-zed");
    expect(screen.getByTestId("plugin-status-acme")).toHaveTextContent("Disabled");
    d.listing = { globalEnabled: true, plugins: [plugin({ id: "acme", enabled: true, active: true, consented: true }), plugin({ id: "zed", name: "Zed" })] };
    act(() => d.emit("plugin.updated", { id: "acme", globalEnabled: true }));
    await flush();
    await waitFor(() => expect(screen.getByTestId("plugin-status-acme")).toHaveTextContent("Enabled"));
    expect(screen.getByTestId("plugin-row-acme")).toBe(row);
    expect(screen.getByTestId("plugin-row-zed")).toBe(zed);
    expect(d.countCalls("plugin.list")).toBe(2);
  });

  it("live: a flip of the global switch shows or hides the off state without a reload", async () => {
    const d = daemonWith([DIR], true);
    await renderSection(d);
    expect(screen.queryByTestId("plugins-off")).toBeNull();
    d.listing = { globalEnabled: false, plugins: [DIR] };
    act(() => d.emit("plugin.updated", { id: "", globalEnabled: false }));
    await waitFor(() => expect(screen.getByTestId("plugins-off")).toBeInTheDocument());
  });

  it("a plugin focus request (from an owner badge) expands that plugin's detail", async () => {
    setPluginFocus("acme");
    await renderSection(daemonWith([DIR]));
    expect(screen.getByTestId("plugin-detail-acme")).toBeInTheDocument();
  });
});
