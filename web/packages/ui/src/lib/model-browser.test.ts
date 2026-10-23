import { describe, expect, it } from "vitest";
import type { ModelInfo, ProviderInfo } from "@/lib/types";
import {
  buildBrowserProviders,
  getAllModelRows,
  getProviderModelCount,
  getProviderModelRows,
  groupProfilesByProviderModel,
  resolveDesktopFixedHeight,
  resolveInitialModelBrowserView,
  resolveModelBrowserAllView,
  resolveTriggerLabel,
  resolveTriggerTitle,
  type BrowserProvider,
} from "./model-browser";

function info(id: string, label: string | undefined, models?: ModelInfo[]): ProviderInfo {
  return { id: id as ProviderInfo["id"], label, models };
}

function model(id: string, label: string, extra?: Partial<ModelInfo>): ModelInfo {
  return { id, label, ...extra };
}

describe("model browser initial view", () => {
  const codex = buildBrowserProviders([info("codex", "Codex")], {})[0]!;
  const pi = buildBrowserProviders([info("pi", "Pi")], {})[0]!;

  it("opens a sole provider directly", () => {
    expect(
      resolveInitialModelBrowserView({
        providers: [pi],
        selectedProvider: "",
        selectedModel: "",
        hasProfiles: false,
      }),
    ).toEqual({ kind: "provider", providerId: "pi", providerLabel: "Pi" });
  });

  it("opens the selected provider when there is one", () => {
    expect(
      resolveInitialModelBrowserView({
        providers: [codex, pi],
        selectedProvider: "pi",
        selectedModel: "pi-pro",
        hasProfiles: false,
      }),
    ).toEqual({ kind: "provider", providerId: "pi", providerLabel: "Pi" });
  });

  it("opens the selected provider even for the default model", () => {
    expect(
      resolveInitialModelBrowserView({
        providers: [codex, pi],
        selectedProvider: "pi",
        selectedModel: "",
        hasProfiles: false,
      }),
    ).toEqual({ kind: "provider", providerId: "pi", providerLabel: "Pi" });
  });

  it("opens the selected provider when profiles exist", () => {
    expect(
      resolveInitialModelBrowserView({
        providers: [codex, pi],
        selectedProvider: "pi",
        selectedModel: "pi-pro",
        hasProfiles: true,
      }),
    ).toEqual({ kind: "provider", providerId: "pi", providerLabel: "Pi" });
  });

  it("falls back to the root when the provider is gone", () => {
    expect(
      resolveInitialModelBrowserView({
        providers: [codex, pi],
        selectedProvider: "gemini",
        selectedModel: "gemini-3",
        hasProfiles: false,
      }),
    ).toEqual({ kind: "all" });
  });

  it("opens a sole provider directly regardless of root content", () => {
    expect(
      resolveInitialModelBrowserView({
        providers: [pi],
        selectedProvider: "pi",
        selectedModel: "pi-pro",
        hasProfiles: true,
      }),
    ).toEqual({ kind: "provider", providerId: "pi", providerLabel: "Pi" });
  });
});

describe("desktop fixed height", () => {
  const providers = buildBrowserProviders(
    [
      info("claude", "Claude Code", [model("opus-5", "Opus 5"), model("sonnet-4.6", "Sonnet 4.6")]),
      info("codex", "Codex", [model("gpt-5.4", "GPT-5.4")]),
    ],
    {},
  );

  it("desktop height grows with rows and clamps at 220 and 400", () => {
    // Provider view: claude has 3 rows (default + 2 models) -> 80 + 3*40 = 200 -> clamped to 220.
    expect(resolveDesktopFixedHeight({ kind: "provider", providerId: "claude", providerLabel: "Claude Code" }, providers)).toBe(220);
    // Provider view: codex has 2 rows -> 80 + 2*40 = 160 -> clamped to 220.
    expect(resolveDesktopFixedHeight({ kind: "provider", providerId: "codex", providerLabel: "Codex" }, providers)).toBe(220);
    // All view: 3 + 2 = 5 rows -> 80 + 5*40 = 280.
    expect(resolveDesktopFixedHeight({ kind: "all" }, providers)).toBe(280);

    const many = buildBrowserProviders(
      [info("big", "Big", Array.from({ length: 10 }, (_, i) => model(`m${i}`, `M${i}`)))],
      {},
    );
    // 11 rows -> 80 + 440 = 520 -> clamped to 400.
    expect(resolveDesktopFixedHeight({ kind: "provider", providerId: "big", providerLabel: "Big" }, many)).toBe(400);
    expect(resolveDesktopFixedHeight({ kind: "all" }, many)).toBe(400);

    const loading = buildBrowserProviders([info("lazy", "Lazy")], {}, { loading: true });
    // A loading provider contributes no rows.
    expect(resolveDesktopFixedHeight({ kind: "provider", providerId: "lazy", providerLabel: "Lazy" }, loading)).toBe(220);
  });

  it("extra rows are added to the row count", () => {
    // All view: 5 rows + 2 extra -> 80 + 7*40 = 360.
    expect(resolveDesktopFixedHeight({ kind: "all" }, providers, 2)).toBe(360);
    expect(resolveDesktopFixedHeight({ kind: "all" }, providers, 0)).toBe(280);
    expect(resolveDesktopFixedHeight({ kind: "all" }, providers, 20)).toBe(400);
  });
});

describe("groupProfilesByProviderModel", () => {
  it("groups profiles by provider and model, skipping profiles without a model", () => {
    const lookup = groupProfilesByProviderModel([
      { provider: "claude", modelId: "opus-5" },
      { provider: "claude", modelId: "opus-5" },
      { provider: "claude", modelId: "sonnet-4.6" },
      { provider: "claude", modelId: "" },
      { provider: "codex", modelId: "gpt-5.4" },
    ]);

    expect(lookup.get("claude:opus-5")).toHaveLength(2);
    expect(lookup.get("claude:sonnet-4.6")).toHaveLength(1);
    expect(lookup.get("codex:gpt-5.4")).toHaveLength(1);
    expect(lookup.has("claude:")).toBe(false);
  });

  it("trims model ids so whitespace cannot create a separate key", () => {
    const lookup = groupProfilesByProviderModel([
      { provider: "claude", modelId: "opus-5" },
      { provider: "claude", modelId: "  opus-5  " },
    ]);

    expect(lookup.get("claude:opus-5")).toHaveLength(2);
  });
});

describe("model browser all view", () => {
  const providers = buildBrowserProviders(
    [
      info("claude", "Claude Code", [model("opus-5", "Opus 5"), model("sonnet-4.6", "Sonnet 4.6")]),
      info("copilot", "Copilot", [model("claude-opus-5", "Opus 5")]),
      info("codex", "Codex", [model("gpt-5.4", "GPT-5.4")]),
    ],
    {},
  );

  it("browses providers while the query is empty", () => {
    expect(
      resolveModelBrowserAllView({ providers, normalizedQuery: "", isSearchFocused: false }),
    ).toEqual({
      kind: "browse",
    });
  });

  it("shows every searchable model as soon as empty search receives focus", () => {
    const view = resolveModelBrowserAllView({
      providers,
      normalizedQuery: "",
      isSearchFocused: true,
    });

    expect(view.kind).toBe("searchResults");
    expect(view.kind === "searchResults" ? view.rows.map((row) => row.key) : []).toEqual([
      "claude:",
      "claude:opus-5",
      "claude:sonnet-4.6",
      "copilot:",
      "copilot:claude-opus-5",
      "codex:",
      "codex:gpt-5.4",
    ]);
  });

  it("ranks the same model label across every provider that offers it", () => {
    const view = resolveModelBrowserAllView({
      providers,
      normalizedQuery: "opus",
      isSearchFocused: true,
    });

    expect(view.kind).toBe("searchResults");
    expect(view.kind === "searchResults" ? view.rows.map((row) => row.key) : []).toEqual([
      "claude:opus-5",
      "copilot:claude-opus-5",
    ]);
  });

  it("matches models by their provider label", () => {
    const view = resolveModelBrowserAllView({
      providers,
      normalizedQuery: "codex",
      isSearchFocused: true,
    });

    expect(view.kind === "searchResults" ? view.rows.map((row) => row.key) : []).toEqual([
      "codex:",
      "codex:gpt-5.4",
    ]);
  });

  it("reports no matches instead of falling back to the provider list", () => {
    expect(
      resolveModelBrowserAllView({
        providers,
        normalizedQuery: "zzzz",
        isSearchFocused: true,
      }),
    ).toEqual({ kind: "noSearchMatches" });
  });

  it("ignores providers that are still loading or errored", () => {
    const loading: BrowserProvider = {
      id: "opencode",
      label: "OpenCode",
      modelSelection: { kind: "loading" },
    };
    const failed: BrowserProvider = {
      id: "pi",
      label: "Pi",
      modelSelection: { kind: "error", message: "unavailable" },
    };

    expect(
      resolveModelBrowserAllView({
        providers: [loading, failed],
        normalizedQuery: "opus",
        isSearchFocused: true,
      }),
    ).toEqual({ kind: "noSearchMatches" });
  });
});

describe("browser providers", () => {
  it("every provider gets a Default model row first", () => {
    const [claude] = buildBrowserProviders(
      [info("claude", "Claude Code", [model("opus-5", "Opus 5")])],
      {},
    );

    const rows = getProviderModelRows(claude!);
    expect(rows[0]).toMatchObject({
      key: "claude:",
      modelId: "",
      modelLabel: "Default model",
      isDefault: true,
    });
    expect(rows.slice(1).map((row) => row.modelId)).toEqual(["opus-5"]);
  });

  it("a provider without a catalog is selectable through its default row", () => {
    const [kimi] = buildBrowserProviders([info("kimi", undefined)], {});

    expect(getProviderModelRows(kimi!)).toEqual([
      {
        key: "kimi:",
        provider: "kimi",
        providerLabel: "Kimi",
        modelId: "",
        modelLabel: "Default model",
        description: undefined,
        isDefault: true,
      },
    ]);
  });

  it("an unhealthy provider is disabled with its reason", () => {
    const providers = buildBrowserProviders(
      [info("codex", "Codex", [model("gpt-5.4", "GPT-5.4")]), info("kimi", "Kimi")],
      {
        codex: { ok: false, detail: "no credential" },
        kimi: { ok: false, detail: "" },
      },
    );

    expect(providers[0]!.unavailableReason).toBe("no credential");
    expect(providers[1]!.unavailableReason).toBe("Unavailable");
    expect(getAllModelRows(providers)).toEqual([]);
  });

  it("provider count excludes the synthetic default row", () => {
    const [claude, kimi] = buildBrowserProviders(
      [info("claude", "Claude Code", [model("opus-5", "Opus 5")]), info("kimi", undefined)],
      {},
    );

    expect(getProviderModelCount(claude!)).toBe(1);
    expect(getProviderModelCount(kimi!)).toBe(0);
  });

  it("a loading provider has no model rows and a Loading label", () => {
    const providers = buildBrowserProviders(
      [info("claude", "Claude Code", [model("opus-5", "Opus 5")])],
      {},
      { loading: true },
    );

    expect(getProviderModelRows(providers[0]!)).toEqual([]);
    expect(
      resolveTriggerLabel({ providers, selectedProvider: "claude", selectedModel: "opus-5" }),
    ).toBe("Loading…");
  });
});

describe("search ranking", () => {
  it("ranking breaks ties by model label", () => {
    const providers = buildBrowserProviders(
      [
        info("a", "A", [model("opus-zeta", "opus zeta"), model("opus-alpha", "opus alpha")]),
        info("b", "B", [model("opus-mid", "opus mid")]),
      ],
      {},
    );
    // Every row hits "opus" as a whole word at the same offset — the tie is
    // broken by model label, not input order.
    const view = resolveModelBrowserAllView({
      providers,
      normalizedQuery: "opus",
      isSearchFocused: true,
    });

    expect(view.kind === "searchResults" ? view.rows.map((row) => row.key) : []).toEqual([
      "a:opus-alpha",
      "b:opus-mid",
      "a:opus-zeta",
    ]);
  });
});

describe("configured default model", () => {
  const withDefault = (defaultModel?: string) =>
    buildBrowserProviders(
      [{ ...info("claude", "Claude Code", [model("sonnet", "Sonnet (latest)"), model("opus", "Opus (latest)")]), defaultModel }],
      {},
    );

  it("the Default row says what a run resolves to, using the catalog label", () => {
    const [claude] = withDefault("sonnet");
    expect(getProviderModelRows(claude!)[0]).toMatchObject({
      modelId: "",
      modelLabel: "Default model",
      resolvedDefaultLabel: "Sonnet (latest)",
      description: "Currently Sonnet (latest)",
    });
  });

  it("an id the catalog does not list is shown as is", () => {
    const [claude] = withDefault("claude-sonnet-5-5");
    expect(getProviderModelRows(claude!)[0]?.resolvedDefaultLabel).toBe("claude-sonnet-5-5");
  });

  it("without a configured default the row is unchanged", () => {
    const [claude] = withDefault(undefined);
    const row = getProviderModelRows(claude!)[0]!;
    expect(row.resolvedDefaultLabel).toBeUndefined();
    expect(row.description).toBeUndefined();
  });

  it("the trigger shows the resolved model when nothing is picked", () => {
    expect(
      resolveTriggerLabel({ providers: withDefault("opus"), selectedProvider: "claude", selectedModel: "" }),
    ).toBe("Opus (latest)");
    expect(
      resolveTriggerLabel({ providers: withDefault(undefined), selectedProvider: "claude", selectedModel: "" }),
    ).toBe("Claude Code");
  });
});

describe("trigger label", () => {
  const providers = buildBrowserProviders(
    [info("claude", "Claude Code", [model("opus-5", "Opus 5"), model("sonnet-4.6", "Sonnet 4.6", { default: true })])],
    {},
  );

  it("trigger label is the model label, the catalog default for \"\", the raw id when unlisted", () => {
    expect(
      resolveTriggerLabel({ providers, selectedProvider: "claude", selectedModel: "opus-5" }),
    ).toBe("Opus 5");
    // "" -> the catalog's default model, not the synthetic row's label.
    expect(
      resolveTriggerLabel({ providers, selectedProvider: "claude", selectedModel: "" }),
    ).toBe("Sonnet 4.6");
    expect(
      resolveTriggerLabel({ providers, selectedProvider: "claude", selectedModel: "haiku" }),
    ).toBe("haiku");
    expect(
      resolveTriggerLabel({ providers, selectedProvider: "gemini", selectedModel: "gemini-3" }),
    ).toBe("Select model");
  });

  it("falls back to the provider label when the catalog has no default model", () => {
    const bare = buildBrowserProviders([info("kimi", "Kimi", [model("k2", "K2")])], {});

    expect(
      resolveTriggerLabel({ providers: bare, selectedProvider: "kimi", selectedModel: "" }),
    ).toBe("Kimi");
  });

  it("trigger title carries provider and model", () => {
    expect(
      resolveTriggerTitle({ providers, selectedProvider: "claude", selectedModel: "opus-5" }),
    ).toBe("Claude Code · Opus 5");
    expect(
      resolveTriggerTitle({ providers, selectedProvider: "claude", selectedModel: "" }),
    ).toBe("Claude Code · Sonnet 4.6");
    expect(
      resolveTriggerTitle({ providers, selectedProvider: "gemini", selectedModel: "gemini-3" }),
    ).toBe("Select model");
    // The provider-label fallback does not duplicate itself.
    const bare = buildBrowserProviders([info("kimi", "Kimi", [model("k2", "K2")])], {});
    expect(
      resolveTriggerTitle({ providers: bare, selectedProvider: "kimi", selectedModel: "" }),
    ).toBe("Kimi");
  });

  it("the title keeps the model when its label starts with the provider name", () => {
    const glm = buildBrowserProviders([info("glm", "GLM", [model("glm-5.3", "GLM 5.3")])], {});
    expect(resolveTriggerLabel({ providers: glm, selectedProvider: "glm", selectedModel: "glm-5.3" })).toBe("GLM 5.3");
    expect(resolveTriggerTitle({ providers: glm, selectedProvider: "glm", selectedModel: "glm-5.3" })).toBe("GLM · GLM 5.3");
  });
});

describe("defaultRowLabel", () => {
  const providers = (opts?: { defaultRowLabel?: string }) =>
    buildBrowserProviders([info("claude", "Claude Code", [model("sonnet", "Sonnet", { default: true })])], {}, opts);

  it("renames the synthetic row and the trigger, even when a catalog model is flagged default", () => {
    const named = providers({ defaultRowLabel: "CLI default" });
    expect(getProviderModelRows(named[0]!)[0]).toMatchObject({ modelId: "", modelLabel: "CLI default" });
    expect(resolveTriggerLabel({ providers: named, selectedProvider: "claude", selectedModel: "" })).toBe("CLI default");
  });

  it("without it the composer's label and trigger are unchanged", () => {
    const plain = providers();
    expect(getProviderModelRows(plain[0]!)[0]?.modelLabel).toBe("Default model");
    expect(resolveTriggerLabel({ providers: plain, selectedProvider: "claude", selectedModel: "" })).toBe("Sonnet");
  });
});
