import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { ComposerStatusChips } from "@/components/composer/composer-status-chips";

describe("ComposerStatusChips", () => {
  it("renders the concurrency chip beside Working and calls onJumpToChat", () => {
    const onJumpToChat = vi.fn();
    render(<ComposerStatusChips running otherRunningChat={{ id: 7, title: "Fix login" }} onJumpToChat={onJumpToChat} />);
    expect(screen.getByTestId("composer-working-chip")).toBeInTheDocument();
    const chip = screen.getByTestId("composer-concurrency-chip");
    expect(chip).toHaveAccessibleName("Another chat is running in this worktree: Fix login. Open it.");
    expect(chip).toHaveTextContent("Fix login");
    fireEvent.click(chip);
    expect(onJumpToChat).toHaveBeenCalledWith(7);
  });

  it("renders nothing when nothing applies", () => {
    const { container } = render(<ComposerStatusChips running={false} otherRunningChat={null} />);
    expect(container).toBeEmptyDOMElement();
  });
});
