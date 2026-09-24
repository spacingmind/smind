import { createRef } from "react";
import { render, screen, fireEvent } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { FindBar, type FindBarHandle } from "@/components/find/find-bar";

function renderFindBar(overrides: Partial<React.ComponentProps<typeof FindBar>> = {}) {
  const props = {
    query: "",
    status: "",
    canNavigate: false,
    onQueryChange: vi.fn(),
    onNext: vi.fn(),
    onPrevious: vi.fn(),
    onClose: vi.fn(),
    ...overrides,
  };
  render(<FindBar {...props} />);
  return props;
}

describe("FindBar", () => {
  it("shows the match-count status next to the input", () => {
    renderFindBar({ status: "3/17" });
    expect(screen.getByTestId("find-status")).toHaveTextContent("3/17");
  });

  it("Enter moves to the next match", () => {
    const props = renderFindBar({ canNavigate: true });
    fireEvent.keyDown(screen.getByTestId("find-input"), { key: "Enter" });
    expect(props.onNext).toHaveBeenCalledTimes(1);
    expect(props.onPrevious).not.toHaveBeenCalled();
  });

  it("Shift+Enter moves to the previous match", () => {
    const props = renderFindBar({ canNavigate: true });
    fireEvent.keyDown(screen.getByTestId("find-input"), { key: "Enter", shiftKey: true });
    expect(props.onPrevious).toHaveBeenCalledTimes(1);
    expect(props.onNext).not.toHaveBeenCalled();
  });

  it("Escape closes the bar", () => {
    const props = renderFindBar();
    fireEvent.keyDown(screen.getByTestId("find-input"), { key: "Escape" });
    expect(props.onClose).toHaveBeenCalledTimes(1);
  });

  it("typing calls onQueryChange", () => {
    const props = renderFindBar();
    fireEvent.change(screen.getByTestId("find-input"), { target: { value: "hello" } });
    expect(props.onQueryChange).toHaveBeenCalledWith("hello");
  });

  it("disables prev/next while there is nothing to navigate to", () => {
    renderFindBar({ canNavigate: false });
    expect(screen.getByRole("button", { name: "Previous match" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Next match" })).toBeDisabled();
  });

  it("close always works even with no matches", () => {
    const props = renderFindBar({ canNavigate: false });
    fireEvent.click(screen.getByRole("button", { name: "Close find" }));
    expect(props.onClose).toHaveBeenCalledTimes(1);
  });

  it("omits the replace row entirely when no replace prop is given", () => {
    renderFindBar();
    expect(screen.queryByTestId("find-toggle-replace")).not.toBeInTheDocument();
    expect(screen.queryByTestId("find-replace-input")).not.toBeInTheDocument();
  });

  it("replace row is collapsed until its toggle is clicked, then exposes Replace/Replace all", () => {
    const replace = {
      value: "",
      onChange: vi.fn(),
      onReplace: vi.fn(),
      onReplaceAll: vi.fn(),
    };
    renderFindBar({ replace, canNavigate: true });
    expect(screen.queryByTestId("find-replace-input")).not.toBeInTheDocument();

    fireEvent.click(screen.getByTestId("find-toggle-replace"));
    expect(screen.getByTestId("find-replace-input")).toBeInTheDocument();

    fireEvent.change(screen.getByTestId("find-replace-input"), { target: { value: "world" } });
    expect(replace.onChange).toHaveBeenCalledWith("world");

    fireEvent.click(screen.getByTestId("find-replace"));
    expect(replace.onReplace).toHaveBeenCalledTimes(1);

    fireEvent.click(screen.getByTestId("find-replace-all"));
    expect(replace.onReplaceAll).toHaveBeenCalledTimes(1);
  });

  it("exposes an imperative focus() that selects the current query", () => {
    const ref = createRef<FindBarHandle>();
    render(
      <FindBar
        ref={ref}
        query="needle"
        status=""
        canNavigate={false}
        onQueryChange={vi.fn()}
        onNext={vi.fn()}
        onPrevious={vi.fn()}
        onClose={vi.fn()}
      />,
    );
    const input = screen.getByTestId("find-input") as HTMLInputElement;
    input.blur();
    ref.current?.focus();
    expect(document.activeElement).toBe(input);
    expect(input.selectionStart).toBe(0);
    expect(input.selectionEnd).toBe("needle".length);
  });
});
