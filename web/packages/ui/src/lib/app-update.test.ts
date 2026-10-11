import { describe, expect, it } from "vitest";

import { appUpdateReducer, initialAppUpdateState, type AppUpdateState } from "@/lib/app-update";
import type { AppUpdateCheck } from "@/lib/platform";

const AVAILABLE: AppUpdateCheck = {
  kind: "available",
  version: "0.11.0",
  currentVersion: "0.10.0",
  notes: "- Fixed a bug",
  date: "2026-10-01T00:00:00Z",
};

function promptState(overrides: Partial<Extract<AppUpdateState, { stage: "prompt" }>> = {}): Extract<AppUpdateState, { stage: "prompt" }> {
  return {
    stage: "prompt",
    info: { version: "0.11.0", currentVersion: "0.10.0", notes: "- Fixed a bug", date: "2026-10-01T00:00:00Z" },
    error: null,
    ...overrides,
  };
}

describe("appUpdateReducer", () => {
  it("ignores unsupported and upToDate checks", () => {
    expect(appUpdateReducer(initialAppUpdateState, { type: "checkResult", check: { kind: "unsupported" }, manual: false, dismissed: new Set() })).toEqual({ stage: "idle" });
    expect(appUpdateReducer(initialAppUpdateState, { type: "checkResult", check: { kind: "upToDate", currentVersion: "0.10.0" }, manual: true, dismissed: new Set() })).toEqual({ stage: "idle" });
  });

  it("prompts on available", () => {
    expect(appUpdateReducer(initialAppUpdateState, { type: "checkResult", check: AVAILABLE, manual: false, dismissed: new Set() })).toEqual(promptState());
  });

  it("a version dismissed this session is not re-prompted by the launch check, but a manual check re-prompts", () => {
    const dismissed = new Set(["0.11.0"]);
    expect(appUpdateReducer(initialAppUpdateState, { type: "checkResult", check: AVAILABLE, manual: false, dismissed })).toEqual({ stage: "idle" });
    expect(appUpdateReducer(initialAppUpdateState, { type: "checkResult", check: AVAILABLE, manual: true, dismissed })).toEqual(promptState());
  });

  it("confirm moves to downloading and nothing before that", () => {
    expect(appUpdateReducer(initialAppUpdateState, { type: "confirm" })).toEqual(initialAppUpdateState);
    expect(appUpdateReducer(promptState(), { type: "confirm" })).toEqual({
      stage: "downloading",
      info: promptState().info,
      progress: { stage: "downloading", downloaded: 0, total: null },
    });
  });

  it("progress events update the download and installing arrives last", () => {
    const downloading = appUpdateReducer(promptState(), { type: "confirm" });
    const advanced = appUpdateReducer(downloading, { type: "progress", progress: { stage: "downloading", downloaded: 50, total: 100 } });
    expect(advanced).toEqual({ stage: "downloading", info: promptState().info, progress: { stage: "downloading", downloaded: 50, total: 100 } });
    expect(appUpdateReducer(advanced, { type: "progress", progress: { stage: "installing" } })).toEqual({ stage: "installing", info: promptState().info });
  });

  it("an install error returns to the prompt with the error, retryable", () => {
    const downloading = appUpdateReducer(promptState(), { type: "confirm" });
    const errored = appUpdateReducer(downloading, { type: "installError", message: "signature verification failed" });
    expect(errored).toEqual(promptState({ error: "signature verification failed" }));
    // Update is retryable: confirm works again from the errored prompt.
    expect(appUpdateReducer(errored, { type: "confirm" }).stage).toBe("downloading");
  });

  it("dismiss from any stage returns to idle", () => {
    for (const state of [promptState(), appUpdateReducer(promptState(), { type: "confirm" }), { stage: "installing" as const, info: promptState().info }]) {
      expect(appUpdateReducer(state, { type: "dismiss" })).toEqual({ stage: "idle" });
    }
  });

  it("progress is ignored when idle", () => {
    expect(appUpdateReducer(initialAppUpdateState, { type: "progress", progress: { stage: "installing" } })).toEqual({ stage: "idle" });
  });

  it("a check result never replaces an in-flight download", () => {
    const downloading = appUpdateReducer(promptState(), { type: "confirm" });
    expect(appUpdateReducer(downloading, { type: "checkResult", check: AVAILABLE, manual: true, dismissed: new Set() })).toBe(downloading);
  });
});
