import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { ProviderModels } from "@/components/providers/provider-models";
import type { ModelInfo, ProviderInfo } from "@/lib/types";

function provider(over: Partial<ProviderInfo> = {}): ProviderInfo {
  return { id: "claude-native", label: "Claude Code", models: [], ...over };
}

function models(n: number, withDefault = true): ModelInfo[] {
  return Array.from({ length: n }, (_, i) => ({
    id: `model-${i + 1}`,
    label: `Model ${i + 1}`,
    default: withDefault && i === 0,
  }));
}

describe("ProviderModels", () => {
  it("models lists the catalog with ids and the default badge", () => {
    render(<ProviderModels provider={provider({ models: models(3) })} />);
    const root = screen.getByTestId("provider-models");
    expect(root).toHaveTextContent("Model 1");
    expect(root).toHaveTextContent("model-2");
    expect(screen.getByText("Default")).toBeTruthy();
    expect(screen.queryByTestId("models-search")).toBeNull();
  });

  it("models shows the source note", () => {
    const { rerender } = render(
      <ProviderModels provider={provider({ models: models(2), modelsDiscovered: true })} />,
    );
    expect(screen.getByTestId("provider-models")).toHaveTextContent("Reported by the agent");
    rerender(<ProviderModels provider={provider({ models: models(2) })} />);
    expect(screen.getByTestId("provider-models")).toHaveTextContent("Built-in catalog");
  });

  it("models search appears only with eight or more models and filters", () => {
    const { rerender } = render(<ProviderModels provider={provider({ models: models(7) })} />);
    expect(screen.queryByTestId("models-search")).toBeNull();
    rerender(<ProviderModels provider={provider({ models: models(8) })} />);
    const search = screen.getByTestId("models-search");
    fireEvent.change(search, { target: { value: "model-8" } });
    expect(screen.getByText("Model 8")).toBeTruthy();
    expect(screen.queryByText("Model 1")).toBeNull();
  });

  it("models shows an empty note when the catalog is empty", () => {
    render(<ProviderModels provider={provider()} />);
    expect(screen.getByTestId("provider-models-empty")).toHaveTextContent(
      "No models reported yet — the agent hasn't been probed.",
    );
  });
});
