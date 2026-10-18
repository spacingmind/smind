import { act } from "react";
import { fireEvent, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { FakeUsageDaemon } from "@/test/usage-test-client";
import { renderWithDaemon } from "@/test/render-with-daemon";
import { ProvidersScreen } from "@/components/providers/providers-screen";
import { useProvidersViewStore } from "@/stores/providers-view-store";
import type { Account, ProviderListResult } from "@/lib/types";

vi.mock("@/hooks/use-mobile", () => ({ useIsMobile: vi.fn(() => false) }));
const { useIsMobile } = await import("@/hooks/use-mobile");

function account(id: number, provider: string, label: string): Account {
  return { id, provider, label, credentialType: "oauth", createdAt: "", updatedAt: "" };
}

function daemon(): FakeUsageDaemon {
  const d = new FakeUsageDaemon([]);
  d.providers = {
    providers: [
      { id: "anthropic", label: "Claude", kind: "oauth", accountProvider: "anthropic" },
      { id: "openai", label: "OpenAI", kind: "api-key", accountProvider: "openai" },
    ],
  } as unknown as ProviderListResult;
  d.accounts = [account(1, "anthropic", "main")];
  return d;
}

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

describe("ProvidersScreen", () => {
  beforeEach(() => {
    useProvidersViewStore.getState().reset();
    vi.mocked(useIsMobile).mockReturnValue(false);
  });

  it("screen derives the first provider as the default selection", async () => {
    renderWithDaemon(<ProvidersScreen client={daemon().client} />, daemon());
    await flush();
    expect(screen.getAllByTestId("provider-nav-item-anthropic")[0]).toHaveAttribute("aria-current", "true");
    expect(screen.getByTestId("provider-detail")).toHaveTextContent("Claude");
  });

  it("selecting a provider shows its detail", async () => {
    const d = daemon();
    renderWithDaemon(<ProvidersScreen client={d.client} />, d);
    await flush();
    await act(async () => {
      fireEvent.click(screen.getAllByTestId("provider-nav-item-openai")[0]);
    });
    expect(screen.getByTestId("provider-detail")).toHaveTextContent("OpenAI");
    expect(screen.getAllByTestId("provider-nav-item-openai")[0]).toHaveAttribute("aria-current", "true");
  });

  it("compact viewport pushes the detail and back returns to the list", async () => {
    vi.mocked(useIsMobile).mockReturnValue(true);
    const d = daemon();
    renderWithDaemon(<ProvidersScreen client={d.client} />, d);
    await flush();
    expect(screen.getByTestId("providers-screen")).toHaveAttribute("data-layout", "pushed");
    expect(screen.getByTestId("provider-nav")).toBeInTheDocument();
    await act(async () => {
      fireEvent.click(screen.getAllByTestId("provider-nav-item-openai")[0]);
    });
    expect(screen.queryByTestId("provider-nav")).not.toBeInTheDocument();
    expect(screen.getByTestId("provider-detail")).toHaveTextContent("OpenAI");
    await act(async () => {
      fireEvent.click(screen.getByTestId("provider-detail-back"));
    });
    expect(screen.getByTestId("provider-nav")).toBeInTheDocument();
    expect(screen.queryByTestId("provider-detail")).not.toBeInTheDocument();
  });

  it("screen shows loading until accounts arrive", () => {
    const d = daemon();
    d.accounts = [];
    renderWithDaemon(<ProvidersScreen client={d.client} />, d);
    expect(screen.getByText("Loading…")).toBeInTheDocument();
    expect(screen.queryByTestId("providers-screen")).not.toBeInTheDocument();
  });
});
