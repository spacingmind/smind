import { describe, expect, it } from "vitest";

import { describeConnectError } from "@/lib/connect-error";

describe("describeConnectError", () => {
  it("maps a refused or failed fetch to the not-running copy naming the address", () => {
    for (const raw of ["Failed to fetch", "wsclient: failed to connect to ws://127.0.0.1:4648/ws", "Load failed"]) {
      expect(describeConnectError(raw, false, "http://127.0.0.1:4648")).toEqual({
        copy: "The daemon isn't running or isn't reachable at http://127.0.0.1:4648.",
        raw,
      });
    }
  });

  it("omits the address for a non-web origin", () => {
    expect(describeConnectError("Failed to fetch", false, "tauri://localhost")?.copy).toBe("The daemon isn't running or isn't reachable.");
  });

  it("a hung connect says the daemon did not answer within 8 s", () => {
    expect(describeConnectError(null, true)?.copy).toBe("The daemon didn't answer within 8 s.");
  });

  it("quotes any other error and returns null when there is none", () => {
    expect(describeConnectError("/api/token: unexpected status 500", false)?.copy).toBe(
      "The daemon answered with an error: /api/token: unexpected status 500",
    );
    expect(describeConnectError(null, false)).toBeNull();
  });
});
