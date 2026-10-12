import { fireEvent, render, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi, type Mock } from "vitest";

import { ModelPicker, type ModelPickerProps, type PickerProfileRow } from "@/components/composer/model-picker";
import { buildBrowserProviders, type ProviderHealth } from "@/lib/model-browser";
import type { ProviderInfo } from "@/lib/types";

const mobile = vi.hoisted(() => ({ value: false }));
vi.mock("@/hooks/use-mobile", () => ({ useIsMobile: () => mobile.value }));

const INFOS: ProviderInfo[] = [
  {
    id: "claude-native",
    label: "Claude Code",
    models: [
      { id: "opus-5-5", label: "Opus 5.5", description: "Most capable" },
      { id: "sonnet-5-5", label: "Sonnet 5.5", description: "Balanced" },
    ],
  },
  {
    id: "glm",
    label: "GLM",
    models: [
      { id: "glm-5.3", label: "GLM 5.3", description: "Flagship" },
      { id: "glm-4.6", label: "GLM 4.6" },
    ],
  },
];

const PROFILES: PickerProfileRow[] = [
  { id: "1", name: "Reviewer", summary: "Claude Code · Opus 5.5", provider: "claude-native" },
  { id: "2", name: "Coder", summary: "GLM · GLM 5.3", provider: "glm" },
];

interface Spies {
  onSelect: Mock<ModelPickerProps["onSelect"]>;
  onApplyProfile: Mock<ModelPickerProps["onApplyProfile"]>;
  onClearAgent: Mock<ModelPickerProps["onClearAgent"]>;
  onManageAgents: Mock<ModelPickerProps["onManageAgents"]>;
  onOpen: Mock<() => void>;
}

type PickerOverrides = Partial<Omit<ModelPickerProps, keyof Spies>> & { health?: Record<string, ProviderHealth> };

function renderPicker(overrides: PickerOverrides = {}): Spies {
  const { health, ...rest } = overrides;
  const spies: Spies = {
    onSelect: vi.fn(),
    onApplyProfile: vi.fn(),
    onClearAgent: vi.fn(),
    onManageAgents: vi.fn(),
    onOpen: vi.fn(),
  };
  render(
    <ModelPicker
      providers={buildBrowserProviders(INFOS, health ?? {})}
      selectedProvider="claude-native"
      selectedModel="opus-5-5"
      boundProvider={null}
      disabled={false}
      profileRows={[]}
      baseAgentId={null}
      custom={false}
      {...spies}
      {...rest}
    />,
  );
  return spies;
}

function open(): void {
  fireEvent.click(screen.getByTestId("composer-model-picker"));
}

function searchInput(): HTMLInputElement {
  return screen.getByTestId("model-search-input") as HTMLInputElement;
}

beforeEach(() => {
  mobile.value = false;
});

describe("model picker", () => {
  it("trigger shows the provider and model and is labelled", () => {
    renderPicker();
    const trigger = screen.getByTestId("composer-model-picker");
    expect(trigger).toHaveTextContent("Claude Code · Opus 5.5");
    expect(trigger).toHaveAttribute("aria-label", "Model: Claude Code · Opus 5.5");
    expect(trigger).toHaveAttribute("title", "Claude Code · Opus 5.5");
  });

  it("opens in the current provider's view with the current model checked", () => {
    renderPicker();
    open();
    expect(screen.getByText("Claude Code")).toBeInTheDocument();
    const current = screen.getByTestId("model-row-claude-native:opus-5-5");
    expect(current).toHaveAttribute("aria-selected", "true");
    expect(within(current).getByText("Opus 5.5")).toBeInTheDocument();
    expect(screen.getByTestId("model-row-claude-native:sonnet-5-5")).toHaveAttribute("aria-selected", "false");
    // "Default model" is always first.
    const rows = screen.getAllByRole("option");
    expect(rows[0]).toHaveTextContent("Default model");
    expect(screen.queryByTestId("model-row-glm:glm-5.3")).not.toBeInTheDocument();
    expect(screen.getByTestId("model-picker-back")).toBeInTheDocument();
  });

  it("Back shows Profiles and Providers with model counts", () => {
    renderPicker({ profileRows: PROFILES, baseAgentId: "1", selectedProvider: "claude-native" });
    open();
    // With profiles the picker opens at the root.
    expect(screen.queryByTestId("model-picker-back")).not.toBeInTheDocument();
    fireEvent.click(screen.getByTestId("model-provider-glm"));
    fireEvent.click(screen.getByTestId("model-picker-back"));
    expect(screen.getByText("Profiles")).toBeInTheDocument();
    expect(screen.getByText("Providers")).toBeInTheDocument();
    // Default row + two catalog models each.
    expect(within(screen.getByTestId("model-provider-claude-native")).getByText("3 models")).toBeInTheDocument();
    expect(within(screen.getByTestId("model-provider-glm")).getByText("3 models")).toBeInTheDocument();
    expect(screen.getByTestId("model-profile-row-1")).toHaveAttribute("aria-selected", "true");
  });

  it("picking a model of another provider selects provider and model together", () => {
    const { onSelect } = renderPicker();
    open();
    fireEvent.click(screen.getByTestId("model-picker-back"));
    fireEvent.click(screen.getByTestId("model-provider-glm"));
    fireEvent.click(screen.getByTestId("model-row-glm:glm-5.3"));
    expect(onSelect).toHaveBeenCalledExactlyOnceWith("glm", "glm-5.3");
    expect(screen.queryByTestId("model-search-input")).not.toBeInTheDocument();
  });

  it("root search ranks models across providers", () => {
    const { onSelect } = renderPicker();
    open();
    fireEvent.click(screen.getByTestId("model-picker-back"));
    fireEvent.change(searchInput(), { target: { value: "GLM 5" } });
    const rows = screen.getAllByRole("option");
    expect(rows[0]).toHaveAttribute("data-testid", "model-row-glm:glm-5.3");
    expect(rows[0]).toHaveTextContent("GLM · Flagship");
    expect(screen.queryByText("Providers")).not.toBeInTheDocument();
    fireEvent.click(rows[0]!);
    expect(onSelect).toHaveBeenCalledWith("glm", "glm-5.3");
  });

  it("search with no matches says so", () => {
    renderPicker();
    open();
    fireEvent.click(screen.getByTestId("model-picker-back"));
    fireEvent.change(searchInput(), { target: { value: "zzzqq" } });
    expect(screen.getByTestId("model-search-empty")).toHaveTextContent("No models match “zzzqq”");
    expect(screen.queryAllByRole("option")).toHaveLength(0);
  });

  it("an unhealthy provider is disabled with its reason and cannot be entered", () => {
    renderPicker({ health: { glm: { ok: false, detail: "glm CLI not found on PATH" } } });
    open();
    fireEvent.click(screen.getByTestId("model-picker-back"));
    const row = screen.getByTestId("model-provider-glm");
    expect(row).toHaveAttribute("aria-disabled", "true");
    expect(row).toHaveTextContent("glm CLI not found on PATH");
    fireEvent.click(row);
    expect(screen.getByText("Providers")).toBeInTheDocument();
    expect(screen.queryByTestId("model-row-glm:glm-5.3")).not.toBeInTheDocument();
    // Unavailable providers are also left out of cross-provider search.
    fireEvent.change(searchInput(), { target: { value: "glm" } });
    expect(screen.queryByTestId("model-row-glm:glm-5.3")).not.toBeInTheDocument();
  });

  it("a bound chat shows only its provider and no back button", () => {
    renderPicker({ boundProvider: "glm", selectedProvider: "glm", selectedModel: "" });
    open();
    expect(screen.queryByTestId("model-picker-back")).not.toBeInTheDocument();
    expect(screen.getByText("Bound to GLM — start a new chat to switch providers")).toBeInTheDocument();
    expect(screen.getByTestId("model-row-glm:glm-4.6")).toBeInTheDocument();
    expect(screen.queryByTestId("model-row-claude-native:opus-5-5")).not.toBeInTheDocument();
    // Backspace on an empty query must not escape to the root either.
    fireEvent.keyDown(searchInput(), { key: "Backspace" });
    expect(screen.getByTestId("model-row-glm:glm-4.6")).toBeInTheDocument();
  });

  it("Arrow keys and Enter select, Left goes back, Escape closes", () => {
    const { onSelect } = renderPicker();
    open();
    expect(searchInput()).toHaveFocus();
    // Provider view: Default (0), Opus (1), Sonnet (2).
    fireEvent.keyDown(searchInput(), { key: "ArrowDown" });
    expect(screen.getByTestId("model-row-claude-native:opus-5-5")).toHaveAttribute("data-active", "true");
    expect(searchInput()).toHaveAttribute(
      "aria-activedescendant",
      screen.getByTestId("model-row-claude-native:opus-5-5").id,
    );
    fireEvent.keyDown(searchInput(), { key: "ArrowDown" });
    fireEvent.keyDown(searchInput(), { key: "ArrowDown" }); // wraps to Default
    expect(screen.getByTestId("model-row-claude-native:")).toHaveAttribute("data-active", "true");
    fireEvent.keyDown(searchInput(), { key: "ArrowUp" }); // wraps to Sonnet
    fireEvent.keyDown(searchInput(), { key: "Enter" });
    expect(onSelect).toHaveBeenCalledExactlyOnceWith("claude-native", "sonnet-5-5");
    expect(screen.queryByTestId("model-search-input")).not.toBeInTheDocument();

    open();
    fireEvent.keyDown(searchInput(), { key: "ArrowLeft" });
    expect(screen.getByText("Providers")).toBeInTheDocument();
    // Enter on a provider drills in.
    fireEvent.keyDown(searchInput(), { key: "ArrowDown" });
    fireEvent.keyDown(searchInput(), { key: "Enter" });
    expect(screen.getByTestId("model-row-glm:glm-5.3")).toBeInTheDocument();
    // A non-empty query keeps Backspace for editing.
    fireEvent.change(searchInput(), { target: { value: "g" } });
    fireEvent.keyDown(searchInput(), { key: "Backspace" });
    expect(screen.queryByText("Providers")).not.toBeInTheDocument();

    fireEvent.keyDown(searchInput(), { key: "Escape" });
    expect(screen.queryByTestId("model-search-input")).not.toBeInTheDocument();
  });

  it("renders a bottom sheet below the md breakpoint", () => {
    mobile.value = true;
    const { onSelect } = renderPicker();
    open();
    const sheet = screen.getByTestId("model-picker-sheet");
    expect(sheet).toHaveAttribute("data-side", "bottom");
    expect(screen.queryByTestId("model-picker-popover")).not.toBeInTheDocument();
    expect(screen.getByRole("dialog", { name: "Select model" })).toBe(sheet);
    fireEvent.click(within(sheet).getByTestId("model-row-claude-native:sonnet-5-5"));
    expect(onSelect).toHaveBeenCalledWith("claude-native", "sonnet-5-5");
  });

  it("profile rows apply, No agent detaches, Manage agents opens settings", () => {
    const spies = renderPicker({ profileRows: PROFILES, baseAgentId: "1", custom: true });
    open();
    const applied = screen.getByTestId("model-profile-row-1");
    expect(applied).toHaveTextContent("custom");
    expect(screen.getByTestId("model-profile-row-2")).not.toHaveTextContent("custom");
    fireEvent.click(screen.getByTestId("model-profile-row-2"));
    expect(spies.onApplyProfile).toHaveBeenCalledExactlyOnceWith("2");

    open();
    fireEvent.click(screen.getByTestId("model-no-agent"));
    expect(spies.onClearAgent).toHaveBeenCalledOnce();

    open();
    fireEvent.click(screen.getByTestId("model-manage-agents"));
    expect(spies.onManageAgents).toHaveBeenCalledOnce();
  });

  it("onOpen fires on every open", () => {
    const { onOpen } = renderPicker();
    expect(onOpen).not.toHaveBeenCalled();
    open();
    expect(onOpen).toHaveBeenCalledTimes(1);
    fireEvent.keyDown(searchInput(), { key: "Escape" });
    open();
    expect(onOpen).toHaveBeenCalledTimes(2);
  });
});
