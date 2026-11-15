import { describe, expect, it } from "vitest";

import { agentStatus, agentStatusLabel, agentsReady } from "@/lib/readiness";
import type { ProviderInfo } from "@/lib/types";

const P = (id: string): ProviderInfo => ({ id });

describe("agentStatus", () => {
  it("undefined -> checking", () => {
    expect(agentStatus(undefined)).toBe("checking");
  });

  it("ok -> ready", () => {
    expect(agentStatus({ ok: true, detail: "ok" })).toBe("ready");
  });

  it("detail containing 'not found on PATH' -> not-installed", () => {
    expect(agentStatus({ ok: false, detail: '"claude" not found on PATH' })).toBe("not-installed");
  });

  it("any other failure -> needs-sign-in", () => {
    expect(agentStatus({ ok: false, detail: "no stored credentials" })).toBe("needs-sign-in");
  });
});

describe("agentStatusLabel", () => {
  it("labels every status", () => {
    expect(agentStatusLabel("ready")).toBe("Ready");
    expect(agentStatusLabel("not-installed")).toBe("Not installed");
    expect(agentStatusLabel("needs-sign-in")).toBe("Sign-in needed");
    expect(agentStatusLabel("checking")).toBe("Checking");
  });
});

describe("agentsReady", () => {
  it("ready when any provider tested ok", () => {
    expect(
      agentsReady([P("claude-native"), P("glm")], {
        "claude-native": { ok: false, detail: '"claude" not found on PATH' },
        glm: { ok: true, detail: "ok" },
      }),
    ).toBe("ready");
  });

  it("none when every provider was tested and none is ok", () => {
    expect(
      agentsReady([P("claude-native"), P("glm")], {
        "claude-native": { ok: false, detail: '"claude" not found on PATH' },
        glm: { ok: false, detail: "not logged in" },
      }),
    ).toBe("none");
  });

  it("unknown while any provider has no health entry", () => {
    expect(
      agentsReady([P("claude-native"), P("glm")], {
        "claude-native": { ok: false, detail: "not logged in" },
      }),
    ).toBe("unknown");
  });

  it("unknown when a provider's RPC itself failed (never a verdict)", () => {
    expect(agentsReady([P("glm")], { glm: { ok: false, detail: "no" } }, new Set(["claude-native"]))).toBe("none");
    expect(agentsReady([P("claude-native")], {}, new Set(["claude-native"]))).toBe("unknown");
    expect(
      agentsReady([P("claude-native"), P("glm")], { glm: { ok: false, detail: "no" } }, new Set(["claude-native"])),
    ).toBe("unknown");
  });

  it("empty provider list -> unknown", () => {
    expect(agentsReady([], {})).toBe("unknown");
  });
});
