import { act } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { DesktopUnreachable } from "@/components/desktop-unreachable";

const getCurrentConnection = vi.fn();

vi.mock("@/lib/platform", () => ({
  desktop: {
    getCurrentConnection: (...args: unknown[]) => getCurrentConnection(...args),
  },
}));

afterEach(() => {
  vi.restoreAllMocks();
  getCurrentConnection.mockReset();
});

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

describe("DesktopUnreachable", () => {
  it("shows the connection's label and URL once resolved", async () => {
    getCurrentConnection.mockResolvedValue({
      id: "url:example",
      kind: "url",
      label: "Tunnel",
      baseUrl: "http://example.com:9000",
    });

    render(<DesktopUnreachable onSwitchConnection={() => {}} />);
    await flush();

    expect(screen.getByTestId("desktop-unreachable")).toHaveTextContent("Can't reach Tunnel");
    expect(screen.getByTestId("desktop-unreachable")).toHaveTextContent("http://example.com:9000");
  });

  it("falls back to a generic label if the connection can't be resolved either", async () => {
    getCurrentConnection.mockRejectedValue(new Error("nope"));

    render(<DesktopUnreachable onSwitchConnection={() => {}} />);
    await flush();

    expect(screen.getByTestId("desktop-unreachable")).toHaveTextContent("Can't reach the daemon");
  });

  it("Retry calls the provided callback", async () => {
    getCurrentConnection.mockResolvedValue({ id: "local", kind: "local", label: "Local", baseUrl: "http://127.0.0.1:4648" });
    const onRetry = vi.fn();

    render(<DesktopUnreachable onRetry={onRetry} onSwitchConnection={() => {}} />);
    await flush();
    fireEvent.click(screen.getByTestId("desktop-unreachable-retry"));

    expect(onRetry).toHaveBeenCalledTimes(1);
  });

  it("Switch connection calls the provided callback", async () => {
    getCurrentConnection.mockResolvedValue({ id: "local", kind: "local", label: "Local", baseUrl: "http://127.0.0.1:4648" });
    const onSwitchConnection = vi.fn();

    render(<DesktopUnreachable onSwitchConnection={onSwitchConnection} />);
    await flush();
    fireEvent.click(screen.getByTestId("desktop-unreachable-switch"));

    expect(onSwitchConnection).toHaveBeenCalledTimes(1);
  });
});
