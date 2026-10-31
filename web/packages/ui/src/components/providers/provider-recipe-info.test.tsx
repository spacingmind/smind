import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { ProviderRecipeInfo } from "@/components/providers/provider-recipe-info";
import type { ProviderInfo } from "@/lib/types";

function inst(over: Partial<ProviderInfo> = {}): ProviderInfo {
  return { id: "zai", custom: true, recipe: "zai-coding", endpointKind: "anthropic", vendor: "zai", notes: ["Needs a coding-plan key"], ...over };
}

describe("ProviderRecipeInfo", () => {
  it("renders nothing for a non-recipe provider", () => {
    render(<ProviderRecipeInfo info={{ id: "plain", custom: true }} />);
    expect(screen.queryByTestId("provider-recipe-plain")).toBeNull();
  });

  it("shows recipe, endpoint and notes", () => {
    render(<ProviderRecipeInfo info={inst()} />);
    const group = screen.getByTestId("provider-recipe-zai");
    expect(group).toHaveTextContent("zai-coding");
    expect(group).toHaveTextContent("anthropic");
    expect(group).toHaveTextContent("zai");
    expect(group).toHaveTextContent("Needs a coding-plan key");
    expect(screen.getByRole("list")).toBeInTheDocument();
  });

  it("shows the unverified row only when unverified", () => {
    const { unmount } = render(<ProviderRecipeInfo info={inst()} />);
    expect(screen.queryByTestId("provider-recipe-unverified")).toBeNull();
    unmount();
    render(<ProviderRecipeInfo info={inst({ unverified: true })} />);
    expect(screen.getByTestId("provider-recipe-unverified")).toHaveTextContent("never verified");
  });

  it("says thinking cannot be turned off when hidden", () => {
    const { unmount } = render(<ProviderRecipeInfo info={inst()} />);
    expect(screen.queryByTestId("provider-recipe-thinking-hidden")).toBeNull();
    unmount();
    render(<ProviderRecipeInfo info={inst({ thinking: "hidden" })} />);
    expect(screen.getByTestId("provider-recipe-thinking-hidden")).toHaveTextContent("Cannot be turned off on this endpoint");
  });
});
