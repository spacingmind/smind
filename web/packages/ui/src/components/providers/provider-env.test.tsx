import { act, fireEvent, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { ProviderEnvField } from "@/components/providers/provider-env";
import type { ProviderEnvEffective, ProviderInfo } from "@/lib/types";
import { renderWithDaemon } from "@/test/render-with-daemon";
import { FakeUsageDaemon } from "@/test/usage-test-client";

function claude(over: Partial<ProviderInfo> = {}, effective?: Partial<ProviderEnvEffective>): ProviderInfo {
  return {
    id: "claude-native",
    label: "Claude Code",
    envEffective: { CLAUDE_CONFIG_DIR: { path: "/home/u/.claude", source: "default", exists: true, ...effective } },
    ...over,
  };
}

function setup(info: ProviderInfo): FakeUsageDaemon {
  const d = new FakeUsageDaemon([]);
  d.providers = { providers: [info] };
  renderWithDaemon(<ProviderEnvField info={info} hasClient />, d);
  return d;
}

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

function type(value: string): void {
  fireEvent.change(screen.getByTestId("provider-env-input-claude-native"), { target: { value } });
}

function setEnvCalls(d: FakeUsageDaemon): unknown[] {
  return d.calls.filter((c) => c.method === "provider.setEnv").map((c) => c.params);
}

describe("ProviderEnvField", () => {
  it("shows the CLI default when nothing is set", () => {
    setup(claude());
    expect(screen.getByText("Claude config directory")).toBeInTheDocument();
    expect(screen.getByTestId("provider-env-input-claude-native")).toHaveValue("");
    expect(screen.getByTestId("provider-env-input-claude-native")).toHaveAttribute("placeholder", "/home/u/.claude");
    expect(screen.getByTestId("provider-env-using-claude-native")).toHaveTextContent("Using /home/u/.claude · CLI default");
    expect(screen.queryByTestId("provider-env-clear-claude-native")).toBeNull();
  });

  it("shows the daemon-environment source", () => {
    setup(claude({}, { path: "/srv/claude", source: "environment" }));
    expect(screen.getByTestId("provider-env-using-claude-native")).toHaveTextContent("Using /srv/claude · from the daemon's environment");
  });

  it("shows the configured path with a Clear button", () => {
    setup(
      claude({ env: { CLAUDE_CONFIG_DIR: "/work/claude" }, envDirExists: { CLAUDE_CONFIG_DIR: true } }, { path: "/work/claude", source: "settings" }),
    );
    expect(screen.getByTestId("provider-env-input-claude-native")).toHaveValue("/work/claude");
    expect(screen.getByTestId("provider-env-using-claude-native")).toHaveTextContent("Using /work/claude · set in smind");
    expect(screen.getByTestId("provider-env-clear-claude-native")).toBeEnabled();
    expect(screen.getByTestId("provider-env-save-claude-native")).toBeDisabled();
  });

  it("rejects a relative path without calling the daemon", async () => {
    const d = setup(claude());
    type("relative/dir");
    await act(async () => {
      fireEvent.click(screen.getByTestId("provider-env-save-claude-native"));
    });
    expect(screen.getByRole("alert")).toHaveTextContent("Enter an absolute path (or start with ~/)");
    expect(setEnvCalls(d)).toEqual([]);
  });

  it("shows the daemon error inline", async () => {
    const d = setup(claude());
    d.setEnvError = new Error('provider.setEnv: CLAUDE_CONFIG_DIR: the path must be absolute (or start with ~/), got "x"');
    type("/ok/path");
    await act(async () => {
      fireEvent.click(screen.getByTestId("provider-env-save-claude-native"));
    });
    await flush();
    expect(screen.getByRole("alert")).toHaveTextContent("the path must be absolute");
  });

  it("notes a directory that does not exist", () => {
    setup(claude({}, { exists: false }));
    expect(screen.getByTestId("provider-env-missing-claude-native")).toHaveTextContent("This directory does not exist yet.");
  });

  it("Save sends provider.setEnv with the path", async () => {
    const d = setup(claude());
    type("~/alt-claude");
    await act(async () => {
      fireEvent.click(screen.getByTestId("provider-env-save-claude-native"));
    });
    await flush();
    expect(setEnvCalls(d)).toEqual([{ provider: "claude-native", key: "CLAUDE_CONFIG_DIR", value: "~/alt-claude" }]);
  });

  it("Save and Clear re-run the provider's readiness check", async () => {
    const d = setup(claude());
    const tests = () => d.calls.filter((c) => c.method === "provider.test").map((c) => c.params);
    type("~/alt-claude");
    await act(async () => {
      fireEvent.click(screen.getByTestId("provider-env-save-claude-native"));
    });
    await flush();
    expect(tests()).toEqual([{ provider: "claude-native" }]);
    // The answer is cached and fresh now; a second save marks it stale and asks again, once.
    type("~/other-claude");
    await act(async () => {
      fireEvent.click(screen.getByTestId("provider-env-save-claude-native"));
    });
    await flush();
    expect(tests()).toEqual([{ provider: "claude-native" }, { provider: "claude-native" }]);
  });

  it("Clear sends provider.setEnv with null", async () => {
    const d = setup(claude({ env: { CLAUDE_CONFIG_DIR: "/work/claude" } }, { path: "/work/claude", source: "settings" }));
    await act(async () => {
      fireEvent.click(screen.getByTestId("provider-env-clear-claude-native"));
    });
    await flush();
    expect(setEnvCalls(d)).toEqual([{ provider: "claude-native", key: "CLAUDE_CONFIG_DIR", value: null }]);
  });

  it("renders nothing for a provider with no env keys", () => {
    setup({ id: "kimi", label: "Kimi" });
    expect(screen.queryByTestId("provider-env-kimi")).toBeNull();
    expect(screen.queryByText("Claude config directory")).toBeNull();
  });
});
