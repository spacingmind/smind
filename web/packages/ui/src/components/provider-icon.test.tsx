import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { ProfileIcon, ProviderIcon } from "@/components/provider-icon";

describe("provider-icon", () => {
  it.each(["claude-native", "codex-native", "glm", "something-unknown"])("renders an aria-hidden svg for %s", (provider) => {
    const { container } = render(<ProviderIcon provider={provider} />);
    const svg = container.querySelector("svg");
    expect(svg).not.toBeNull();
    expect(svg).toHaveAttribute("aria-hidden", "true");
  });

  it("uses distinct marks for claude, codex and the fallback", () => {
    const sig = (p: string) => render(<ProviderIcon provider={p} />).container.innerHTML;
    expect(sig("claude-native")).not.toBe(sig("codex-native"));
    expect(sig("glm")).not.toBe(sig("claude-native"));
  });

  it("maps both id vocabularies to the same family mark", () => {
    const sig = (p: string) => render(<ProviderIcon provider={p} />).container.innerHTML;
    expect(sig("anthropic")).toBe(sig("claude-native"));
    expect(sig("openai")).toBe(sig("codex-native"));
  });

  it("renders the agent fallback for unknown providers and the usage fallback on demand", () => {
    const sig = (p: string, fallback?: "agent" | "usage") =>
      render(<ProviderIcon provider={p} fallback={fallback} />).container.innerHTML;
    expect(sig("kimi")).toBe(sig("something-unknown"));
    expect(sig("xai", "usage")).not.toBe(sig("xai"));
    expect(sig("anthropic", "usage")).toBe(sig("claude-native", "usage"));
  });

  it("honours size and renders the profile icon", () => {
    const { container } = render(<ProfileIcon size={20} />);
    expect(container.querySelector("svg")).toHaveAttribute("width", "20");
  });

  it("an ACP agent that names a family keeps that family's mark", () => {
    const mark = (provider: string) => render(<ProviderIcon provider={provider} />).container.querySelector("path")?.getAttribute("d");
    expect(mark("claude-code-acp")).toBe(mark("claude-native"));
    expect(mark("codex-acp")).toBe(mark("codex-native"));
    expect(mark("gemini-acp")).not.toBe(mark("claude-native"));
  });
});
