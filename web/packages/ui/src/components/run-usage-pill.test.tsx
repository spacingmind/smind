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

const ALL_NULL = {
  inputTokens: null,
  outputTokens: null,
  costUsd: null,
  cachedInputTokens: null,
  cacheWriteTokens: null,
  reasoningTokens: null,
  model: null,
  contextUsed: null,
  contextSize: null,
};

describe("RunUsagePill", () => {
  it("the pill shows cost only, else total tokens", () => {
    // Cost wins, two decimals at a cent and above; the card keeps the exact value.
    let view = render(<RunUsagePill usage={usage()} />);
    expect(view.getByTestId("run-usage-pill")).toHaveTextContent("$0.04");
    view.unmount();

    view = render(<RunUsagePill usage={usage({ costUsd: 1.234 })} />);
    expect(view.getByTestId("run-usage-pill")).toHaveTextContent("$1.23");
    view.unmount();

    // No cost -- the compact in+out total.
    view = render(<RunUsagePill usage={usage({ costUsd: null, inputTokens: 16400, outputTokens: 1234 })} />);
    expect(view.getByTestId("run-usage-pill")).toHaveTextContent("17.6k tokens");
    view.unmount();

    // Only one side reported still sums fine.
    view = render(<RunUsagePill usage={usage({ costUsd: null, inputTokens: null, outputTokens: 900 })} />);
    expect(view.getByTestId("run-usage-pill")).toHaveTextContent("900 tokens");
    view.unmount();

    // Nothing reported -- the pill stays (its card is still reachable), icon only.
    view = render(<RunUsagePill usage={usage({ source: "not_reported", ...ALL_NULL })} />);
    const pill = view.getByTestId("run-usage-pill");
    expect(pill.textContent).toBe("");
    expect(pill).toHaveAttribute("aria-label", "Usage: Usage not reported");
    view.unmount();

    // A reported 0 is a real value: $0.00 wins over the token total.
    view = render(<RunUsagePill usage={usage({ inputTokens: 0, outputTokens: 0, costUsd: 0 })} />);
    expect(view.getByTestId("run-usage-pill")).toHaveTextContent("$0.00");
    view.unmount();
  });

  it("the aria-label keeps in/out/cost", () => {
    render(<RunUsagePill usage={usage()} />);
    expect(screen.getByTestId("run-usage-pill")).toHaveAttribute("aria-label", "Usage: 12.3k in, 1.2k out, $0.042");
  });

  it("shows an em dash in the card for null fields, never 0", async () => {
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
    expect(pill).toHaveTextContent("500 tokens");

    await act(async () => {
      fireEvent.click(pill);
    });
    const popover = screen.getByTestId("run-usage-popover");
    expect(popover).toHaveTextContent("Output");
    const outputValue = popover.querySelectorAll("li")[3];
    expect(outputValue).toHaveTextContent("—");
    expect(outputValue).not.toHaveTextContent("0");
  });

  it("renders nothing without usage", () => {
    const { container } = render(<RunUsagePill usage={undefined} />);
    expect(container).toBeEmptyDOMElement();
  });

  it("the card still shows every field at full precision", async () => {
    render(<RunUsagePill usage={usage()} />);

    await act(async () => {
      fireEvent.click(screen.getByTestId("run-usage-pill"));
    });

    const popover = screen.getByTestId("run-usage-popover");
    expect(popover).toHaveTextContent("12,345");
    expect(popover).toHaveTextContent("1,234");
    expect(popover).toHaveTextContent("$0.042");
    expect(popover).toHaveTextContent("claude-sonnet-4-5");
    expect(popover).toHaveTextContent("Source: Claude result");
  });
});
