import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { NoticeAction, NoticeBanner } from "@/components/ui/notice-banner";

describe("notice-banner", () => {
  it("renders icon, text and tone", () => {
    const { unmount } = render(
      <NoticeBanner tone="warning" testId="nb">
        Heads up
      </NoticeBanner>
    );
    expect(screen.getByText("Heads up")).toBeInTheDocument();
    const root = screen.getByTestId("nb");
    expect(root).toHaveAttribute("data-tone", "warning");
    expect(root).toHaveAttribute("role", "status");
    expect(root.querySelector("svg")).not.toBeNull();
    unmount();

    render(
      <NoticeBanner tone="error" testId="nb">
        Broken
      </NoticeBanner>
    );
    expect(screen.getByTestId("nb")).toHaveAttribute("role", "alert");
  });

  it("renders primary and secondary actions", () => {
    render(
      <NoticeBanner
        actions={
          <>
            <NoticeAction>Go</NoticeAction>
            <NoticeAction variant="secondary">Later</NoticeAction>
          </>
        }
      >
        Text
      </NoticeBanner>
    );
    expect(screen.getByRole("button", { name: "Go" })).toHaveClass("rounded-full");
    expect(screen.getByRole("button", { name: "Later" })).toBeInTheDocument();
  });

  it("dismiss calls onDismiss and is absent without it", () => {
    const onDismiss = vi.fn();
    const { unmount } = render(<NoticeBanner onDismiss={onDismiss}>Text</NoticeBanner>);
    fireEvent.click(screen.getByRole("button", { name: "Dismiss" }));
    expect(onDismiss).toHaveBeenCalledTimes(1);
    unmount();

    render(<NoticeBanner>Text</NoticeBanner>);
    expect(screen.queryByRole("button", { name: "Dismiss" })).toBeNull();
  });
});
