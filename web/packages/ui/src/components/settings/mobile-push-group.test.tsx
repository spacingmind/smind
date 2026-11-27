import { act } from "react";
import { cleanup, fireEvent, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { MobilePushGroup } from "@/components/settings/mobile-push-group";
import type { PushTestResult } from "@/hooks/use-push-test";
import type { RelayDevice } from "@/lib/relay-devices";
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
const TOKEN = "ExponentPushToken[SECRET-NEVER-RENDER-0123456789]";

function phone(over: Partial<RelayDevice> = {}): RelayDevice {
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

/** Pixel (push on), iPhone (push off), Old tablet (unknown): one phone to notify. */
function threePhones(): RelayDevice[] {
  return [
    phone({ device_id: "11111111-aaaa", label: "Pixel", push: true }),
    phone({ device_id: "22222222-bbbb", label: "iPhone", push: false }),
    phone({ device_id: "33333333-cccc", label: "Old tablet" }),
  ];
}

/**
 * A FakeUsageDaemon answering the push group's surface: `relay.device.list` from a device list (or a refusal),
 * `push.test` from `testAnswer` (default: no device registered); both calls are recorded.
 */
function pushDaemon(opts: { devices?: RelayDevice[]; listError?: Error; testAnswer?: () => unknown } = {}) {
  const d = new FakeUsageDaemon([]);
  const holder = d.client as unknown as { call: (m: string, p?: unknown) => Promise<unknown> };
  const inner = holder.call.bind(holder);
  holder.call = async (method, params) => {
    if (method !== "relay.device.list" && method !== "push.test") return inner(method, params);
    d.calls.push({ method, params });
    if (method === "relay.device.list") {
      if (opts.listError) throw opts.listError;
      return { devices: opts.devices ?? [], window: null };
    }
    return opts.testAnswer ? opts.testAnswer() : { results: [] };
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
  const view = renderWithDaemon(<MobilePushGroup />, d);
  await flush();
  return view;
}

function callsOf(d: FakeUsageDaemon, method: string): { method: string; params: unknown }[] {
  return d.calls.filter((c) => c.method === method);
}

describe("settings mobile push group", () => {
  it("push_group_counts_registered_devices", async () => {
    const d = pushDaemon({ devices: threePhones() });
    await renderGroup(d);

    expect(screen.getByTestId("push-count")).toHaveTextContent("1 with push on");
    expect(screen.getByTestId("push-count")).not.toHaveTextContent("3");
    expect(callsOf(d, "push.test")).toHaveLength(0);
    expect(screen.queryByTestId("push-results")).toBeNull();
    expect(screen.queryByTestId("push-no-device")).toBeNull();
  });

  it("push_test_with_no_device_is_not_an_error", async () => {
    const d = pushDaemon({ devices: threePhones() });
    await renderGroup(d);

    await click(screen.getByTestId("push-test"));
    expect(callsOf(d, "push.test")).toHaveLength(1);
    expect(screen.getByTestId("push-no-device")).toHaveTextContent("No device has push set up yet. Open sMind on your phone to register one.");
    expect(screen.getByTestId("push-no-device")).not.toHaveAttribute("role", "alert");
    expect(screen.queryByTestId("push-error")).toBeNull();
    expect(screen.queryByTestId("push-results")).toBeNull();
    expect(screen.getByTestId("push-test")).toBeEnabled();
  });

  it("push_test_results_map_codes_to_sentences", async () => {
    const d = pushDaemon({
      devices: threePhones(),
      testAnswer: (): PushTestResult => ({
        results: [
          { deviceId: "11111111-aaaa", sent: true },
          { deviceId: "22222222-bbbb", sent: false, error: "not_registered" },
          { deviceId: "33333333-cccc", sent: false, error: "DeviceNotRegistered" },
          { deviceId: "44444444-dddd", sent: false, error: "UnregisteredTopic" },
        ],
      }),
    });
    await renderGroup(d);
    await click(screen.getByTestId("push-test"));

    // a known device is named by its label; an unknown one by its short id only
    expect(screen.getByTestId("push-result-11111111-aaaa")).toHaveTextContent("Pixel");
    expect(screen.getByTestId("push-result-11111111-aaaa")).toHaveTextContent("11111111");
    expect(screen.getByTestId("push-result-11111111-aaaa")).toHaveTextContent("Sent");
    expect(screen.getByTestId("push-result-22222222-bbbb")).toHaveTextContent("This device has no push registration any more.");
    expect(screen.getByTestId("push-result-33333333-cccc")).toHaveTextContent("The push service no longer knows this device.");
    expect(screen.getByTestId("push-result-44444444-dddd")).toHaveTextContent("The push service refused it (UnregisteredTopic).");
    expect(screen.getByTestId("push-result-44444444-dddd")).toHaveTextContent("44444444");
    expect(screen.getByTestId("push-result-44444444-dddd")).not.toHaveTextContent("Unnamed device");
    expect(screen.queryByTestId("push-no-device")).toBeNull();
    expect(screen.queryByTestId("push-error")).toBeNull();
  });

  it("push_test_shows_no_secrets", async () => {
    const d = pushDaemon({
      devices: threePhones(),
      // the wire carries no token; the fake puts one in an extra field to prove it is never echoed
      testAnswer: () => ({ results: [{ deviceId: "11111111-aaaa", sent: false, error: "send_failed", token: TOKEN }] }),
    });
    await renderGroup(d);
    await click(screen.getByTestId("push-test"));

    expect(screen.getByTestId("push-results")).toBeInTheDocument();
    expect(document.body.textContent).not.toContain(TOKEN);
    expect(document.body.textContent).not.toContain(KEY);
    expect(document.body.textContent).not.toMatch(/token|device_pub/i);
  });

  it("push_test_one_call_per_click", async () => {
    const d = pushDaemon({ devices: threePhones() });
    await renderGroup(d);
    expect(callsOf(d, "push.test")).toHaveLength(0);

    await click(screen.getByTestId("push-test"));
    expect(callsOf(d, "push.test")).toHaveLength(1);

    await click(screen.getByTestId("push-test"));
    expect(callsOf(d, "push.test")).toHaveLength(2);
    expect(callsOf(d, "push.test").map((c) => c.params)).toEqual([{}, {}]);
  });

  it("push_group_relay_connection_is_read_only_and_makes_no_call", async () => {
    platformState.desktop = true;
    platformState.connection = { id: "relay1", kind: "relay", label: "Relay", baseUrl: "wss://relay.example" };
    const d = pushDaemon({ devices: threePhones() });
    await renderGroup(d);
    await flush();

    expect(screen.getByTestId("push-read-only")).toHaveTextContent("Send test notifications from the daemon's machine.");
    expect(screen.queryByTestId("push-test")).toBeNull();
    expect(screen.queryByTestId("push-count")).toBeNull();
    expect(d.calls.filter((c) => c.method === "relay.device.list" || c.method === "push.test")).toHaveLength(0);

    // A `forbidden` answer to the test itself says the same: this connection is a relay session.
    cleanup();
    resetPlatform();
    const refused = pushDaemon({
      devices: threePhones(),
      testAnswer: () => {
        throw new RpcError("push.test: relay: push.* needs a paired relay device", "forbidden");
      },
    });
    await renderGroup(refused);
    await click(screen.getByTestId("push-test"));
    expect(screen.getByTestId("push-read-only")).toHaveTextContent("Send test notifications from the daemon's machine.");
    expect(screen.queryByTestId("push-test")).toBeNull();
    expect(screen.queryByTestId("push-error")).toBeNull();
  });

  it("push_disabled_is_a_sentence", async () => {
    const d = pushDaemon({
      devices: threePhones(),
      testAnswer: () => {
        throw new RpcError("push.test: push: push notifications are disabled on this daemon", "push_disabled");
      },
    });
    await renderGroup(d);
    await click(screen.getByTestId("push-test"));

    const error = screen.getByTestId("push-error");
    expect(error).toHaveTextContent("Push notifications are disabled on this daemon.");
    expect(error.textContent).not.toContain("push.test");
    expect(screen.queryByTestId("push-no-device")).toBeNull();
    expect(screen.queryByTestId("push-results")).toBeNull();
    expect(screen.getByTestId("push-test")).toBeEnabled();
  });
});
