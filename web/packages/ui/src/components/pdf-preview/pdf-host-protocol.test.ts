import { describe, expect, it } from "vitest";

import { acceptViewerEvent, parseHostMessage, parseViewerMessage } from "./host-protocol";

describe("pdf host protocol (AC20)", () => {
  it("accepts_exactly_the_four_viewer_shapes", () => {
    expect(parseViewerMessage({ type: "ready" })).toEqual({ type: "ready" });
    expect(parseViewerMessage({ type: "pages", count: 3, total: 3 })).toEqual({ type: "pages", count: 3, total: 3 });
    expect(parseViewerMessage({ type: "page", current: 2, count: 3, zoom: 1.5 })).toEqual({ type: "page", current: 2, count: 3, zoom: 1.5 });
    expect(parseViewerMessage({ type: "error", reason: "password" })).toEqual({ type: "error", reason: "password" });
  });

  it("ignores_malformed_and_foreign_messages", () => {
    const bad: unknown[] = [
      null, undefined, 42, "ready", [], [{ type: "ready" }],
      {}, { type: "unknown" }, { type: 1 }, { type: "READY" },
      { type: "pages" }, { type: "pages", count: "3", total: 3 }, { type: "pages", count: 0, total: 0 },
      { type: "pages", count: 2001, total: 2001 }, { type: "pages", count: 3, total: 2 },
      { type: "pages", count: 1.5, total: 2 }, { type: "pages", count: -1, total: 2 },
      { type: "page", current: 0, count: 3, zoom: 1 }, { type: "page", current: 4, count: 3, zoom: 1 },
      { type: "page", current: 1, count: 3, zoom: 0 }, { type: "page", current: 1, count: 3, zoom: NaN },
      { type: "page", current: 1, count: 3, zoom: Infinity }, { type: "page", current: 1, count: 3 },
      { type: "error" }, { type: "error", reason: "boom" }, { type: "error", reason: { toString: () => "password" } },
    ];
    for (const m of bad) expect(parseViewerMessage(m), JSON.stringify(m)).toBeNull();
  });

  it("drops_extra_fields_it_does_not_trust", () => {
    const m = parseViewerMessage({ type: "pages", count: 2, total: 2, path: "/etc/passwd", token: "x" });
    expect(m).toEqual({ type: "pages", count: 2, total: 2 });
    expect(Object.keys(m ?? {})).toEqual(["type", "count", "total"]);
  });

  it("only_the_hosts_own_frame_is_listened_to", () => {
    const frame = {} as Window;
    const other = {} as Window;
    const data = { type: "ready" };
    expect(acceptViewerEvent({ source: frame, data }, frame)).toEqual({ type: "ready" });
    expect(acceptViewerEvent({ source: other, data }, frame)).toBeNull(); // another window, same shape
    expect(acceptViewerEvent({ source: null, data }, frame)).toBeNull();
    expect(acceptViewerEvent({ source: frame, data }, null)).toBeNull(); // no frame mounted
    expect(acceptViewerEvent({ source: frame, data: { type: "nope" } }, frame)).toBeNull();
  });

  it("host_messages_carry_bytes_and_view_commands_only", () => {
    const buf = new ArrayBuffer(4);
    expect(parseHostMessage({ type: "load", data: buf })).toEqual({ type: "load", data: buf });
    expect(parseHostMessage({ type: "load", data: new Uint8Array(4) })).toBeNull(); // must be a transferred ArrayBuffer
    expect(parseHostMessage({ type: "load", data: "x" })).toBeNull();
    expect(parseHostMessage({ type: "zoom", mode: "fit" })).toEqual({ type: "zoom", mode: "fit" });
    expect(parseHostMessage({ type: "zoom", mode: "set", value: 2 })).toEqual({ type: "zoom", mode: "set", value: 2 });
    expect(parseHostMessage({ type: "zoom", mode: "set" })).toBeNull();
    expect(parseHostMessage({ type: "zoom", mode: "evil", value: 1 })).toBeNull();
    expect(parseHostMessage({ type: "goto", page: "next" })).toEqual({ type: "goto", page: "next" });
    expect(parseHostMessage({ type: "goto", page: 5 })).toEqual({ type: "goto", page: 5 });
    expect(parseHostMessage({ type: "goto", page: 0 })).toBeNull();
    expect(parseHostMessage({ type: "goto", page: "../../x" })).toBeNull();
    expect(parseHostMessage({ type: "navigate", url: "https://x" })).toBeNull();
  });
});
