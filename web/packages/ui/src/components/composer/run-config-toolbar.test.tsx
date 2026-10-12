import { act } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";

import { RunConfigToolbar } from "@/components/composer/run-config-toolbar";
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
