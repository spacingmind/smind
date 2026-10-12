import { act } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { RunConfigToolbar, type LiveRunControls } from "@/components/composer/run-config-toolbar";
import type { AgentProfile, ProviderInfo } from "@/lib/types";

/** The same served catalog shape composer.test.tsx stubs provider.list with: Claude's mode list is static, GLM's ACP auto-accept is on. */
const SERVED_PROVIDERS: ProviderInfo[] = [
  {
    id: "claude-native",
    label: "Claude Code",
    defaultMode: "acceptEdits",
    liveModeSwitch: true,
    modes: [
      { id: "acceptEdits", label: "Accept File Edits", description: "Automatically approves edit-focused tools without prompting" },
      { id: "default", label: "Always Ask" },
      { id: "plan", label: "Plan Mode" },
      { id: "auto", label: "Auto mode", autoApproves: true },
      { id: "bypassPermissions", label: "Bypass", description: "Skip all permission prompts (use with caution)", autoApproves: true },
    ],
    models: [{ id: "claude-opus-5-5", label: "Opus 5.5" }],
  },
  {
    id: "glm",
    label: "GLM",
    defaultMode: "default",
    modesDiscovered: true,
    supportsAutoAccept: true,
    liveModeSwitch: true,
    modes: [
      { id: "default", label: "Default" },
      { id: "accept_edits", label: "Accept Edits" },
      { id: "bypass_permissions", label: "Bypass Permissions", autoApproves: true },
    ],
    models: [{ id: "glm-5.3", label: "GLM 5.3" }],
  },
];

const PROFILE: AgentProfile = {
  ID: 7,
  Name: "Agent 7",
  Provider: "claude-native",
  PermissionMode: "",
  AutoAccept: false,
  ThinkingLevel: "",
  Notes: "",
  CreatedAt: "2026-01-01T00:00:00Z",
  UpdatedAt: "2026-01-01T00:00:00Z",
};

function toolbarProps(over: Partial<React.ComponentProps<typeof RunConfigToolbar>> = {}) {
  return { taskId: 1, chatId: 10, profiles: [PROFILE], providers: SERVED_PROVIDERS, ...over };
}

function Parts() {
  return (
    <>
      <RunConfigToolbar.ModelPicker />
      <RunConfigToolbar.Mode />
      <RunConfigToolbar.Thinking />
    </>
  );
}

function renderToolbar(over: Partial<React.ComponentProps<typeof RunConfigToolbar>> = {}) {
  const props = toolbarProps(over);
  return { props, rerender: render(<RunConfigToolbar {...props}><Parts /></RunConfigToolbar>).rerender };
}

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

/** Picks `modelId` ("" = the provider's default row) of `providerId` through the combined picker: open, drill into the provider if the root shows, click the row. */
async function pickModel(providerId: string, modelId = ""): Promise<void> {
  fireEvent.click(screen.getByTestId("composer-model-picker"));
  // The popover opens on the current provider's view: go back to the root when the target is another provider's.
  const back = screen.queryByTestId("model-picker-back");
  if (back && !screen.queryByTestId(`model-row-${providerId}:${modelId}`)) fireEvent.click(back);
  const drill = screen.queryByTestId(`model-provider-${providerId}`);
  if (drill) fireEvent.click(drill);
  fireEvent.click(await screen.findByTestId(`model-row-${providerId}:${modelId}`));
  await flush();
}

/** Picks `optionText` from the Radix Select labelled `label` (same interaction shape composer.test.tsx uses). */
async function pick(label: string, optionText: string): Promise<void> {
  fireEvent.click(screen.getByLabelText(label));
  const options = await screen.findAllByRole("option");
  const target = options.find((o) => o.textContent === optionText);
  expect(target).toBeTruthy();
  fireEvent.click(target!);
  await flush();
}

describe("RunConfigToolbar pills", () => {
  beforeEach(() => {
    window.localStorage.clear();
  });

  it("pills render an icon, the label and a chevron", () => {
    renderToolbar();

    for (const trigger of [screen.getByTestId("composer-model-picker"), screen.getByLabelText("Permission mode"), screen.getByLabelText("Thinking level")]) {
      const svgs = trigger.querySelectorAll("svg");
      // leading icon before the label text, trailing chevron after it
      expect(svgs.length).toBeGreaterThanOrEqual(2);
      expect(trigger.textContent).toBeTruthy();
    }
  });

  it("pills keep their aria-labels and test ids", () => {
    renderToolbar();

    expect(screen.getByTestId("composer-model-picker")).toHaveAttribute("aria-label", expect.stringMatching(/^Model: /));
    for (const label of ["Permission mode", "Thinking level"]) {
      expect(screen.getByLabelText(label)).toBeTruthy();
    }
  });

  it("permission pill is warning-tinted for an autoApproves mode and plain otherwise", async () => {
    renderToolbar();

    // acceptEdits is Claude's default -- not auto-approving, so plain.
    const plain = screen.getByLabelText("Permission mode");
    expect(plain).not.toHaveAttribute("data-danger");
    expect(plain.className).not.toContain("text-warning");

    await pick("Permission mode", "Bypass");

    const bypass = screen.getByLabelText("Permission mode");
    expect(bypass).toHaveAttribute("data-danger", "true");
    expect(bypass.className).toContain("text-warning");
  });

  it("permission pill is warning-tinted while Auto-accept is on", async () => {
    renderToolbar();

    await pickModel("glm");
    fireEvent.click(screen.getByTestId("composer-auto-accept"));
    await flush();

    const mode = screen.getByLabelText("Permission mode");
    expect(mode).toHaveAttribute("data-danger", "true");
    expect(mode.className).toContain("text-warning");
  });

  it("pill labels and chevrons carry the below-sm hide classes and 44px touch size", () => {
    renderToolbar();

    for (const trigger of [screen.getByTestId("composer-model-picker"), screen.getByLabelText("Permission mode"), screen.getByLabelText("Thinking level")]) {
      expect(trigger.className).toContain("size-11");
      expect(trigger.className).toContain("md:h-7");
      expect(trigger.className).toContain("max-sm:[&>svg:last-child]:hidden");
      // Radix's SelectValue ignores className, so Select pills hide the label from the trigger; the picker trigger hides its own span.
      if (trigger.dataset.testid === "composer-model-picker") expect(trigger.querySelector(":scope > span")?.className).toContain("max-sm:hidden");
      else expect(trigger.className).toContain("max-sm:[&>span]:hidden!");
    }
  });
});

describe("RunConfigToolbar live run (D1)", () => {
  beforeEach(() => {
    window.localStorage.clear();
  });

  const claudeModes = SERVED_PROVIDERS[0].modes!;
  function live(over: Partial<LiveRunControls> = {}): LiveRunControls {
    return { provider: "claude-native", runId: "run-1", modes: claudeModes, mode: "plan", autoAccept: false, showAutoAccept: false, onChange: vi.fn().mockResolvedValue(undefined), ...over };
  }

  it("live run: the mode pill shows the running run's mode and changing it calls the live onChange", async () => {
    const liveRun = live();
    renderToolbar({ liveRun });
    expect(screen.getByLabelText("Permission mode")).toHaveTextContent("Plan Mode");

    await pick("Permission mode", "Always Ask");
    expect(liveRun.onChange).toHaveBeenCalledWith({ modeId: "default" });
  });

  it("live run: Auto-accept toggle calls onChange with autoAccept", async () => {
    const liveRun = live({ modes: SERVED_PROVIDERS[1].modes!, mode: "default", showAutoAccept: true });
    renderToolbar({ liveRun });
    const toggle = screen.getByTestId("composer-auto-accept");
    expect(toggle).toHaveAttribute("aria-pressed", "false");
    fireEvent.click(toggle);
    await flush();
    expect(liveRun.onChange).toHaveBeenCalledWith({ autoAccept: true });
  });

  it("live run: a failed switch is shown inline", async () => {
    const liveRun = live({ onChange: vi.fn().mockRejectedValue(new Error("run already finished")) });
    renderToolbar({ liveRun });
    await pick("Permission mode", "Always Ask");
    expect(screen.getByTestId("permission-mode-error")).toHaveTextContent("run already finished");
  });

  it("live run: a successful switch also becomes the next run's config (a follow-up goes out with what the pill shows)", async () => {
    const onChange = vi.fn();
    renderToolbar({ liveRun: live(), onChange });
    await pick("Permission mode", "Always Ask");
    expect(onChange.mock.calls.at(-1)![0].state.permissionMode).toBe("default");
  });

  it("live run: the failed-switch message is cleared once the run is no longer live", async () => {
    const liveRun = live({ onChange: vi.fn().mockRejectedValue(new Error("run already finished")) });
    const { props, rerender } = renderToolbar({ liveRun });
    await pick("Permission mode", "Always Ask");
    expect(screen.getByTestId("permission-mode-error")).toBeInTheDocument();
    rerender(
      <RunConfigToolbar {...props} liveRun={null}>
        <Parts />
      </RunConfigToolbar>,
    );
    expect(screen.queryByTestId("permission-mode-error")).toBeNull();
  });

  it("no live run: the mode pill edits the next run's config and makes no RPC", async () => {
    const onChange = vi.fn();
    renderToolbar({ liveRun: null, onChange });
    await pick("Permission mode", "Plan Mode");
    expect(screen.getByLabelText("Permission mode")).toHaveTextContent("Plan Mode");
    expect(onChange.mock.calls.at(-1)![0].state.permissionMode).toBe("plan");
  });
});
