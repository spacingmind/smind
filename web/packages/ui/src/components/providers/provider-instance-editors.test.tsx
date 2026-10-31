import { act, fireEvent, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { ProviderInstanceEditors } from "@/components/providers/provider-instance-editors";
import type { ProviderInfo, ProviderInstanceOverlay } from "@/lib/types";
import { renderWithDaemon } from "@/test/render-with-daemon";
import { FakeUsageDaemon } from "@/test/usage-test-client";

function claudeInstance(instance?: ProviderInstanceOverlay): ProviderInfo {
  return {
    id: "claude-work",
    label: "Claude work",
    custom: true,
    runner: "claude-native",
    extends: "claude-native",
    harness: "claude-code",
    instance,
  };
}

function setup(info: ProviderInfo): FakeUsageDaemon {
  const d = new FakeUsageDaemon([]);
  d.providers = { providers: [info] };
  renderWithDaemon(<ProviderInstanceEditors info={info} hasClient />, d);
  return d;
}

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

function calls(d: FakeUsageDaemon): unknown[] {
  return d.calls.filter((c) => c.method === "provider.updateInstance").map((c) => c.params);
}

describe("ProviderInstanceEditors", () => {
  it("renders nothing for a built-in provider and for a custom ACP provider", () => {
    setup({ id: "claude-native", label: "Claude Code" });
    expect(screen.queryByTestId("provider-instance-editors-claude-native")).toBeNull();
    expect(document.body.textContent).toBe("");
    setup({ id: "glm-acp", label: "GLM", custom: true, runner: "acp", harness: "acp" });
    expect(screen.queryByTestId("provider-instance-editors-glm-acp")).toBeNull();
    expect(document.body.textContent).toBe("");
  });

  it("shows the stored models and adds and removes ids, saving one call with the other mode list empty", async () => {
    const d = setup(claudeInstance({ models: [{ id: "m-a", label: "A", default: true }, { id: "m-b" }] }));
    expect(screen.getByTestId("instance-model-row-m-a")).toHaveTextContent("m-a");
    expect(screen.getByTestId("instance-model-row-m-b")).toHaveTextContent("m-b");
    // Empty and duplicate ids are ignored.
    fireEvent.change(screen.getByTestId("instance-model-input"), { target: { value: "m-a" } });
    fireEvent.click(screen.getByTestId("instance-model-add"));
    fireEvent.change(screen.getByTestId("instance-model-input"), { target: { value: "   " } });
    fireEvent.click(screen.getByTestId("instance-model-add"));
    expect(screen.queryByTestId("instance-model-row-m-c")).toBeNull();
    // Enter adds too.
    fireEvent.change(screen.getByTestId("instance-model-input"), { target: { value: "m-c" } });
    fireEvent.keyDown(screen.getByTestId("instance-model-input"), { key: "Enter" });
    expect(screen.getByTestId("instance-model-row-m-c")).toBeInTheDocument();
    expect(screen.getByTestId("instance-model-input")).toHaveValue("");
    fireEvent.click(screen.getByTestId("instance-model-remove-m-b"));
    expect(screen.queryByTestId("instance-model-row-m-b")).toBeNull();
    await act(async () => {
      fireEvent.click(screen.getByTestId("instance-models-save"));
    });
    await flush();
    expect(calls(d)).toEqual([
      { id: "claude-work", models: [{ id: "m-a", label: "A", default: true }, { id: "m-c" }], additionalModels: [] },
    ]);
  });

  it("switching Replace list to Add to list sends models: [] and additionalModels with the draft", async () => {
    const d = setup(claudeInstance({ models: [{ id: "m-a" }] }));
    expect(screen.getByTestId("instance-mode-replace")).toHaveAttribute("aria-checked", "true");
    fireEvent.click(screen.getByTestId("instance-mode-add"));
    await act(async () => {
      fireEvent.click(screen.getByTestId("instance-models-save"));
    });
    await flush();
    expect(calls(d)).toEqual([{ id: "claude-work", models: [], additionalModels: [{ id: "m-a" }] }]);
  });

  it("disallowed tools: add chip, remove chip, save sends the whole list", async () => {
    const d = setup(claudeInstance({ disallowedTools: ["WebSearch"] }));
    expect(screen.getByTestId("instance-tool-WebSearch")).toHaveTextContent("WebSearch");
    fireEvent.change(screen.getByTestId("instance-tool-input"), { target: { value: "Bash(rm:*)" } });
    fireEvent.click(screen.getByTestId("instance-tool-add"));
    expect(screen.getByTestId("instance-tool-Bash(rm:*)")).toBeInTheDocument();
    fireEvent.click(screen.getByTestId("instance-tool-remove-WebSearch"));
    expect(screen.queryByTestId("instance-tool-WebSearch")).toBeNull();
    await act(async () => {
      fireEvent.click(screen.getByTestId("instance-tools-save"));
    });
    await flush();
    expect(calls(d)).toEqual([{ id: "claude-work", disallowedTools: ["Bash(rm:*)"] }]);
  });

  it("the tool editor and the toggle are hidden for a codex-native instance", () => {
    const info: ProviderInfo = {
      id: "codex-work",
      label: "Codex work",
      custom: true,
      runner: "codex-native",
      extends: "codex-native",
      harness: "codex-app-server",
      instance: { models: [{ id: "gpt-5" }] },
    };
    setup(info);
    expect(screen.getByTestId("instance-models-codex-work")).toBeInTheDocument();
    expect(screen.queryByTestId("instance-tools-codex-work")).toBeNull();
    expect(screen.queryByTestId("instance-inherit-switch")).toBeNull();
  });

  it("the inherit switch calls updateInstance immediately with inheritClaudeConfig", async () => {
    const d = setup(claudeInstance({ disallowedTools: ["WebSearch"] }));
    expect(screen.getByTestId("instance-inherit-switch")).toHaveAttribute("aria-checked", "false");
    await act(async () => {
      fireEvent.click(screen.getByTestId("instance-inherit-switch"));
    });
    await flush();
    expect(calls(d)).toEqual([{ id: "claude-work", inheritClaudeConfig: true }]);
    expect(d.providers.providers[0].instance?.inheritClaudeConfig).toBe(true);
  });

  it("toggling inherit keeps an unsaved models draft", async () => {
    const instance = { models: [{ id: "m-a" }] };
    const info = claudeInstance(instance);
    const d = new FakeUsageDaemon([]);
    d.providers = { providers: [info] };
    const view = renderWithDaemon(<ProviderInstanceEditors info={info} hasClient />, d);
    fireEvent.click(screen.getByTestId("instance-mode-add"));
    fireEvent.change(screen.getByTestId("instance-model-input"), { target: { value: "m-c" } });
    fireEvent.click(screen.getByTestId("instance-model-add"));
    await act(async () => {
      fireEvent.click(screen.getByTestId("instance-inherit-switch"));
    });
    await flush();
    // The landed save re-reads provider.list: the same instance with the flag added.
    const refreshed = claudeInstance({ ...instance, inheritClaudeConfig: true });
    view.rerenderWith(d, <ProviderInstanceEditors info={refreshed} hasClient />);
    expect(screen.getByTestId("instance-inherit-switch")).toHaveAttribute("aria-checked", "true");
    expect(screen.getByTestId("instance-model-row-m-c")).toBeInTheDocument();
    expect(screen.getByTestId("instance-models-save")).toBeEnabled();
    expect(screen.getByTestId("instance-mode-add")).toHaveAttribute("aria-checked", "true");
  });

  it("a daemon refusal shows its text and keeps the draft", async () => {
    const d = new FakeUsageDaemon([]);
    d.updateInstanceError = new Error("provider.updateInstance: run-scoped callers may not edit providers");
    const info = claudeInstance({ models: [{ id: "m-a" }] });
    d.providers = { providers: [info] };
    renderWithDaemon(<ProviderInstanceEditors info={info} hasClient />, d);
    fireEvent.change(screen.getByTestId("instance-model-input"), { target: { value: "m-c" } });
    fireEvent.click(screen.getByTestId("instance-model-add"));
    await act(async () => {
      fireEvent.click(screen.getByTestId("instance-models-save"));
    });
    await flush();
    expect(await screen.findByTestId("instance-editors-error")).toHaveTextContent(
      "provider.updateInstance: run-scoped callers may not edit providers",
    );
    // The refusal sits inside the models row, next to Save, and is the only one in the DOM.
    expect(screen.getAllByTestId("instance-editors-error")).toHaveLength(1);
    expect(screen.getByTestId("instance-models-claude-work")).toContainElement(screen.getByTestId("instance-editors-error"));
    expect(screen.getByTestId("instance-model-row-m-a")).toBeInTheDocument();
    expect(screen.getByTestId("instance-model-row-m-c")).toBeInTheDocument();
    expect(screen.getByTestId("instance-models-save")).toBeEnabled();
    // The next edit clears the message.
    await act(async () => {
      fireEvent.change(screen.getByTestId("instance-model-input"), { target: { value: "m-d" } });
    });
    expect(screen.queryByTestId("instance-editors-error")).toBeNull();
  });

  it("save is disabled while unchanged", () => {
    setup(claudeInstance({ models: [{ id: "m-a" }], disallowedTools: ["WebSearch"] }));
    expect(screen.getByTestId("instance-models-save")).toBeDisabled();
    expect(screen.getByTestId("instance-tools-save")).toBeDisabled();
    expect(screen.queryByTestId("instance-models-reset")).toBeNull();
    fireEvent.change(screen.getByTestId("instance-model-input"), { target: { value: "m-c" } });
    fireEvent.click(screen.getByTestId("instance-model-add"));
    expect(screen.getByTestId("instance-models-save")).toBeEnabled();
    expect(screen.getByTestId("instance-models-reset")).toBeInTheDocument();
    fireEvent.click(screen.getByTestId("instance-models-reset"));
    expect(screen.getByTestId("instance-models-save")).toBeDisabled();
    expect(screen.queryByTestId("instance-models-reset")).toBeNull();
    expect(screen.queryByTestId("instance-model-row-m-c")).toBeNull();
  });
});
