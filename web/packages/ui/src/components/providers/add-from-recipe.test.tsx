import { act, fireEvent, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { AddFromRecipe } from "@/components/providers/add-from-recipe";
import type { ProviderRecipe } from "@/lib/types";
import { renderWithDaemon } from "@/test/render-with-daemon";
import { FakeUsageDaemon } from "@/test/usage-test-client";

/** The write-only key the tests paste; never a real-looking credential. */
const SENTINEL = "sk-test-SENTINEL-0001";

function recipes(): ProviderRecipe[] {
  return [
    {
      id: "zai-glm",
      label: "GLM Coding Plan",
      description: "GLM's Anthropic-compatible endpoint",
      harness: "claude-code",
      endpointKind: "anthropic",
      vendor: "zai",
      env: [{ name: "ANTHROPIC_AUTH_TOKEN", kind: "secret", value: "" }],
      models: [
        { id: "glm-5.3", label: "GLM-5.3", default: true },
        { id: "glm-5.3-flash", label: "GLM-5.3 Flash", default: false },
      ],
      modelsRequired: false,
      disallowedTools: [],
      thinking: "supported",
      notes: ["Uses ANTHROPIC_AUTH_TOKEN, not ANTHROPIC_API_KEY."],
      docs: "https://docs.z.ai/devpack/tool/claude",
      source: "vendor-docs",
      verifiedAt: "2026-01-01",
      unverified: false,
      unverifiedAspects: [],
      needsBaseUrl: false,
      needsKey: true,
    },
    {
      id: "custom-openai",
      label: "Custom OpenAI-compatible",
      description: "Any OpenAI Responses-compatible endpoint",
      harness: "codex-app-server",
      endpointKind: "openai-responses",
      vendor: "generic",
      env: [],
      models: [],
      modelsRequired: false,
      disallowedTools: [],
      thinking: "hidden",
      notes: [],
      docs: "",
      source: "inferred",
      verifiedAt: "",
      unverified: true,
      unverifiedAspects: ["endpoint"],
      needsBaseUrl: true,
      needsKey: true,
    },
    {
      id: "local",
      label: "Local daemon",
      description: "A keyless local endpoint",
      harness: "acp",
      endpointKind: "anthropic",
      vendor: "generic",
      env: [],
      models: [],
      modelsRequired: false,
      disallowedTools: [],
      thinking: "supported",
      notes: [],
      docs: "",
      source: "vendor-docs",
      verifiedAt: "2026-01-01",
      unverified: false,
      unverifiedAspects: [],
      needsBaseUrl: false,
      needsKey: false,
    },
  ];
}

function setup(): { daemon: FakeUsageDaemon; onClose: ReturnType<typeof vi.fn> } {
  const daemon = new FakeUsageDaemon([]);
  daemon.recipes = { recipes: recipes() };
  const onClose = vi.fn();
  renderWithDaemon(<AddFromRecipe hasClient onClose={onClose} />, daemon);
  return { daemon, onClose };
}

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

async function pick(id: string): Promise<void> {
  await act(async () => {
    fireEvent.click(screen.getByTestId(`recipe-card-${id}`));
  });
}

function type(testId: string, value: string): void {
  fireEvent.change(screen.getByTestId(testId), { target: { value } });
}

async function submit(): Promise<void> {
  await act(async () => {
    fireEvent.click(screen.getByTestId("recipe-submit"));
  });
  await flush();
}

function adds(daemon: FakeUsageDaemon): { method: string; params: unknown }[] {
  return daemon.calls.filter((c) => c.method === "provider.addFromRecipe");
}

describe("AddFromRecipe", () => {
  it("lists the recipes with an Unverified badge only on unverified rows", async () => {
    setup();
    await flush();
    expect(screen.getByTestId("recipe-card-zai-glm")).toHaveTextContent("GLM Coding Plan");
    expect(screen.getByTestId("recipe-card-custom-openai")).toHaveTextContent("openai-responses");
    expect(screen.getByTestId("recipe-card-local")).toBeInTheDocument();
    expect(screen.getByTestId("recipe-unverified-custom-openai")).toHaveTextContent("Unverified");
    expect(screen.queryByTestId("recipe-unverified-zai-glm")).toBeNull();
    expect(screen.queryByTestId("recipe-unverified-local")).toBeNull();
  });

  it("opens the form for a picked recipe with notes, docs link and prefilled models", async () => {
    setup();
    await flush();
    await pick("zai-glm");
    expect(screen.getByTestId("recipe-form")).toBeInTheDocument();
    expect(screen.getByTestId("recipe-notes")).toHaveTextContent("Uses ANTHROPIC_AUTH_TOKEN, not ANTHROPIC_API_KEY.");
    expect(screen.getByTestId("recipe-docs")).toHaveAttribute("href", "https://docs.z.ai/devpack/tool/claude");
    expect(screen.getByTestId("recipe-docs")).toHaveAttribute("target", "_blank");
    expect(screen.getByTestId("recipe-model-glm-5.3")).toBeInTheDocument();
    expect(screen.getByTestId("recipe-model-glm-5.3-flash")).toBeInTheDocument();
    expect(screen.getByTestId("recipe-key")).toBeInTheDocument();
    expect(screen.queryByTestId("recipe-base-url")).toBeNull();
  });

  it("the key field is a password input with autocomplete off", async () => {
    setup();
    await flush();
    await pick("zai-glm");
    const key = screen.getByTestId("recipe-key");
    expect(key).toHaveAttribute("type", "password");
    expect(key).toHaveAttribute("autocomplete", "off");
    expect(key).toHaveAttribute("spellcheck", "false");
  });

  it("submits the key once, clears the field and never renders it back", async () => {
    const { daemon, onClose } = setup();
    await flush();
    await pick("zai-glm");
    type("recipe-key", SENTINEL);
    await submit();
    const calls = adds(daemon);
    expect(calls).toHaveLength(1);
    expect(calls[0].params).toEqual({
      recipe: "zai-glm",
      models: ["glm-5.3", "glm-5.3-flash"],
      secrets: { "api-key": SENTINEL },
    });
    expect(onClose).toHaveBeenCalledTimes(1);
    // Success resets the form: back at the picker, the key field is gone (and was cleared before that).
    expect(screen.queryByTestId("recipe-form")).toBeNull();
    expect(screen.queryByTestId("recipe-key")).toBeNull();
    expect(document.body.textContent).not.toContain("SENTINEL");
  });

  it("clears the key field when the daemon refuses and shows its text plain", async () => {
    const { daemon } = setup();
    daemon.addFromRecipeError = new Error("id already used");
    await flush();
    await pick("zai-glm");
    type("recipe-key", SENTINEL);
    await submit();
    expect(screen.getByTestId("recipe-key")).toHaveValue("");
    expect(screen.getByTestId("recipe-error")).toHaveTextContent("id already used");
    expect(screen.getByText("Re-enter the key and try again.")).toBeInTheDocument();
    expect(document.body.textContent).toContain("id already used");
    expect(document.body.textContent).not.toContain("SENTINEL");
  });

  it("edits the model list: remove, add, no duplicate", async () => {
    const { daemon } = setup();
    await flush();
    await pick("zai-glm");
    await act(async () => {
      fireEvent.click(screen.getByTestId("recipe-model-remove-glm-5.3-flash"));
    });
    expect(screen.queryByTestId("recipe-model-glm-5.3-flash")).toBeNull();
    // A duplicate id is not addable.
    type("recipe-model-input", "glm-5.3");
    expect(screen.getByTestId("recipe-model-add")).toBeDisabled();
    // A new id goes in by button, and by Enter (which must not submit the form).
    type("recipe-model-input", "glm-5.3-air");
    await act(async () => {
      fireEvent.click(screen.getByTestId("recipe-model-add"));
    });
    expect(screen.getByTestId("recipe-model-input")).toHaveValue("");
    type("recipe-model-input", "glm-5.3-max");
    await act(async () => {
      fireEvent.keyDown(screen.getByTestId("recipe-model-input"), { key: "Enter" });
    });
    expect(screen.getByTestId("recipe-model-glm-5.3-air")).toBeInTheDocument();
    expect(screen.getByTestId("recipe-model-glm-5.3-max")).toBeInTheDocument();
    expect(adds(daemon)).toHaveLength(0);
    type("recipe-key", SENTINEL);
    await submit();
    expect(adds(daemon)[0].params).toMatchObject({ models: ["glm-5.3", "glm-5.3-air", "glm-5.3-max"] });
  });

  it("shows the base URL field only when the recipe needs one", async () => {
    setup();
    await flush();
    await pick("zai-glm");
    expect(screen.queryByTestId("recipe-base-url")).toBeNull();
    await act(async () => {
      fireEvent.click(screen.getByTestId("recipe-back"));
    });
    await pick("custom-openai");
    expect(screen.getByTestId("recipe-base-url")).toBeInTheDocument();
    // Both a key and the base URL are required before the form can be sent.
    type("recipe-key", SENTINEL);
    expect(screen.getByTestId("recipe-submit")).toBeDisabled();
    type("recipe-base-url", "https://api.example.com/v1");
    expect(screen.getByTestId("recipe-submit")).toBeEnabled();
  });

  it("keeps submit disabled until the key is typed and hides the key field for a keyless recipe", async () => {
    setup();
    await flush();
    await pick("zai-glm");
    expect(screen.getByTestId("recipe-submit")).toBeDisabled();
    type("recipe-key", SENTINEL);
    expect(screen.getByTestId("recipe-submit")).toBeEnabled();
    await act(async () => {
      fireEvent.click(screen.getByTestId("recipe-back"));
    });
    await pick("local");
    expect(screen.queryByTestId("recipe-key")).toBeNull();
    expect(screen.getByTestId("recipe-submit")).toBeEnabled();
  });

  it("Back returns to the picker", async () => {
    setup();
    await flush();
    await pick("custom-openai");
    expect(screen.getByTestId("recipe-unverified-note")).toHaveTextContent("Not verified against a live endpoint yet (endpoint).");
    await act(async () => {
      fireEvent.click(screen.getByTestId("recipe-back"));
    });
    expect(screen.queryByTestId("recipe-form")).toBeNull();
    expect(screen.getByTestId("recipe-card-zai-glm")).toBeInTheDocument();
    expect(screen.getByTestId("recipe-card-custom-openai")).toBeInTheDocument();
  });
});
