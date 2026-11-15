// The desktop popover's provider rail (chat-surface-ui E-b, decision 6) wired into the picker.
import { fireEvent, render, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { ProviderModelPicker, type PickerProfileRow, type ProviderModelPickerProps } from "@/components/pickers/provider-model-picker";
import { buildBrowserProviders, type ProviderHealth } from "@/lib/model-browser";
import type { ProviderInfo } from "@/lib/types";

const mobile = vi.hoisted(() => ({ value: false }));
vi.mock("@/hooks/use-mobile", () => ({ useIsMobile: () => mobile.value }));

const many = (n: number) => Array.from({ length: n }, (_, i) => ({ id: `m${i}`, label: `Model ${i}` }));
const INFOS: ProviderInfo[] = [
  { id: "claude-native", label: "Claude Code", models: [{ id: "opus", label: "Opus" }, { id: "sonnet", label: "Sonnet" }] },
  { id: "glm", label: "GLM", models: [{ id: "glm-5.3", label: "GLM 5.3" }] },
  { id: "kimi", label: "Kimi", models: many(30) },
];
const PROFILES: PickerProfileRow[] = [
  { id: "1", name: "Reviewer", summary: "Claude Code · Sonnet", provider: "claude-native", model: "sonnet" },
  { id: "2", name: "Coder", summary: "GLM · GLM 5.3", provider: "glm", model: "glm-5.3" },
];

function renderPicker(over: Partial<ProviderModelPickerProps> & { health?: Record<string, ProviderHealth> } = {}) {
  const { health, ...rest } = over;
  const onSelect = vi.fn();
  render(
    <ProviderModelPicker
      providers={buildBrowserProviders(INFOS, health ?? {})}
      selectedProvider="claude-native"
      selectedModel="opus"
      boundProvider={null}
      disabled={false}
      onSelect={onSelect}
      {...rest}
    />,
  );
  fireEvent.click(screen.getByTestId("composer-model-picker"));
  return onSelect;
}

const search = () => screen.getByTestId("model-search-input") as HTMLInputElement;

beforeEach(() => {
  mobile.value = false;
});

describe("desktop popover with the provider rail", () => {
  it("picker-lists-providers-in-a-rail: one tab per provider, the current one selected, no Back and no Providers list", () => {
    renderPicker();
    const rail = screen.getByTestId("model-picker-rail");
    const tabs = within(rail).getAllByRole("tab");
    expect(tabs.map((t) => t.getAttribute("data-testid"))).toEqual(["model-provider-claude-native", "model-provider-glm", "model-provider-kimi"]);
    expect(screen.getByTestId("model-provider-claude-native")).toHaveAttribute("aria-selected", "true");
    expect(screen.getByTestId("model-provider-glm")).toHaveAttribute("aria-selected", "false");
    expect(screen.queryByTestId("model-picker-back")).not.toBeInTheDocument();
    expect(screen.queryByText("Providers")).not.toBeInTheDocument();
    // the right column is the selected provider's models
    expect(screen.getByTestId("model-row-claude-native:opus")).toBeInTheDocument();
    expect(screen.queryByTestId("model-row-glm:glm-5.3")).not.toBeInTheDocument();
  });

  it("a rail tab switches the models column and a model pick selects provider and model", () => {
    const onSelect = renderPicker();
    fireEvent.click(screen.getByTestId("model-provider-glm"));
    expect(screen.getByTestId("model-provider-glm")).toHaveAttribute("aria-selected", "true");
    expect(screen.queryByTestId("model-row-claude-native:opus")).not.toBeInTheDocument();
    fireEvent.click(screen.getByTestId("model-row-glm:glm-5.3"));
    expect(onSelect).toHaveBeenCalledWith("glm", "glm-5.3");
  });

  it("typing searches every provider and the rail keeps its selection", () => {
    renderPicker();
    fireEvent.change(search(), { target: { value: "glm" } });
    expect(screen.getByTestId("model-row-glm:glm-5.3")).toBeInTheDocument();
    expect(screen.getByTestId("model-provider-claude-native")).toHaveAttribute("aria-selected", "true");
    fireEvent.change(search(), { target: { value: "" } });
    expect(screen.queryByTestId("model-row-glm:glm-5.3")).not.toBeInTheDocument();
  });

  it("picker-rail-is-keyboard-operable: Left on an empty search steps into the rail, Arrow keys select the neighbour", () => {
    renderPicker();
    expect(search()).toHaveFocus();
    fireEvent.keyDown(search(), { key: "ArrowLeft" });
    expect(screen.getByTestId("model-provider-claude-native")).toHaveFocus();
    fireEvent.keyDown(screen.getByTestId("model-provider-claude-native"), { key: "ArrowDown" });
    expect(screen.getByTestId("model-provider-glm")).toHaveAttribute("aria-selected", "true");
    expect(screen.getByTestId("model-provider-glm")).toHaveFocus();
    expect(screen.getByTestId("model-row-glm:glm-5.3")).toBeInTheDocument();
    // a non-empty query keeps Left for the caret
    fireEvent.change(search(), { target: { value: "g" } });
    search().focus();
    fireEvent.keyDown(search(), { key: "ArrowLeft" });
    expect(search()).toHaveFocus();
  });

  it("picker-provider-without-account-says-why: an unavailable provider keeps its tab with a warning dot and its view gives the reason, not its models", () => {
    renderPicker({ health: { glm: { ok: false, detail: '"glm" not found on PATH' } } });
    const tab = screen.getByTestId("model-provider-glm");
    expect(tab).toHaveAttribute("data-unavailable", "true");
    expect(tab.getAttribute("aria-label")).toContain('"glm" not found on PATH');
    expect(screen.getByTestId("model-provider-warning-glm")).toBeInTheDocument();
    fireEvent.click(tab);
    expect(screen.getByTestId("model-provider-unavailable")).toHaveTextContent('"glm" not found on PATH');
    expect(screen.queryByTestId("model-row-glm:glm-5.3")).not.toBeInTheDocument(); // cannot be switched to
  });

  it("the current provider stays pickable even when it is unavailable", () => {
    const onSelect = renderPicker({ health: { "claude-native": { ok: false, detail: "claude CLI not found" } } });
    expect(screen.getByTestId("model-provider-unavailable")).toHaveTextContent("claude CLI not found");
    fireEvent.click(screen.getByTestId("model-row-claude-native:sonnet"));
    expect(onSelect).toHaveBeenCalledWith("claude-native", "sonnet");
  });

  it("the Profiles tab lists every profile across providers", () => {
    const onApplyProfile = vi.fn();
    renderPicker({ profileRows: PROFILES, baseAgentId: null, onApplyProfile });
    fireEvent.click(screen.getByTestId("model-profiles-tab"));
    expect(screen.getByTestId("model-profiles-tab")).toHaveAttribute("aria-selected", "true");
    expect(screen.getByText("Reviewer")).toBeInTheDocument();
    expect(screen.getByText("Coder")).toBeInTheDocument();
    fireEvent.click(screen.getByText("Coder"));
    expect(onApplyProfile).toHaveBeenCalledWith("2");
  });

  it("picker-height-is-capped: a provider with many models never grows the popover past 400 px", () => {
    renderPicker({ selectedProvider: "kimi", selectedModel: "m0" });
    expect(screen.getByTestId("model-picker-popover").getAttribute("style")).toContain("min(400px,");
  });

  it("the popover is wider by the rail's width", () => {
    renderPicker();
    expect(screen.getByTestId("model-picker-popover").className).toContain("w-[25rem]");
  });

  it("a single provider (a bound chat) has no rail", () => {
    renderPicker({ boundProvider: "glm", selectedProvider: "glm", selectedModel: "" });
    expect(screen.queryByTestId("model-picker-rail")).not.toBeInTheDocument();
    expect(screen.getByTestId("model-picker-popover").className).toContain("w-[22rem]");
  });

  it("the mobile sheet keeps its root list and Back instead of the rail", () => {
    mobile.value = true;
    renderPicker();
    expect(screen.queryByTestId("model-picker-rail")).not.toBeInTheDocument();
    expect(screen.getByTestId("model-picker-back")).toBeInTheDocument();
  });
});
