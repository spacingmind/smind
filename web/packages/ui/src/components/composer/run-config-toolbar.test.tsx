import { act, type ReactNode } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { RunConfigToolbar, useRunConfig, type LiveRunControls, type RunConfigState } from "@/components/composer/run-config-toolbar";
import { DaemonQueryProvider } from "@/lib/query/daemon-query-provider";
import { createQueryClient } from "@/lib/query/query-client";
import { queryKeys } from "@/lib/query-keys";
import type { AgentProfile, ProviderInfo, RunSummary } from "@/lib/types";
import { useDefaultAgentStore } from "@/stores/default-agent-store";
import { useProvidersViewStore } from "@/stores/providers-view-store";

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

function renderToolbar(over: Partial<React.ComponentProps<typeof RunConfigToolbar>> = {}, queryClient = createQueryClient()) {
  const props = toolbarProps(over);
  // No daemon client: the shared run-list query stays disabled unless a test seeds it into queryClient.
  const wrapper = ({ children }: { children: ReactNode }) => (
    <DaemonQueryProvider queryClient={queryClient} client={null} events={null}>
      {children}
    </DaemonQueryProvider>
  );
  return { props, rerender: render(<RunConfigToolbar {...props}><Parts /></RunConfigToolbar>, { wrapper }).rerender };
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
  const target = options.find((o) => (o.getAttribute("aria-label") ?? o.textContent) === optionText);
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

  it("manage provider opens Providers with that provider selected", async () => {
    useProvidersViewStore.getState().reset();
    const events: CustomEvent[] = [];
    const listener = (e: Event) => events.push(e as CustomEvent);
    window.addEventListener("smind:open-settings", listener);
    renderToolbar();
    // Open the picker's provider view (starts on Claude Code's view).
    fireEvent.click(screen.getByTestId("composer-model-picker"));
    fireEvent.click(await screen.findByTestId("model-picker-provider-settings"));
    expect(useProvidersViewStore.getState().selectedProviderId).toBe("claude-native");
    expect(events.at(-1)!.detail).toEqual({ sectionId: "providers" });
    window.removeEventListener("smind:open-settings", listener);
  });
});

describe("RunConfigToolbar observed model", () => {
  function run(over: Partial<RunSummary>): RunSummary {
    return { ID: "r", TaskID: 1, ChatID: 10, Provider: "claude-native", StartedAt: "2026-10-23T10:00:00Z", usage: { source: "acp", model: "claude-opus-5-5" }, ...over } as RunSummary;
  }

  it("RunConfigToolbar chip uses the chat's last run model", () => {
    const queryClient = createQueryClient();
    queryClient.setQueryData(queryKeys.runList(), [
      run({ ID: "other-chat", ChatID: 99, usage: { source: "acp", model: "glm-5.3" }, StartedAt: "2026-10-23T12:00:00Z" }),
      run({ ID: "this-chat" }),
    ]);
    renderToolbar({}, queryClient);
    const chip = screen.getByTestId("composer-model-picker");
    expect(chip).toHaveAttribute("aria-label", expect.stringContaining("Opus 5.5"));
    expect(chip).toHaveAttribute("title", "Claude Code · Opus 5.5 (default)");
  });

  it("the chip keeps the provider name when no run is known", () => {
    renderToolbar();
    expect(screen.getByTestId("composer-model-picker")).toHaveAttribute("title", "Claude Code");
  });
});

// ADR-0042 AC37: an instance appears wherever a provider does, and runs through its base provider's capabilities.
describe("RunConfigToolbar with a provider instance", () => {
  const WORK: ProviderInfo = {
    id: "claude-work",
    label: "Claude work",
    runner: "claude-native",
    extends: "claude-native",
    custom: true,
    defaultMode: "acceptEdits",
    liveModeSwitch: true,
    modes: [{ id: "acceptEdits", label: "Accept File Edits" }],
    models: [{ id: "claude-opus-5-5", label: "Opus 5.5" }],
  };
  const CUSTOM_ACP: ProviderInfo = { id: "my-acp", label: "My ACP agent", runner: "acp", custom: true, defaultMode: "default", modes: [{ id: "default", label: "Default" }], models: [] };

  beforeEach(() => {
    window.localStorage.clear();
  });

  it("lists the instance and a custom ACP provider in the composer picker next to the built-ins", async () => {
    renderToolbar({ providers: [...SERVED_PROVIDERS, WORK, CUSTOM_ACP] });
    fireEvent.click(screen.getByTestId("composer-model-picker"));
    fireEvent.click(await screen.findByTestId("model-picker-back"));
    const rows = await screen.findAllByTestId(/^model-provider-/);
    expect(rows.map((r) => r.getAttribute("data-testid"))).toEqual(["model-provider-claude-native", "model-provider-glm", "model-provider-claude-work", "model-provider-my-acp"]);
    expect(screen.getByTestId("model-provider-claude-work")).toHaveTextContent("Claude work");
    // The unknown ACP provider wears the generic provider icon, never a throw or a blank.
    expect(screen.getByTestId("model-provider-my-acp").querySelector("svg")).not.toBeNull();
  });

  it("picking the instance keeps the Thinking control (its runner is claude-native) and the base's modes", async () => {
    renderToolbar({ providers: [...SERVED_PROVIDERS, WORK, CUSTOM_ACP] });
    await pickModel("claude-work");
    expect(screen.getByLabelText("Thinking level")).toBeTruthy();
    expect(screen.getByTestId("composer-model-picker")).toHaveAttribute("aria-label", expect.stringContaining("Claude work"));
  });

  it("picking a custom ACP provider drops the Thinking control (not a claude-native runner)", async () => {
    renderToolbar({ providers: [...SERVED_PROVIDERS, WORK, CUSTOM_ACP] });
    await pickModel("my-acp");
    expect(screen.queryByLabelText("Thinking level")).toBeNull();
  });
});

describe("RunConfigToolbar hand-off state", () => {
  const GLM_PROFILE: AgentProfile = { ...PROFILE, ID: 9, Name: "Agent 9", Provider: "glm", PermissionMode: "accept_edits" };
  const HANDED: RunConfigState = { baseAgentId: null, custom: false, provider: "claude-native", permissionMode: "plan", autoAccept: false, thinkingLevel: "", model: "" };

  function Probe({ apply }: { apply?: RunConfigState }) {
    const { state, actions } = useRunConfig();
    return (
      <>
        <output data-testid="probe">{`${state.provider}/${state.permissionMode}`}</output>
        {apply ? <button onClick={() => actions.applyState(apply)}>apply</button> : null}
      </>
    );
  }

  function wrap(children: ReactNode) {
    return (
      <DaemonQueryProvider queryClient={createQueryClient()} client={null} events={null}>
        {children}
      </DaemonQueryProvider>
    );
  }

  beforeEach(() => {
    window.localStorage.clear();
    useDefaultAgentStore.getState().set("9");
  });

  it("applyState replaces the state and blocks the default-agent seed", async () => {
    const { rerender } = render(
      wrap(
        <RunConfigToolbar {...toolbarProps({ profiles: [] })}>
          <Probe apply={HANDED} />
        </RunConfigToolbar>,
      ),
    );
    fireEvent.click(screen.getByText("apply"));
    await flush();
    expect(screen.getByTestId("probe").textContent).toBe("claude-native/plan");
    // The default agent's profile loads afterwards: it must not overwrite the applied state.
    rerender(
      wrap(
        <RunConfigToolbar {...toolbarProps({ profiles: [GLM_PROFILE] })}>
          <Probe apply={HANDED} />
        </RunConfigToolbar>,
      ),
    );
    await flush();
    expect(screen.getByTestId("probe").textContent).toBe("claude-native/plan");
    useDefaultAgentStore.getState().set(null);
  });

  it("initialState seeds a toolbar with no persisted state", async () => {
    render(
      wrap(
        <RunConfigToolbar {...toolbarProps({ profiles: [GLM_PROFILE], initialState: HANDED })}>
          <Probe />
        </RunConfigToolbar>,
      ),
    );
    await flush();
    expect(screen.getByTestId("probe").textContent).toBe("claude-native/plan");
    useDefaultAgentStore.getState().set(null);
  });
});
