import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { UsageSourceIcon } from "@/components/account-usage/usage-source-icon";

describe("provider_logos_render_and_unknown_provider_falls_back_to_gauge", () => {
  it("renders a distinct mark for anthropic and openai", () => {
    const a = render(<UsageSourceIcon provider="anthropic" />).container.innerHTML;
    const o = render(<UsageSourceIcon provider="openai" />).container.innerHTML;
    expect(a).not.toBe(o);
    expect(a).toContain("<svg");
    expect(o).toContain("<svg");
  });

  it("falls back to the lucide gauge for unknown and empty providers", () => {
    const g = render(<UsageSourceIcon provider="glm" />).container.querySelector("svg");
    expect(g).toHaveClass("lucide-gauge");
    expect(render(<UsageSourceIcon provider="" />).container.querySelector("svg")).toHaveClass("lucide-gauge");
  });
});
