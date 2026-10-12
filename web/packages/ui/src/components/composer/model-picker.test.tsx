import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi, type Mock } from "vitest";

import { ModelPicker, type ModelPickerProps, type PickerProfileRow } from "@/components/composer/model-picker";
import { buildBrowserProviders, type BrowserProvider, type ProviderHealth } from "@/lib/model-browser";
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
  { id: "1", name: "Reviewer", summary: "Claude Code · Sonnet 5.5", provider: "claude-native", model: "sonnet-5-5" },
  { id: "2", name: "Coder", summary: "GLM · GLM 5.3", provider: "glm", model: "glm-5.3" },
];

interface Spies {
  onSelect: Mock<ModelPickerProps["onSelect"]>;
  onApplyProfile: Mock<ModelPickerProps["onApplyProfile"]>;
  onClearAgent: Mock<ModelPickerProps["onClearAgent"]>;
  onManageAgents: Mock<ModelPickerProps["onManageAgents"]>;
  onCreateProfile: Mock<NonNullable<ModelPickerProps["onCreateProfile"]>>;
  onEditProfile: Mock<NonNullable<ModelPickerProps["onEditProfile"]>>;
  onOpenProviderSettings: Mock<NonNullable<ModelPickerProps["onOpenProviderSettings"]>>;
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
    onCreateProfile: vi.fn(),
    onEditProfile: vi.fn(),
    onOpenProviderSettings: vi.fn(),
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
      currentConfig={{ permissionMode: "plan", autoAccept: true, thinkingLevel: "high" }}
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
  it("trigger shows the model label with the provider in the title", () => {
    renderPicker();
    const trigger = screen.getByTestId("composer-model-picker");
    expect(trigger).toHaveTextContent("Opus 5.5");
    expect(trigger).not.toHaveTextContent("Claude Code");
    expect(within(trigger).getByTestId("provider-glyph")).toBeInTheDocument();
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
    // "Default model" is the first model row, below the profiles band.
    const modelRows = screen.getAllByRole("option").filter((r) => r.dataset.testid?.startsWith("model-row-"));
    expect(modelRows[0]).toHaveTextContent("Default model");
    expect(screen.queryByTestId("model-row-glm:glm-5.3")).not.toBeInTheDocument();
    expect(screen.getByTestId("model-picker-back")).toBeInTheDocument();
  });

  it("opens on the current provider with profiles configured", () => {
    renderPicker({ profileRows: PROFILES, baseAgentId: "1", selectedProvider: "claude-native" });
    open();
    expect(screen.getByTestId("model-picker-back")).toBeInTheDocument();
    expect(screen.getByTestId("model-row-claude-native:opus-5-5")).toBeInTheDocument();
  });

  it("Back shows Profiles and Providers with model counts", () => {
    renderPicker({ profileRows: PROFILES, baseAgentId: "1", selectedProvider: "claude-native" });
    open();
    fireEvent.click(screen.getByTestId("model-picker-back"));
    expect(screen.getByText("Profiles")).toBeInTheDocument();
    expect(screen.getByText("Providers")).toBeInTheDocument();
    // Two catalog models each; the synthetic default row does not count.
    expect(within(screen.getByTestId("model-provider-claude-native")).getByText("2 models")).toBeInTheDocument();
    expect(within(screen.getByTestId("model-provider-glm")).getByText("2 models")).toBeInTheDocument();
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
    // Provider view: Create profile (0), Default (1), Opus (2), Sonnet (3). The highlight starts on the current selection.
    expect(screen.getByTestId("model-row-claude-native:opus-5-5")).toHaveAttribute("data-active", "true");
    fireEvent.keyDown(searchInput(), { key: "ArrowDown" });
    expect(screen.getByTestId("model-row-claude-native:sonnet-5-5")).toHaveAttribute("data-active", "true");
    expect(searchInput()).toHaveAttribute(
      "aria-activedescendant",
      screen.getByTestId("model-row-claude-native:sonnet-5-5").id,
    );
    fireEvent.keyDown(searchInput(), { key: "ArrowDown" }); // wraps to Create profile
    expect(screen.getByTestId("model-profiles-empty")).toHaveAttribute("data-active", "true");
    fireEvent.keyDown(searchInput(), { key: "ArrowDown" });
    expect(screen.getByTestId("model-row-claude-native:")).toHaveAttribute("data-active", "true");
    fireEvent.keyDown(searchInput(), { key: "ArrowUp" });
    fireEvent.keyDown(searchInput(), { key: "ArrowUp" }); // wraps to Sonnet
    fireEvent.keyDown(searchInput(), { key: "Enter" });
    expect(onSelect).toHaveBeenCalledExactlyOnceWith("claude-native", "sonnet-5-5");
    expect(screen.queryByTestId("model-search-input")).not.toBeInTheDocument();

    open();
    fireEvent.keyDown(searchInput(), { key: "ArrowLeft" });
    expect(screen.getByText("Providers")).toBeInTheDocument();
    // Enter on a provider drills in (Create profile, Claude, GLM).
    fireEvent.keyDown(searchInput(), { key: "ArrowDown" });
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

  it("profile rows apply, No agent detaches, the pencil opens settings", () => {
    const spies = renderPicker({ profileRows: PROFILES, baseAgentId: "1", custom: true });
    open();
    fireEvent.click(screen.getByTestId("model-picker-back"));
    const applied = screen.getByTestId("model-profile-row-1");
    expect(applied).toHaveTextContent("custom");
    expect(screen.getByTestId("model-profile-row-2")).not.toHaveTextContent("custom");
    fireEvent.click(screen.getByTestId("model-profile-row-2"));
    expect(spies.onApplyProfile).toHaveBeenCalledExactlyOnceWith("2");

    open();
    fireEvent.click(screen.getByTestId("model-no-agent"));
    expect(spies.onClearAgent).toHaveBeenCalledOnce();

    open();
    fireEvent.click(screen.getByTestId("model-profiles-edit"));
    expect(spies.onManageAgents).toHaveBeenCalledOnce();
    expect(screen.queryByTestId("model-manage-agents")).not.toBeInTheDocument();
  });

  it("popover height follows the row count", () => {
    // Claude view: 3 model rows + heading + create row + models heading = 6 -> 80 + 240.
    renderPicker();
    open();
    const height = () => screen.getByTestId("model-picker-popover").getAttribute("style") ?? "";
    expect(height()).toContain("min(320px, var(--radix-popover-content-available-height))");
  });

  it("popover height grows with profiles in the view", () => {
    const two = [
      { id: "1", name: "A", summary: "", provider: "claude-native", model: "opus-5-5" },
      { id: "2", name: "B", summary: "", provider: "claude-native", model: "sonnet-5-5" },
    ];
    renderPicker({ profileRows: two });
    open();
    // heading + 2 profiles + models heading = 4 extra -> 7 rows -> 360.
    expect(screen.getByTestId("model-picker-popover").getAttribute("style")).toContain("min(360px,");
  });

  it("provider view shows the provider's profiles above its models", () => {
    renderPicker({ profileRows: PROFILES });
    open();
    const band = screen.getByTestId("model-profiles-band");
    expect(within(band).getByTestId("model-profile-row-1")).toBeInTheDocument();
    expect(within(band).queryByTestId("model-profile-row-2")).not.toBeInTheDocument();
    expect(band.compareDocumentPosition(screen.getByTestId("model-row-claude-native:opus-5-5"))).toBe(
      Node.DOCUMENT_POSITION_FOLLOWING,
    );
    expect(screen.queryByTestId("model-profiles-empty")).not.toBeInTheDocument();
    // Searching hides the band.
    fireEvent.change(searchInput(), { target: { value: "opus" } });
    expect(screen.queryByTestId("model-profiles-band")).not.toBeInTheDocument();
  });

  it("root shows every profile under a Profiles heading with an edit pencil", () => {
    renderPicker({ profileRows: PROFILES });
    open();
    fireEvent.click(screen.getByTestId("model-picker-back"));
    const band = screen.getByTestId("model-profiles-band");
    expect(within(band).getByText("Profiles")).toBeInTheDocument();
    expect(within(band).getByTestId("model-profile-row-1")).toBeInTheDocument();
    expect(within(band).getByTestId("model-profile-row-2")).toBeInTheDocument();
    const pencil = within(band).getByTestId("model-profiles-edit");
    expect(pencil).toHaveAttribute("aria-label", "Edit profiles");
    expect(screen.getByText("Providers")).toBeInTheDocument();
  });

  it("no profiles shows a Create profile row", () => {
    const { onCreateProfile } = renderPicker();
    open();
    const empty = screen.getByTestId("model-profiles-empty");
    expect(empty).toHaveTextContent("Create profile");
    fireEvent.click(empty);
    expect(onCreateProfile).toHaveBeenCalledOnce();
  });

  it("model row + creates a profile seeded with provider and model", () => {
    const { onCreateProfile, onSelect } = renderPicker();
    open();
    const plus = screen.getByTestId("model-create-profile-claude-native-sonnet-5-5");
    expect(plus).toHaveAttribute("aria-label", "Create profile from this model");
    fireEvent.click(plus);
    expect(onCreateProfile).toHaveBeenCalledExactlyOnceWith({
      name: "Sonnet 5.5",
      provider: "claude-native",
      model: "sonnet-5-5",
    });
    expect(onSelect).not.toHaveBeenCalled();
    expect(screen.queryByTestId("model-search-input")).not.toBeInTheDocument();
    // The synthetic default row offers no action.
    open();
    expect(screen.queryByTestId("model-create-profile-claude-native-")).not.toBeInTheDocument();
  });

  it("the selected model's seed carries mode and thinking", () => {
    const { onCreateProfile } = renderPicker();
    open();
    fireEvent.click(screen.getByTestId("model-create-profile-claude-native-opus-5-5"));
    expect(onCreateProfile).toHaveBeenCalledExactlyOnceWith({
      name: "Opus 5.5",
      provider: "claude-native",
      model: "opus-5-5",
      permissionMode: "plan",
      autoAccept: true,
      thinkingLevel: "high",
    });
  });

  it("model row with one profile offers edit, with several offers the list", () => {
    const profiles: PickerProfileRow[] = [
      { id: "1", name: "Reviewer", summary: "", provider: "claude-native", model: "sonnet-5-5" },
      { id: "2", name: "Solo", summary: "", provider: "claude-native", model: "opus-5-5" },
      { id: "3", name: "Other", summary: "", provider: "claude-native", model: "opus-5-5" },
    ];
    const { onEditProfile, onManageAgents } = renderPicker({ profileRows: profiles });
    open();
    const edit = screen.getByTestId("model-edit-profile-claude-native-sonnet-5-5");
    expect(edit).toHaveAttribute("aria-label", "Edit profile Reviewer");
    expect(within(edit).getByTestId("profile-glyph")).toBeInTheDocument();
    fireEvent.click(edit);
    expect(onEditProfile).toHaveBeenCalledExactlyOnceWith("1");

    open();
    const list = screen.getByTestId("model-edit-profiles-claude-native-opus-5-5");
    expect(list).toHaveAttribute("aria-label", "Edit profiles (2)");
    fireEvent.click(list);
    expect(onManageAgents).toHaveBeenCalledOnce();
  });

  it("row action buttons keep search focus and never leave the arrow walk", () => {
    renderPicker();
    open();
    const plus = screen.getByTestId("model-create-profile-claude-native-sonnet-5-5");
    expect(plus).toHaveAttribute("tabindex", "-1");
    expect(fireEvent.mouseDown(plus)).toBe(false);
    expect(searchInput()).toHaveFocus();
  });

  it("provider gear opens provider settings", () => {
    const { onOpenProviderSettings } = renderPicker();
    open();
    const gear = screen.getByTestId("model-picker-provider-settings");
    expect(gear).toHaveAttribute("aria-label", "Open Claude Code settings");
    fireEvent.click(gear);
    expect(onOpenProviderSettings).toHaveBeenCalledExactlyOnceWith("claude-native");
    // The root header has no gear.
    open();
    fireEvent.click(screen.getByTestId("model-picker-back"));
    expect(screen.queryByTestId("model-picker-provider-settings")).not.toBeInTheDocument();
  });

  it("provider rows show glyph, count, loading and error states", () => {
    const providers: BrowserProvider[] = [
      ...buildBrowserProviders([INFOS[0]!], {}),
      ...buildBrowserProviders([INFOS[1]!], {}, { loading: true }),
      { id: "kimi", label: "Kimi", modelSelection: { kind: "error", message: "boom" } },
      ...buildBrowserProviders([{ id: "pi" as ProviderInfo["id"], label: "Pi" }], {}),
    ];
    renderPicker({ providers, selectedProvider: "none" });
    open();
    const claude = screen.getByTestId("model-provider-claude-native");
    expect(within(claude).getByTestId("provider-glyph")).toBeInTheDocument();
    expect(claude).toHaveTextContent("2 models");
    expect(screen.getByTestId("model-provider-glm")).toHaveTextContent("Loading…");
    expect(screen.getByTestId("model-provider-kimi")).toHaveTextContent("Error");
    expect(screen.getByTestId("model-provider-pi")).toHaveTextContent("Default model");

    // Provider view bodies.
    fireEvent.click(screen.getByTestId("model-provider-glm"));
    expect(screen.getByTestId("model-provider-loading")).toHaveTextContent("Loading…");
    fireEvent.click(screen.getByTestId("model-picker-back"));
    fireEvent.click(screen.getByTestId("model-provider-kimi"));
    expect(screen.getByTestId("model-provider-error")).toHaveTextContent("boom");
  });

  it("search ranks with scoreTextFields and breaks ties by label", () => {
    const providers = buildBrowserProviders(
      [
        {
          id: "claude-native" as ProviderInfo["id"],
          label: "P",
          models: [
            { id: "m1", label: "Zed Pro" },
            { id: "m2", label: "Amy Pro" },
            { id: "m3", label: "Pro" },
          ],
        },
        { id: "glm" as ProviderInfo["id"], label: "Q", models: [] },
      ],
      {},
    );
    renderPicker({ providers, selectedProvider: "none" });
    open();
    fireEvent.change(searchInput(), { target: { value: "pro" } });
    const order = screen.getAllByRole("option").map((r) => r.dataset.testid);
    expect(order).toEqual([
      "model-row-claude-native:m3",
      "model-row-claude-native:m2",
      "model-row-claude-native:m1",
    ]);
  });

  it("sheet does not autofocus search and shows all rows on focus", () => {
    mobile.value = true;
    renderPicker({ selectedProvider: "none", profileRows: PROFILES });
    open();
    expect(searchInput()).not.toHaveFocus();
    expect(screen.getByText("Providers")).toBeInTheDocument();
    expect(screen.getByTestId("model-picker-sheet").className).toContain("data-[side=bottom]:h-[80dvh]");

    act(() => searchInput().focus());
    expect(screen.queryByText("Providers")).not.toBeInTheDocument();
    expect(screen.queryByTestId("model-profiles-band")).not.toBeInTheDocument();
    expect(screen.getByTestId("model-row-claude-native:opus-5-5")).toBeInTheDocument();
    expect(screen.getByTestId("model-row-glm:glm-5.3")).toBeInTheDocument();

    act(() => searchInput().blur());
    expect(screen.getByText("Providers")).toBeInTheDocument();
  });

  it("sheet focuses search on open only with a fine pointer", () => {
    mobile.value = true;
    const original = window.matchMedia;
    window.matchMedia = ((q: string) => ({ matches: q === "(pointer: fine)" })) as typeof window.matchMedia;
    try {
      renderPicker({ selectedProvider: "none" });
      open();
      expect(searchInput()).toHaveFocus();
    } finally {
      window.matchMedia = original;
    }
  });

  it("a narrow window with a fine pointer keeps the provider list while the search is focused", () => {
    mobile.value = true;
    const original = window.matchMedia;
    window.matchMedia = ((q: string) => ({ matches: q === "(pointer: fine)" })) as typeof window.matchMedia;
    try {
      renderPicker({ selectedProvider: "none", profileRows: PROFILES });
      open();
      expect(searchInput()).toHaveFocus();
      expect(screen.getByText("Providers")).toBeInTheDocument();
      expect(screen.getByTestId("model-provider-glm")).toBeInTheDocument();
    } finally {
      window.matchMedia = original;
    }
  });

  it("settings buttons are tab stops, row actions are not, and the action reveal is fine-pointer only", () => {
    renderPicker();
    open();
    expect(screen.getByTestId("model-profiles-edit")).not.toHaveAttribute("tabindex");
    expect(screen.getByTestId("model-picker-provider-settings")).not.toHaveAttribute("tabindex");
    const plus = screen.getByTestId("model-create-profile-claude-native-sonnet-5-5");
    expect(plus).toHaveAttribute("tabindex", "-1");
    expect(plus.className).toContain("pointer-fine:opacity-0");
    expect(fireEvent.mouseDown(screen.getByTestId("model-profiles-edit"))).toBe(false);
  });

  it("Back keeps the search input focused for arrows and typing", () => {
    renderPicker();
    open();
    const back = screen.getByTestId("model-picker-back");
    expect(fireEvent.mouseDown(back)).toBe(false);
    expect(fireEvent.pointerDown(back)).toBe(false);
    act(() => screen.getByTestId("model-profiles-edit").focus());
    fireEvent.click(back);
    expect(searchInput()).toHaveFocus();
  });

  it("an empty result while a provider is loading says Loading…, not no matches", () => {
    const providers = [
      ...buildBrowserProviders([INFOS[0]!], {}),
      ...buildBrowserProviders([INFOS[1]!], {}, { loading: true }),
    ];
    renderPicker({ providers, selectedProvider: "none" });
    open();
    fireEvent.change(searchInput(), { target: { value: "zzzqq" } });
    expect(screen.getByTestId("model-provider-loading")).toBeInTheDocument();
    expect(screen.queryByTestId("model-search-empty")).not.toBeInTheDocument();
  });

  it("a focused empty sheet search with every provider loading says Loading…", () => {
    mobile.value = true;
    renderPicker({ providers: buildBrowserProviders(INFOS, {}, { loading: true }), selectedProvider: "none" });
    open();
    act(() => searchInput().focus());
    expect(screen.getByTestId("model-provider-loading")).toBeInTheDocument();
    expect(screen.queryByTestId("model-search-empty")).not.toBeInTheDocument();
  });

  it("an unavailable provider's view shows its reason and keeps models selectable", () => {
    const { onSelect } = renderPicker({ health: { "claude-native": { ok: false, detail: "claude CLI not found" } } });
    open();
    expect(screen.getByTestId("model-provider-unavailable")).toHaveTextContent("claude CLI not found");
    fireEvent.click(screen.getByTestId("model-row-claude-native:sonnet-5-5"));
    expect(onSelect).toHaveBeenCalledWith("claude-native", "sonnet-5-5");
  });

  it("Create profile in a provider view seeds that provider; at the root it opens Agents", () => {
    const { onCreateProfile, onManageAgents } = renderPicker();
    open();
    fireEvent.click(screen.getByTestId("model-profiles-empty"));
    expect(onCreateProfile).toHaveBeenCalledExactlyOnceWith({ name: "", provider: "claude-native", model: "" });
    expect(onManageAgents).not.toHaveBeenCalled();
    open();
    fireEvent.click(screen.getByTestId("model-picker-back"));
    fireEvent.click(screen.getByTestId("model-profiles-empty"));
    expect(onManageAgents).toHaveBeenCalledOnce();
  });

  it("popover height counts the bound-chat note", () => {
    renderPicker({ boundProvider: "glm", selectedProvider: "glm", selectedModel: "" });
    open();
    // 3 glm rows + heading + create + models heading + note = 7 -> 360.
    expect(screen.getByTestId("model-picker-popover").getAttribute("style")).toContain("min(360px,");
  });

  it("desktop autofocuses search", () => {
    renderPicker();
    open();
    expect(searchInput()).toHaveFocus();
  });

  it("tapping a row while the search input is focused selects it", () => {
    mobile.value = true;
    const { onSelect } = renderPicker({ selectedProvider: "none" });
    open();
    act(() => searchInput().focus());
    const row = screen.getByTestId("model-row-glm:glm-5.3");
    // The press is cancelled so the input keeps focus and the list does not re-render under the tap.
    expect(fireEvent.pointerDown(row)).toBe(false);
    expect(fireEvent.mouseDown(row)).toBe(false);
    expect(searchInput()).toHaveFocus();
    fireEvent.click(row);
    expect(onSelect).toHaveBeenCalledExactlyOnceWith("glm", "glm-5.3");
  });

  it("keyboard walk skips headings, disabled rows and row action buttons", () => {
    renderPicker({
      selectedProvider: "none",
      health: { glm: { ok: false, detail: "missing" } },
    });
    open();
    // Root actionable rows: Create profile, Claude Code (GLM is disabled).
    expect(screen.getByTestId("model-profiles-empty")).toHaveAttribute("data-active", "true");
    fireEvent.keyDown(searchInput(), { key: "ArrowDown" });
    expect(screen.getByTestId("model-provider-claude-native")).toHaveAttribute("data-active", "true");
    fireEvent.keyDown(searchInput(), { key: "ArrowDown" });
    expect(screen.getByTestId("model-profiles-empty")).toHaveAttribute("data-active", "true");

    // In a provider view the model action buttons are not walked either.
    fireEvent.keyDown(searchInput(), { key: "ArrowDown" });
    fireEvent.keyDown(searchInput(), { key: "Enter" });
    const walked = new Set<string | null>();
    for (let i = 0; i < 6; i++) {
      walked.add(searchInput().getAttribute("aria-activedescendant"));
      fireEvent.keyDown(searchInput(), { key: "ArrowDown" });
    }
    const buttonIds = screen.getAllByRole("button", { hidden: true }).map((b) => b.id);
    expect([...walked].some((id) => id && buttonIds.includes(id))).toBe(false);
    // Create profile + Default + 2 models = 4 distinct stops.
    expect(walked.size).toBe(4);
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
