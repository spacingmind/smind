import { act } from "react";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { RelayDevicesGroup } from "@/components/settings/relay-devices-group";
import type { RelayDevice, RelayDeviceWindow } from "@/lib/relay-devices";
import { RpcError } from "@/lib/ws-client";
import { FakeUsageDaemon } from "@/test/usage-test-client";
import { renderWithDaemon } from "@/test/render-with-daemon";

// The desktop build's connection, faked: `desktop` false keeps the group on the web path (ready at once, no
// connection object); the relay test flips it to a desktop app whose current connection is a relay session.
const platformState = vi.hoisted(() => ({
  desktop: false,
  connection: { id: "local", kind: "local", label: "Local", baseUrl: "http://127.0.0.1:4648" } as {
    id: string;
    kind: "local" | "url" | "relay" | "ssh";
    label: string;
    baseUrl: string;
  },
}));

function resetPlatform() {
  platformState.desktop = false;
  platformState.connection = { id: "local", kind: "local", label: "Local", baseUrl: "http://127.0.0.1:4648" };
}

vi.mock("@/lib/platform", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/platform")>();
  return {
    ...actual,
    get isDesktop() {
      return platformState.desktop;
    },
    desktop: { ...actual.desktop, getCurrentConnection: () => Promise.resolve(platformState.connection) },
  };
});

afterEach(() => {
  resetPlatform();
});

const KEY = "PUBKEY-BYTES-NEVER-RENDER-0123456789abcdef";

function device(over: Partial<RelayDevice> = {}): RelayDevice {
  return {
    device_id: "11111111-aaaa",
    device_pub: KEY,
    label: "Pixel",
    first_seen: "2026-11-20T10:00:00Z",
    last_seen: "2026-11-27T09:59:00Z",
    state: "approved",
    connected: false,
    ...over,
  };
}

/** The plan's three devices: pending, approved (push and connected), revoked. */
function threeDevices(): RelayDevice[] {
  return [
    device({ device_id: "11111111-aaaa", label: "Pixel", state: "pending" }),
    device({ device_id: "22222222-bbbb", label: "iPhone", state: "approved", connected: true, push: true }),
    device({ device_id: "33333333-cccc", label: "Old tablet", state: "revoked" }),
  ];
}

/**
 * A FakeUsageDaemon answering the relay.device.* surface: the list answers from a mutable device list and window,
 * approve/revoke/rename apply and answer the device, `window` opens one; every relay call is recorded. `listError`
 * makes the list fail the way a relay session (forbidden) or a bridgeless daemon (method not found) does.
 */
function relayDevicesDaemon(
  opts: { devices?: RelayDevice[]; window?: RelayDeviceWindow | null; listError?: Error } = {},
) {
  const devices = opts.devices ?? [];
  let window: RelayDeviceWindow | null = opts.window ?? null;
  const d = new FakeUsageDaemon([]);
  const holder = d.client as unknown as { call: (m: string, p?: unknown) => Promise<unknown> };
  const inner = holder.call.bind(holder);
  holder.call = async (method, params) => {
    if (!method.startsWith("relay.device.")) return inner(method, params);
    d.calls.push({ method, params });
    if (opts.listError && method === "relay.device.list") throw opts.listError;
    const p = (params ?? {}) as { id?: string; label?: string };
    const patch = (fn: (x: RelayDevice) => RelayDevice): RelayDevice => {
      const i = devices.findIndex((x) => x.device_id === p.id);
      devices[i] = fn(devices[i]!);
      return devices[i]!;
    };
    switch (method) {
      case "relay.device.list":
        return { devices, window };
      case "relay.device.approve":
        return patch((x) => ({ ...x, state: "approved" }));
      case "relay.device.revoke":
        return patch((x) => ({ ...x, state: "revoked", connected: false }));
      case "relay.device.rename":
        return patch((x) => ({ ...x, label: p.label as string }));
      case "relay.device.window":
        window = { expires_at: "2026-11-27T11:10:00Z", remaining: 2 };
        return window;
    }
    return {};
  };
  return d;
}

async function flush(): Promise<void> {
  await act(async () => {
    for (let i = 0; i < 12; i++) await Promise.resolve();
  });
}

async function click(el: HTMLElement): Promise<void> {
  fireEvent.click(el);
  await flush();
}

async function renderGroup(d: FakeUsageDaemon) {
  const view = renderWithDaemon(<RelayDevicesGroup />, d);
  await flush();
  return view;
}

function relayCalls(d: FakeUsageDaemon): { method: string; params: unknown }[] {
  return d.calls.filter((c) => c.method.startsWith("relay.device."));
}

/** Every text node in the tree, for the "no key bytes are ever rendered" assertion. */
function allText(): string {
  return Array.from(document.body.querySelectorAll("*"))
    .map((el) => el.textContent ?? "")
    .join("\n");
}

describe("settings relay devices group", () => {
  it("relay_devices_list_renders_all_states", async () => {
    await renderGroup(relayDevicesDaemon({ devices: threeDevices() }));

    expect(screen.getByTestId("relay-devices-list")).toBeInTheDocument();
    expect(screen.getByTestId("relay-device-11111111-aaaa")).toHaveTextContent("Pixel");
    expect(screen.getByTestId("relay-device-state-11111111-aaaa")).toHaveTextContent("Pending");
    expect(screen.getByTestId("relay-device-state-22222222-bbbb")).toHaveTextContent("Approved");
    expect(screen.getByTestId("relay-device-state-33333333-cccc")).toHaveTextContent("Revoked");

    // short ids, not the full id or any key bytes
    expect(screen.getByTestId("relay-device-22222222-bbbb")).toHaveTextContent("22222222");

    const approved = screen.getByTestId("relay-device-22222222-bbbb");
    expect(approved).toHaveTextContent("Connected now");
    expect(approved).toHaveTextContent("Last seen");
    expect(approved).toHaveTextContent("Mobile push on");
    expect(screen.getByTestId("relay-device-11111111-aaaa")).not.toHaveTextContent("Connected now");
    expect(screen.getByTestId("relay-device-11111111-aaaa")).not.toHaveTextContent("Mobile push");
    expect(screen.getByTestId("relay-device-33333333-cccc")).not.toHaveTextContent("Mobile push");

    // the actions each state offers
    expect(screen.getByTestId("relay-device-approve-11111111-aaaa")).toBeInTheDocument();
    expect(screen.queryByTestId("relay-device-revoke-11111111-aaaa")).toBeNull();
    expect(screen.getByTestId("relay-device-revoke-22222222-bbbb")).toBeInTheDocument();
    expect(screen.queryByTestId("relay-device-approve-22222222-bbbb")).toBeNull();
    expect(screen.getByTestId("relay-device-rename-33333333-cccc")).toBeInTheDocument();
  });

  it("relay_devices_empty_state", async () => {
    await renderGroup(relayDevicesDaemon({ devices: [] }));

    expect(screen.getByTestId("relay-devices-empty")).toHaveTextContent("No devices are paired yet");
    expect(screen.getByTestId("relay-devices-empty")).toHaveTextContent("Open a pairing window, then add this daemon on your phone.");
    expect(screen.queryByTestId("relay-devices-list")).toBeNull();
  });

  it("relay_devices_never_render_key_bytes", async () => {
    await renderGroup(relayDevicesDaemon({ devices: threeDevices() }));

    expect(allText()).not.toContain(KEY);
    expect(allText()).not.toMatch(/device_pub/i);
  });

  it("relay_devices_approve_confirms_then_calls", async () => {
    const d = relayDevicesDaemon({ devices: threeDevices() });
    await renderGroup(d);

    await click(screen.getByTestId("relay-device-approve-11111111-aaaa"));
    const dialog = screen.getByTestId("relay-confirm-dialog");
    expect(within(dialog).getByText("Approve Pixel?")).toBeInTheDocument();
    expect(dialog).toHaveTextContent("Pixel");
    expect(dialog).toHaveTextContent("11111111");
    expect(d.calls.filter((c) => c.method === "relay.device.approve")).toHaveLength(0);

    await click(within(dialog).getByTestId("relay-confirm-dialog-confirm"));
    const approves = d.calls.filter((c) => c.method === "relay.device.approve");
    expect(approves).toHaveLength(1);
    expect(approves[0]!.params).toEqual({ id: "11111111-aaaa" });
    await waitFor(() => expect(screen.queryByTestId("relay-confirm-dialog")).toBeNull());
    await waitFor(() => expect(screen.getByTestId("relay-device-state-11111111-aaaa")).toHaveTextContent("Approved"));
  });

  it("relay_devices_approve_cancel_sends_nothing", async () => {
    const d = relayDevicesDaemon({ devices: threeDevices() });
    await renderGroup(d);

    await click(screen.getByTestId("relay-device-approve-11111111-aaaa"));
    await click(screen.getByTestId("relay-confirm-dialog-cancel"));
    await waitFor(() => expect(screen.queryByTestId("relay-confirm-dialog")).toBeNull());
    expect(d.calls.filter((c) => c.method === "relay.device.approve")).toHaveLength(0);
    expect(screen.getByTestId("relay-device-state-11111111-aaaa")).toHaveTextContent("Pending");
  });

  it("relay_devices_revoke_confirms_then_calls", async () => {
    const d = relayDevicesDaemon({ devices: threeDevices() });
    await renderGroup(d);

    await click(screen.getByTestId("relay-device-revoke-22222222-bbbb"));
    const dialog = screen.getByTestId("relay-confirm-dialog");
    expect(within(dialog).getByText("Revoke iPhone?")).toBeInTheDocument();
    expect(dialog).toHaveTextContent("22222222");
    expect(d.calls.filter((c) => c.method === "relay.device.revoke")).toHaveLength(0);

    await click(within(dialog).getByTestId("relay-confirm-dialog-confirm"));
    const revokes = d.calls.filter((c) => c.method === "relay.device.revoke");
    expect(revokes).toHaveLength(1);
    expect(revokes[0]!.params).toEqual({ id: "22222222-bbbb" });
    await waitFor(() => expect(screen.queryByTestId("relay-confirm-dialog")).toBeNull());
    await waitFor(() => expect(screen.getByTestId("relay-device-state-22222222-bbbb")).toHaveTextContent("Revoked"));
  });

  it("relay_devices_rename_sends_label", async () => {
    const d = relayDevicesDaemon({ devices: threeDevices() });
    await renderGroup(d);

    await click(screen.getByTestId("relay-device-rename-11111111-aaaa"));
    expect(screen.getByTestId("relay-rename-input")).toHaveValue("Pixel");
    fireEvent.change(screen.getByTestId("relay-rename-input"), { target: { value: "  Pixel 8  " } });
    await flush();
    await click(screen.getByTestId("relay-rename-save"));

    const renames = d.calls.filter((c) => c.method === "relay.device.rename");
    expect(renames).toHaveLength(1);
    expect(renames[0]!.params).toEqual({ id: "11111111-aaaa", label: "Pixel 8" });
    await waitFor(() => expect(screen.queryByTestId("relay-rename-dialog")).toBeNull());
    await waitFor(() => expect(screen.getByTestId("relay-device-11111111-aaaa")).toHaveTextContent("Pixel 8"));
  });

  it("relay_devices_rename_empty_label_refused", async () => {
    const d = relayDevicesDaemon({ devices: threeDevices() });
    await renderGroup(d);

    await click(screen.getByTestId("relay-device-rename-11111111-aaaa"));
    fireEvent.change(screen.getByTestId("relay-rename-input"), { target: { value: "   " } });
    await flush();
    await click(screen.getByTestId("relay-rename-save"));

    expect(screen.getByTestId("relay-rename-error")).toHaveTextContent("Give the device a name.");
    expect(d.calls.filter((c) => c.method === "relay.device.rename")).toHaveLength(0);
    expect(screen.getByTestId("relay-rename-dialog")).toBeInTheDocument();
  });

  it("relay_devices_pairing_window_opens_and_shows_remaining", async () => {
    const d = relayDevicesDaemon({ devices: threeDevices() });
    await renderGroup(d);

    expect(screen.queryByTestId("relay-window-state")).toBeNull();
    await click(screen.getByTestId("relay-window-open"));
    const windows = d.calls.filter((c) => c.method === "relay.device.window");
    expect(windows).toHaveLength(1);
    expect(windows[0]!.params).toEqual({});

    await waitFor(() => expect(screen.getByTestId("relay-window-state")).toBeInTheDocument());
    expect(screen.getByTestId("relay-window-state")).toHaveTextContent(
      `Open until ${new Date("2026-11-27T11:10:00Z").toLocaleTimeString()}, 2 device(s) left`,
    );
    expect(screen.queryByTestId("relay-window-open")).toBeNull();
  });

  it("relay_devices_relay_connection_is_read_only_and_makes_no_call", async () => {
    platformState.desktop = true;
    platformState.connection = { id: "relay1", kind: "relay", label: "Relay", baseUrl: "wss://relay.example" };
    const d = relayDevicesDaemon({ devices: threeDevices() });
    await renderGroup(d);
    await flush();

    expect(screen.getByTestId("relay-devices-read-only")).toHaveTextContent("Manage paired devices on the daemon's machine.");
    expect(relayCalls(d)).toHaveLength(0);
    expect(screen.queryByTestId("relay-devices-refresh")).toBeNull();
    expect(screen.queryByTestId("relay-window-open")).toBeNull();
    expect(screen.queryByTestId("relay-device-approve-11111111-aaaa")).toBeNull();
    expect(screen.queryByTestId("relay-device-revoke-22222222-bbbb")).toBeNull();
    expect(screen.queryByTestId("relay-device-rename-33333333-cccc")).toBeNull();
  });

  it("relay_devices_forbidden_answer_becomes_read_only_copy", async () => {
    const d = relayDevicesDaemon({
      devices: threeDevices(),
      listError: new RpcError("relay.device.list: relay: relay.device.* is not available over the relay", "forbidden"),
    });
    await renderGroup(d);

    expect(screen.getByTestId("relay-devices-read-only")).toHaveTextContent("Manage paired devices on the daemon's machine.");
    expect(screen.queryByTestId("relay-devices-refresh")).toBeNull();
    expect(screen.queryByTestId("relay-window-open")).toBeNull();
    expect(screen.queryByTestId("relay-device-rename-33333333-cccc")).toBeNull();
  });

  it("relay_devices_no_bridge_says_relay_not_set_up", async () => {
    const d = relayDevicesDaemon({ devices: threeDevices(), listError: new Error("method not found: relay.device.list") });
    await renderGroup(d);

    expect(screen.getByTestId("relay-devices-no-bridge")).toHaveTextContent("Relay is not set up on this daemon.");
    expect(screen.queryByTestId("relay-devices-read-only")).toBeNull();
    expect(screen.queryByTestId("relay-window-open")).toBeNull();
  });

  it("relay_devices_error_is_a_sentence", async () => {
    const d = relayDevicesDaemon({ devices: threeDevices(), listError: new Error("relay.device.list: relay: the pairing window is closed") });
    await renderGroup(d);

    expect(screen.getByTestId("relay-devices-error")).toHaveTextContent("The pairing window is closed.");
    expect(screen.getByTestId("relay-devices-error")).not.toHaveTextContent("relay.device.list");
  });
});
