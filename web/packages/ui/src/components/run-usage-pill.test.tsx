import { act } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { RunUsagePill } from "@/components/run-usage-pill";
import type { RunUsage } from "@/lib/types";

function usage(overrides: Partial<RunUsage> = {}): RunUsage {
  return {
    inputTokens: 12345,
    outputTokens: 1234,
    costUsd: 0.042,
    model: "claude-sonnet-4-5",
    source: "claude_result",
    ...overrides,
  };
}

describe("RunUsagePill", () => {
  it("shows input, output and cost compactly", () => {
    render(<RunUsagePill usage={usage()} />);

    const pill = screen.getByTestId("run-usage-pill");
    expect(pill).toHaveTextContent("12.3k in · 1.2k out · $0.042");
  });

  it('shows "Usage not reported" for a not_reported run, never 0', () => {
    render(
      <RunUsagePill
        usage={usage({
          source: "not_reported",
          inputTokens: null,
          outputTokens: null,
          cachedInputTokens: null,
          cacheWriteTokens: null,
          reasoningTokens: null,
          costUsd: null,
          model: null,
          contextUsed: null,
          contextSize: null,
        })}
      />,
    );

    const pill = screen.getByTestId("run-usage-pill");
    expect(pill).toHaveTextContent("Usage not reported");
    expect(pill).not.toHaveTextContent("0");
  });

  it("omits null fields from the pill and shows an em dash for them in the popover", async () => {
    render(
      <RunUsagePill
        usage={usage({
          inputTokens: 500,
          outputTokens: null,
          costUsd: null,
          cachedInputTokens: null,
          cacheWriteTokens: null,
          reasoningTokens: null,
          model: null,
          contextUsed: null,
          contextSize: null,
        })}
      />,
    );

    const pill = screen.getByTestId("run-usage-pill");
    expect(pill).toHaveTextContent("500 in");
    expect(pill.textContent).not.toContain("out");

    await act(async () => {
      fireEvent.click(pill);
    });
    const popover = screen.getByTestId("run-usage-popover");
    expect(popover).toHaveTextContent("Output");
    const outputValue = popover.querySelectorAll("li")[3];
    expect(outputValue).toHaveTextContent("—");
    expect(outputValue).not.toHaveTextContent("0");
  });

  it("renders a reported 0 as 0", () => {
    render(<RunUsagePill usage={usage({ inputTokens: 0, outputTokens: 0, costUsd: 0 })} />);

    expect(screen.getByTestId("run-usage-pill")).toHaveTextContent("0 in · 0 out · $0.00");
  });

  it("popover shows full thousands, the model and the source label", async () => {
    render(<RunUsagePill usage={usage()} />);

    await act(async () => {
      fireEvent.click(screen.getByTestId("run-usage-pill"));
    });

    const popover = screen.getByTestId("run-usage-popover");
    expect(popover).toHaveTextContent("Usage");
    expect(popover).toHaveTextContent("12,345");
    expect(popover).toHaveTextContent("1,234");
    expect(popover).toHaveTextContent("claude-sonnet-4-5");
    expect(popover).toHaveTextContent("Source: Claude result");
  });

  it("renders nothing without usage", () => {
    const { container } = render(<RunUsagePill usage={undefined} />);
    expect(container).toBeEmptyDOMElement();
  });
});
