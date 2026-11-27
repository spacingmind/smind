import { act, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { AutomationsOffCard } from "@/components/automations/automations-off-card";
import { renderWithDaemon } from "@/test/render-with-daemon";
import { FakeUsageDaemon } from "@/test/usage-test-client";

const COMMAND = "smind config set automations.enabled true";

/** A daemon answering config.get with automations.enabled off, writable or not. */
function daemonWith(writable: boolean) {
  const daemon = new FakeUsageDaemon([]);
  const call = daemon.client.call.bind(daemon.client);
  (daemon.client as { call: unknown }).call = async (method: string, params?: unknown) =>
    method === "config.get"
      ? {
          keys: [
            {
              key: "automations.enabled",
              value: "false",
              default: "false",
              source: "file",
              restart: false,
              writable,
            },
          ],
        }
      : call(method, params);
  return daemon;
}

describe("AutomationsOffCard", () => {
  beforeEach(() => {
    Object.assign(navigator, {
      clipboard: { writeText: vi.fn().mockResolvedValue(undefined) },
    });
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("read-only connection shows the exact command and copies it", async () => {
    renderWithDaemon(<AutomationsOffCard />, daemonWith(false));
    await waitFor(() => expect(screen.getByTestId("automations-off-copy")).toBeInTheDocument());
    expect(screen.getByTestId("automations-off-card")).toHaveTextContent(COMMAND);
    await act(async () => {
      fireEvent.click(screen.getByTestId("automations-off-copy"));
    });
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith(COMMAND);
  });

  it("shows the rails and no restart instructions or config.yaml sentence", async () => {
    renderWithDaemon(<AutomationsOffCard />, daemonWith(false));
    await waitFor(() => expect(screen.getByTestId("automations-off-copy")).toBeInTheDocument());
    const card = screen.getByTestId("automations-off-card");
    expect(card).toHaveTextContent("never merge or release");
    expect(card).toHaveTextContent("Each run has a budget cap");
    expect(card).not.toHaveTextContent("config.yaml");
    expect(card).not.toHaveTextContent("smind service restart");
    expect(card).not.toHaveTextContent("smind stop && smind serve");
  });
});
