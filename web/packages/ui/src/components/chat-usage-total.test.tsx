import { act } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { ChatUsageTotal } from "@/components/chat-usage-total";
import type { RunUsage } from "@/lib/types";

function usage(overrides: Partial<RunUsage> = {}): RunUsage {
  return { inputTokens: 100, outputTokens: 20, source: "claude_result", ...overrides };
}

describe("ChatUsageTotal", () => {
  it("chat total equals the sum of its runs' own usage", async () => {
    render(
      <ChatUsageTotal
        runs={[
          { usage: usage({ inputTokens: 12000, outputTokens: 1000, costUsd: 0.2 }) },
          { usage: usage({ inputTokens: 12100, outputTokens: 2200, costUsd: 0.11 }) },
        ]}
      />,
    );

    const pill = screen.getByTestId("chat-usage-total");
    expect(pill).toHaveTextContent("24.1k in · 3.2k out · $0.31");

    await act(async () => {
      fireEvent.click(pill);
    });
    const popover = screen.getByTestId("chat-usage-total-popover");
    expect(popover).toHaveTextContent("24,100");
    expect(popover).toHaveTextContent("3,200");
  });

  it("renders tokens in full, in a max-sm:hidden span so narrow screens show only the cost", () => {
    render(<ChatUsageTotal runs={[{ usage: usage({ inputTokens: 13500, outputTokens: 11400, costUsd: 0.17 }) }]} />);
    const pill = screen.getByTestId("chat-usage-total");
    expect(pill).toHaveTextContent("13.5k in · 11.4k out · $0.17");
    const tokens = screen.getByTestId("chat-usage-tokens");
    expect(tokens.className).toContain("max-sm:hidden");
    expect(tokens).toHaveTextContent("13.5k in · 11.4k out");
    expect(tokens).not.toHaveTextContent("$");
    expect(pill).toHaveAttribute("aria-label", expect.stringContaining("Usage total: 13.5k in, 11.4k out, $0.17"));
  });

  it("without a cost the tokens are not hidden", () => {
    render(<ChatUsageTotal runs={[{ usage: usage({ inputTokens: 100, outputTokens: 20 }) }]} />);
    expect(screen.queryByTestId("chat-usage-tokens")).toBeNull();
    expect(screen.getByTestId("chat-usage-total")).toHaveTextContent("100 in · 20 out");
  });

  it("a run with null cost does not count as 0 in the total", async () => {
    const first = render(
      <ChatUsageTotal
        runs={[
          { usage: usage({ inputTokens: 100, costUsd: 0.05 }) },
          { usage: usage({ inputTokens: 50, costUsd: null }) },
        ]}
      />,
    );

    const pill = screen.getByTestId("chat-usage-total");
    expect(pill).toHaveTextContent("150 in");
    // The total cost is just the one reported cost, not $0.05 + 0.
    expect(pill).toHaveTextContent("$0.05");
    first.unmount();

    // And when NO run reported a cost, the pill shows none at all and the
    // popover's Cost row is an em dash -- never $0.00.
    const none = render(
      <ChatUsageTotal
        runs={[
          { usage: usage({ inputTokens: 100, costUsd: null }) },
          { usage: usage({ inputTokens: 50, costUsd: null }) },
        ]}
      />,
    );
    expect(none.getByTestId("chat-usage-total").textContent).not.toContain("$");
    await act(async () => {
      fireEvent.click(none.getByTestId("chat-usage-total"));
    });
    const costRow = none.getAllByTestId("chat-usage-total-popover")[0].querySelectorAll("li")[5];
    expect(costRow).toHaveTextContent("—");
    expect(costRow).not.toHaveTextContent("$0");
    none.unmount();
  });

  it('says "2 of 3 runs reported usage" when only some reported', async () => {
    render(
      <ChatUsageTotal
        runs={[
          { usage: usage({ inputTokens: 100 }) },
          { usage: usage({ inputTokens: 50 }) },
          {},
        ]}
      />,
    );

    await act(async () => {
      fireEvent.click(screen.getByTestId("chat-usage-total"));
    });

    expect(screen.getByTestId("chat-usage-total-popover")).toHaveTextContent("2 of 3 runs reported usage");
  });

  it("renders nothing when no run reported usage", () => {
    const { container } = render(<ChatUsageTotal runs={[{}, {}]} />);
    expect(container).toBeEmptyDOMElement();

    const { container: nullRuns } = render(<ChatUsageTotal runs={null} />);
    expect(nullRuns).toBeEmptyDOMElement();
  });
});
