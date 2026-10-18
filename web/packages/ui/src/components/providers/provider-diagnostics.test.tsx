import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ProviderDiagnostics } from "@/components/providers/provider-diagnostics";

const TEXT = "sMind provider diagnostic\nProvider: GLM (glm)\n";

describe("ProviderDiagnostics", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("diagnostics is collapsed by default", () => {
    render(<ProviderDiagnostics text={TEXT} />);
    const toggle = screen.getByTestId("provider-diagnostics-toggle");
    expect(toggle.getAttribute("aria-expanded")).toBe("false");
    expect(screen.queryByTestId("provider-diagnostics-text")).toBeNull();
    expect(screen.queryByTestId("provider-diagnostics-copy")).toBeNull();
    expect(screen.getByText("Copyable text for bug reports")).toBeTruthy();
  });

  it("diagnostics shows and copies the text", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.assign(navigator, { clipboard: { writeText } });
    render(<ProviderDiagnostics text={TEXT} />);
    fireEvent.click(screen.getByTestId("provider-diagnostics-toggle"));
    expect(screen.getByTestId("provider-diagnostics-toggle").getAttribute("aria-expanded")).toBe("true");
    expect(screen.getByTestId("provider-diagnostics-text").textContent).toBe(TEXT);
    fireEvent.click(screen.getByTestId("provider-diagnostics-copy"));
    await act(async () => {
      await Promise.resolve();
    });
    expect(writeText).toHaveBeenCalledWith(TEXT);
    expect(screen.getByTestId("provider-diagnostics-copy").textContent).toBe("Copied");
  });
});
