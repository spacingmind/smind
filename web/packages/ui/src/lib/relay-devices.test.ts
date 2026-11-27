import { describe, expect, it } from "vitest";

import { deviceName, relayDeviceUnavailable, shortDeviceId } from "@/lib/relay-devices";
import { RpcError } from "@/lib/ws-client";

describe("relay device helpers", () => {
  it("shortDeviceId takes the first 8 characters", () => {
    expect(shortDeviceId("abcdef1234567890")).toBe("abcdef12");
    expect(shortDeviceId("short-id")).toBe("short-id");
  });

  it("deviceName falls back to Unnamed device for a blank label", () => {
    expect(deviceName({ label: "  Pixel  " })).toBe("Pixel");
    expect(deviceName({ label: "   " })).toBe("Unnamed device");
    expect(deviceName({ label: "" })).toBe("Unnamed device");
  });

  it("relayDeviceUnavailable classifies forbidden and method-not-found, null otherwise", () => {
    expect(relayDeviceUnavailable(new RpcError("relay.device.list: relay: relay.device.* is not available over the relay", "forbidden"))).toBe("relay");
    expect(relayDeviceUnavailable(new Error("method not found: relay.device.list"))).toBe("no-bridge");
    expect(relayDeviceUnavailable(new Error("relay.device.list: relay: the window is closed"))).toBeNull();
    expect(relayDeviceUnavailable(null)).toBeNull();
  });
});
