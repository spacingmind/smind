import { describe, expect, it } from "vitest";

import type { RunConfigState } from "@/components/composer/run-config-toolbar";
import { profileIdFor } from "@/lib/run-profile";
import type { AgentProfile } from "@/lib/types";

function profile(over: Partial<AgentProfile> = {}): AgentProfile {
  return {
    ID: 7,
    Name: "Supervisor",
    Provider: "claude-native",
    PermissionMode: "plan",
    AutoAccept: false,
    ThinkingLevel: "standard",
    Model: "",
    Notes: "",
    CreatedAt: "",
    UpdatedAt: "",
    ...over,
  };
}

function state(over: Partial<RunConfigState> = {}): RunConfigState {
  return {
    baseAgentId: "7",
    custom: false,
    provider: "claude-native",
    permissionMode: "plan",
    autoAccept: false,
    thinkingLevel: "standard",
    model: "",
    ...over,
  };
}

describe("profileIdFor", () => {
  it("no base agent sends no profileId", () => {
    expect(profileIdFor(state({ baseAgentId: null }), [profile()])).toBeUndefined();
  });

  it("applied profile with unchanged fields sends its id", () => {
    expect(profileIdFor(state(), [profile()])).toBe(7);
  });

  it("a hand-edited model drops it", () => {
    expect(profileIdFor(state({ model: "haiku" }), [profile()])).toBeUndefined();
  });

  it("a hand-edited mode drops it", () => {
    expect(profileIdFor(state({ permissionMode: "default" }), [profile()])).toBeUndefined();
  });

  it("a different provider drops it", () => {
    expect(profileIdFor(state({ provider: "glm" }), [profile()])).toBeUndefined();
  });

  it("auto-accept differing drops it", () => {
    expect(profileIdFor(state({ autoAccept: true }), [profile()])).toBeUndefined();
  });

  it("deleted profile drops it", () => {
    expect(profileIdFor(state(), [profile({ ID: 8 })])).toBeUndefined();
  });

  it("empty thinking level in the profile is ignored", () => {
    expect(profileIdFor(state({ thinkingLevel: "extended" }), [profile({ ThinkingLevel: "" })])).toBe(7);
  });

  it("a profile with a model and the same model sends it", () => {
    expect(profileIdFor(state({ model: "opus" }), [profile({ Model: "opus" })])).toBe(7);
  });
});
