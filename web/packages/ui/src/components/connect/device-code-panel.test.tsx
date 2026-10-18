import { act } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { DeviceCodePanel } from "@/components/connect/device-code-panel";

const USER_CODE = "ABCD-1234";
const VERIFICATION_URL = "https://example.com/device";
const EXPIRES_IN_MS = 12 * 60 * 1000 + 34 * 1000;

function renderPanel(overrides: Partial<Parameters<typeof DeviceCodePanel>[0]> = {}) {
  const base = Date.now();
  const onCancel = vi.fn();
  const props = {
    userCode: USER_CODE,
    verificationUrl: VERIFICATION_URL,
    expiresAt: new Date(base + EXPIRES_IN_MS).toISOString(),
    onCancel,
    ...overrides,
  };
  render(<DeviceCodePanel {...props} />);
  return { onCancel, props };
}

describe("device code panel", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-01-01T00:00:00Z"));
    Object.assign(navigator, {
      clipboard: { writeText: vi.fn().mockResolvedValue(undefined) },
    });
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it("device panel shows the code, link and countdown", () => {
    renderPanel();
    expect(screen.getByTestId("device-code").textContent).toBe(USER_CODE);
    const link = screen.getByTestId("device-code-link");
    expect(link).toHaveAttribute("href", VERIFICATION_URL);
    expect(link.textContent).toBe(VERIFICATION_URL);
    expect(screen.getByTestId("device-code-countdown").textContent).toBe("Expires in 12:34");

    act(() => {
      vi.advanceTimersByTime(35_000);
    });
    expect(screen.getByTestId("device-code-countdown").textContent).toBe("Expires in 11:59");
  });

  it("device panel copies the code", async () => {
    renderPanel();
    await act(async () => {
      fireEvent.click(screen.getByTestId("device-code-copy"));
    });
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith(USER_CODE);
  });

  it("device panel shows expired when the time has passed", () => {
    renderPanel();
    act(() => {
      vi.advanceTimersByTime(EXPIRES_IN_MS + 1000);
    });
    expect(screen.getByTestId("device-code-countdown").textContent).toBe("The code has expired");
    act(() => {
      vi.advanceTimersByTime(5000);
    });
    expect(screen.getByTestId("device-code-countdown").textContent).toBe("The code has expired");
  });

  it("device panel cancel calls onCancel", () => {
    const { onCancel } = renderPanel();
    fireEvent.click(screen.getByTestId("accounts-connect-cancel"));
    expect(onCancel).toHaveBeenCalledTimes(1);
  });
});
