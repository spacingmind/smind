import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { AutomationsOffCard } from "@/components/automations/automations-off-card";

describe("AutomationsOffCard", () => {
  beforeEach(() => {
    Object.assign(navigator, {
      clipboard: { writeText: vi.fn().mockResolvedValue(undefined) },
    });
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("shows the exact command and copies it", async () => {
    render(<AutomationsOffCard />);
    expect(screen.getByTestId("automations-off-card")).toHaveTextContent("smind config set automations.enabled true");
    await act(async () => {
      fireEvent.click(screen.getByTestId("automations-off-copy"));
    });
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith("smind config set automations.enabled true");
  });

  it("shows the restart line and the rails", () => {
    render(<AutomationsOffCard />);
    const card = screen.getByTestId("automations-off-card");
    expect(card).toHaveTextContent("smind stop && smind serve");
    expect(card).toHaveTextContent("never merge or release");
    expect(card).toHaveTextContent("Each run has a budget cap");
    expect(card).toHaveTextContent("config.yaml on purpose");
  });
});
