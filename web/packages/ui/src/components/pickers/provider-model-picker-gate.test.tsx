import { fireEvent, render, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import {
  ProviderModelPicker,
  type PickerProfileRow,
  type ProviderModelPickerProps,
} from "@/components/pickers/provider-model-picker";
import { buildBrowserProviders } from "@/lib/model-browser";
import type { ProviderGate, ProviderInfo } from "@/lib/types";

const mobile = vi.hoisted(() => ({ value: false }));
vi.mock("@/hooks/use-mobile", () => ({ useIsMobile: () => mobile.value }));

const FUTURE = new Date(Date.now() + 2 * 3600_000).toISOString();
const PAST = new Date(Date.now() - 60_000).toISOString();

function infos(
  over: {
    claude?: Partial<ProviderInfo>;
    glm?: Partial<ProviderInfo>;
    kimi?: Partial<ProviderInfo>;
  } = {},
): ProviderInfo[] {
  return [
    {
      id: "claude-native",
      label: "Claude Code",
      models: [
        { id: "opus", label: "Opus" },
        { id: "sonnet", label: "Sonnet" },
      ],
      ...over.claude,
    },
    {
      id: "glm",
      label: "GLM",
      models: [{ id: "glm-5", label: "GLM 5" }],
      ...over.glm,
    },
    {
      id: "kimi",
      label: "Kimi",
      models: [{ id: "k2", label: "K2" }],
      ...over.kimi,
    },
  ];
}

function renderPicker(
  list: ProviderInfo[],
  over: Partial<ProviderModelPickerProps> = {},
) {
  const spies = {
    onSelect: vi.fn(),
    onApplyProfile: vi.fn(),
    onOpenProviderSettings: vi.fn(),
    onClearCooldown: vi.fn(),
  };
  render(
    <ProviderModelPicker
      providers={buildBrowserProviders(list, {})}
      selectedProvider="claude-native"
      selectedModel="opus"
      boundProvider={null}
      disabled={false}
      {...spies}
      {...over}
    />,
  );
  return spies;
}
const open = () => fireEvent.click(screen.getByTestId("composer-model-picker"));
const search = () => screen.getByTestId("model-search-input");
function toRoot() {
  open();
  fireEvent.click(screen.getByTestId("model-picker-back"));
}

const gate = (g: ProviderGate): ProviderGate => g;
const REASONS: Array<[ProviderGate, string]> = [
  [
    gate({
      reason: "not_signed_in",
      action: {
        kind: "sign_in",
        target: "settings:providers?provider=glm",
        label: "Add the API key for GLM",
      },
    }),
    "Signed out",
  ],
  [
    gate({
      reason: "out_of_plan",
      until: FUTURE,
      action: { kind: "wait_until", label: "Wait" },
    }),
    "Plan used up",
  ],
  [
    gate({
      reason: "cooling_down",
      until: FUTURE,
      action: { kind: "clear_cooldown", target: "glm", label: "Clear" },
    }),
    "Cooling down until",
  ],
  [
    gate({
      reason: "unsupported_by_harness",
      action: { kind: "pick_provider", label: "Pick another provider" },
    }),
    "Not supported by this harness",
  ],
  [
    gate({
      reason: "model_not_in_plan",
      action: { kind: "pick_model", label: "Pick another model" },
    }),
    "Not in your plan",
  ],
];

beforeEach(() => {
  mobile.value = false;
});

describe("picker gates: provider rows", () => {
  it.each(REASONS)(
    "a hard %j provider row stays visible with its reason, is aria-disabled and cannot be entered",
    (g, text) => {
      renderPicker(infos({ glm: { gate: g } }));
      toRoot();
      const row = screen.getByTestId("model-provider-glm");
      expect(row).toHaveAttribute("aria-disabled", "true");
      expect(within(row).getByTestId("model-gate-glm")).toHaveTextContent(text);
      fireEvent.click(row);
      expect(
        screen.queryByTestId("model-row-glm:glm-5"),
      ).not.toBeInTheDocument();
      expect(screen.getByTestId("model-provider-glm")).toBeInTheDocument();
    },
  );

  it("an advisory gate warns but the provider can still be entered", () => {
    renderPicker(
      infos({
        glm: { gate: { reason: "out_of_plan", until: FUTURE, advisory: true } },
      }),
    );
    toRoot();
    const row = screen.getByTestId("model-provider-glm");
    expect(row).not.toHaveAttribute("aria-disabled");
    expect(within(row).getByTestId("model-gate-glm")).toHaveTextContent(
      "Plan used up",
    );
    fireEvent.click(row);
    expect(screen.getByTestId("model-row-glm:glm-5")).toBeInTheDocument();
  });

  it("a time gate whose until has passed shows nothing", () => {
    renderPicker(
      infos({ glm: { gate: { reason: "cooling_down", until: PAST } } }),
    );
    toRoot();
    expect(screen.queryByTestId("model-gate-glm")).not.toBeInTheDocument();
    expect(screen.getByTestId("model-provider-glm")).not.toHaveAttribute(
      "aria-disabled",
    );
  });

  it("an unknown reason shows its action label and is not selectable", () => {
    renderPicker(
      infos({
        glm: {
          gate: {
            reason: "from_the_future",
            action: { kind: "mystery", label: "Do the thing" },
          },
        },
      }),
    );
    toRoot();
    expect(screen.getByTestId("model-gate-glm")).toHaveTextContent(
      "Do the thing",
    );
    expect(screen.getByTestId("model-provider-glm")).toHaveAttribute(
      "aria-disabled",
      "true",
    );
    expect(
      screen.queryByTestId("model-gate-action-glm"),
    ).not.toBeInTheDocument();
  });
});

describe("picker gates: model rows", () => {
  it("a hard model gate blocks that model only; click does not select, the action runs", () => {
    const g = gate({
      reason: "model_not_in_plan",
      action: { kind: "pick_model", label: "Pick another model" },
    });
    const spies = renderPicker(infos({ claude: { modelGates: { opus: g } } }), {
      selectedModel: "sonnet",
    });
    open();
    const opus = screen.getByTestId("model-row-claude-native:opus");
    expect(opus).toHaveAttribute("aria-disabled", "true");
    expect(
      within(opus).getByTestId("model-gate-claude-native:opus"),
    ).toHaveTextContent("Not in your plan");
    fireEvent.click(opus);
    expect(spies.onSelect).not.toHaveBeenCalled();
    fireEvent.click(screen.getByTestId("model-row-claude-native:sonnet"));
    expect(spies.onSelect).toHaveBeenCalledWith("claude-native", "sonnet");
  });

  it("an advisory model gate warns and still selects", () => {
    const spies = renderPicker(
      infos({
        claude: {
          modelGates: { opus: { reason: "model_not_in_plan", advisory: true } },
        },
      }),
      { selectedModel: "sonnet" },
    );
    open();
    const opus = screen.getByTestId("model-row-claude-native:opus");
    expect(opus).not.toHaveAttribute("aria-disabled");
    expect(
      within(opus).getByTestId("model-gate-claude-native:opus"),
    ).toBeInTheDocument();
    fireEvent.click(opus);
    expect(spies.onSelect).toHaveBeenCalledWith("claude-native", "opus");
  });

  it("flat search results keep a gated model visible and blocked", () => {
    const spies = renderPicker(
      infos({
        glm: {
          gate: {
            reason: "cooling_down",
            until: FUTURE,
            action: { kind: "clear_cooldown", target: "glm", label: "Clear" },
          },
        },
      }),
    );
    toRoot();
    fireEvent.change(search(), { target: { value: "glm" } });
    const row = screen.getByTestId("model-row-glm:glm-5");
    expect(row).toHaveAttribute("aria-disabled", "true");
    fireEvent.click(row);
    expect(spies.onSelect).not.toHaveBeenCalled();
  });
});

describe("picker gates: actions", () => {
  it("sign_in opens that provider's settings and closes the picker", () => {
    const spies = renderPicker(infos({ glm: { gate: REASONS[0]![0] } }));
    toRoot();
    fireEvent.click(screen.getByTestId("model-gate-action-glm"));
    expect(spies.onOpenProviderSettings).toHaveBeenCalledExactlyOnceWith("glm");
    expect(screen.queryByTestId("model-search-input")).not.toBeInTheDocument();
  });

  it("clear_cooldown calls onClearCooldown and keeps the picker open", () => {
    const spies = renderPicker(infos({ glm: { gate: REASONS[2]![0] } }));
    toRoot();
    fireEvent.click(screen.getByTestId("model-gate-action-glm"));
    expect(spies.onClearCooldown).toHaveBeenCalledExactlyOnceWith("glm");
    expect(screen.getByTestId("model-search-input")).toBeInTheDocument();
  });

  it("clear_cooldown shows no control when the owner cannot clear", () => {
    renderPicker(infos({ glm: { gate: REASONS[2]![0] } }), {
      onClearCooldown: undefined,
    });
    toRoot();
    expect(
      screen.queryByTestId("model-gate-action-glm"),
    ).not.toBeInTheDocument();
  });

  it("pick_provider goes to the provider list", () => {
    renderPicker(
      infos({
        claude: {
          gate: {
            reason: "unsupported_by_harness",
            action: { kind: "pick_provider", label: "Pick another provider" },
          },
        },
      }),
    );
    open();
    // The selected provider is gated: its view is not blocked, the row action returns to the providers.
    fireEvent.click(screen.getByTestId("model-gate-action-claude-native:opus"));
    expect(screen.getByText("Providers")).toBeInTheDocument();
  });

  it("pick_model opens the provider's model list", () => {
    renderPicker(
      infos({
        glm: {
          gate: {
            reason: "model_not_in_plan",
            action: { kind: "pick_model", label: "Pick another model" },
          },
        },
      }),
    );
    toRoot();
    fireEvent.click(screen.getByTestId("model-gate-action-glm"));
    expect(screen.getByTestId("model-row-glm:glm-5")).toBeInTheDocument();
  });

  it("switch_profile applies the fallback profile", () => {
    const spies = renderPicker(
      infos({
        glm: {
          gate: {
            reason: "out_of_plan",
            until: FUTURE,
            action: {
              kind: "switch_profile",
              target: "profile:7",
              label: "Switch to Backup",
            },
          },
        },
      }),
    );
    toRoot();
    fireEvent.click(screen.getByTestId("model-gate-action-glm"));
    expect(spies.onApplyProfile).toHaveBeenCalledExactlyOnceWith("7");
  });
});

describe("picker gates: keyboard", () => {
  it("gated rows stay in the arrow order and Enter runs the action instead of selecting", () => {
    const spies = renderPicker(infos({ glm: { gate: REASONS[0]![0] } }));
    toRoot();
    // Root: Create profile, Claude, GLM (gated), Kimi.
    fireEvent.keyDown(search(), { key: "ArrowDown" });
    fireEvent.keyDown(search(), { key: "ArrowDown" });
    expect(screen.getByTestId("model-provider-glm")).toHaveAttribute(
      "data-active",
      "true",
    );
    fireEvent.keyDown(search(), { key: "Enter" });
    expect(spies.onOpenProviderSettings).toHaveBeenCalledExactlyOnceWith("glm");
    expect(screen.queryByTestId("model-row-glm:glm-5")).not.toBeInTheDocument();
  });

  it("the gate action is a tab stop only on the active row", () => {
    renderPicker(infos({ glm: { gate: REASONS[0]![0] } }));
    toRoot();
    expect(screen.getByTestId("model-gate-action-glm")).toHaveAttribute(
      "tabindex",
      "-1",
    );
    fireEvent.keyDown(search(), { key: "ArrowDown" });
    fireEvent.keyDown(search(), { key: "ArrowDown" });
    expect(screen.getByTestId("model-gate-action-glm")).toHaveAttribute(
      "tabindex",
      "0",
    );
  });

  it("the reason is linked to the row for screen readers", () => {
    renderPicker(infos({ glm: { gate: REASONS[0]![0] } }));
    toRoot();
    const row = screen.getByTestId("model-provider-glm");
    const reason = within(row).getByTestId("model-gate-glm");
    expect(row.getAttribute("aria-describedby")).toBe(reason.id);
  });
});

describe("picker gates: trigger and profiles", () => {
  it("the trigger warns when the selected model is gated (advisory too)", () => {
    renderPicker(
      infos({
        claude: {
          gate: { reason: "out_of_plan", until: FUTURE, advisory: true },
        },
      }),
    );
    const trigger = screen.getByTestId("composer-model-picker");
    expect(trigger).toHaveAttribute("data-gated", "out_of_plan");
    expect(trigger.getAttribute("title")).toContain("Plan used up");
    expect(
      within(trigger).getByTestId("model-trigger-gate-dot"),
    ).toBeInTheDocument();
  });

  it("no gate, no marker on the trigger", () => {
    renderPicker(infos());
    expect(screen.getByTestId("composer-model-picker")).not.toHaveAttribute(
      "data-gated",
    );
  });

  it("a profile on a hard-gated provider is not applied; its action runs", () => {
    const profile: PickerProfileRow = {
      id: "2",
      name: "Coder",
      summary: "GLM",
      provider: "glm",
      model: "glm-5",
    };
    const spies = renderPicker(infos({ glm: { gate: REASONS[0]![0] } }), {
      profileRows: [profile],
      baseAgentId: null,
    });
    toRoot();
    const row = screen.getByTestId("model-profile-row-2");
    expect(row).toHaveAttribute("aria-disabled", "true");
    fireEvent.click(row);
    expect(spies.onApplyProfile).not.toHaveBeenCalled();
  });
});
