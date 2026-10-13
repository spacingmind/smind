import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { ProfileGlyph, ProviderGlyph } from "@/components/composer/provider-glyph";

describe("provider-glyph", () => {
  it.each(["claude-native", "codex-native", "glm", "something-unknown"])("renders an aria-hidden svg for %s", (provider) => {
    const { container } = render(<ProviderGlyph provider={provider} />);
    const svg = container.querySelector("svg");
    expect(svg).not.toBeNull();
    expect(svg).toHaveAttribute("aria-hidden", "true");
  });

  it("uses distinct marks for claude, codex and the fallback", () => {
    const sig = (p: string) => render(<ProviderGlyph provider={p} />).container.innerHTML;
    expect(sig("claude-native")).not.toBe(sig("codex-native"));
    expect(sig("glm")).not.toBe(sig("claude-native"));
  });

  it("honours size and renders the profile glyph", () => {
    const { container } = render(<ProfileGlyph size={20} />);
    expect(container.querySelector("svg")).toHaveAttribute("width", "20");
  });
});
