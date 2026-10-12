import { describe, expect, it } from "vitest";
import type { ModelInfo, ProviderInfo } from "@/lib/types";
import {
  buildBrowserProviders,
  getAllModelRows,
  getProviderModelRows,
  groupProfilesByProviderModel,
  resolveInitialModelBrowserView,
  resolveModelBrowserAllView,
  resolveTriggerLabel,
  type BrowserProvider,
} from "./model-browser";

function info(id: string, label: string | undefined, models?: ModelInfo[]): ProviderInfo {
  return { id: id as ProviderInfo["id"], label, models };
}

function model(id: string, label: string): ModelInfo {
  return { id, label };
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

  it("opens the root so pinned profiles are reachable", () => {
    expect(
      resolveInitialModelBrowserView({
        providers: [codex, pi],
        selectedProvider: "pi",
        selectedModel: "pi-pro",
        hasProfiles: true,
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

  it("falls back to the root when the selected provider is gone", () => {
    expect(
      resolveInitialModelBrowserView({
        providers: [codex, pi],
        selectedProvider: "gemini",
        selectedModel: "gemini-3",
        hasProfiles: false,
      }),
    ).toEqual({ kind: "all" });
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
        providerLabel: "kimi",
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
});

describe("trigger label", () => {
  const providers = buildBrowserProviders(
    [info("claude", "Claude Code", [model("opus-5", "Opus 5")])],
    {},
  );

  it("trigger label is provider and model, provider alone for the default model", () => {
    expect(
      resolveTriggerLabel({ providers, selectedProvider: "claude", selectedModel: "opus-5" }),
    ).toBe("Claude Code · Opus 5");
    expect(
      resolveTriggerLabel({ providers, selectedProvider: "claude", selectedModel: "" }),
    ).toBe("Claude Code");
    expect(
      resolveTriggerLabel({ providers, selectedProvider: "gemini", selectedModel: "gemini-3" }),
    ).toBe("Select model");
  });

  it("an unlisted model id shows as its raw id", () => {
    expect(
      resolveTriggerLabel({ providers, selectedProvider: "claude", selectedModel: "sonnet-4.6" }),
    ).toBe("Claude Code · sonnet-4.6");
  });
});
