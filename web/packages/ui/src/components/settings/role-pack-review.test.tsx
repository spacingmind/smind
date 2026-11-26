import { act } from "react";
import { fireEvent, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { listSettingsSections } from "@/components/settings/settings-registry";
import "@/components/settings/profiles-section";
import { RpcError } from "@/lib/ws-client";
import { renderWithDaemon } from "@/test/render-with-daemon";
import { rolePackCheck, rolePackDaemon, staleLeadCheck } from "@/test/role-pack-test-client";

function renderSection(handler: (method: string, params: unknown) => unknown) {
  const d = rolePackDaemon(handler);
  const section = listSettingsSections().find((s) => s.id === "agents");
  if (!section) throw new Error("profiles section did not register");
  renderWithDaemon(<>{section.render({ client: d.client as never, events: d.events })}</>, d);
  return d;
}

const count = (d: { calls: { method: string }[] }, method: string) => d.calls.filter((c) => c.method === method).length;

afterEach(() => {
  vi.useRealTimers();
});

describe("role-pack banner", () => {
  it("rolepack_banner_hidden_when_every_seat_is_current", async () => {
    renderSection(() => rolePackCheck());
    expect(await screen.findByTestId("role-pack-current")).toHaveTextContent("Role prompts match pack v9");
    expect(screen.queryByTestId("role-pack-banner")).toBeNull();
    expect(screen.queryByTestId("role-pack-review-open")).toBeNull();
  });

  it("rolepack_banner_shows_the_count_and_a_review_button", async () => {
    renderSection(() => staleLeadCheck());
    const banner = await screen.findByTestId("role-pack-banner");
    expect(banner).toHaveTextContent("Role prompts have updates for 1 seat");
    expect(within(banner).getByRole("button", { name: "Review" })).toBeInTheDocument();
  });

  it("rolepack_banner_shows_the_plural_count", async () => {
    renderSection(() => ({ ...staleLeadCheck(), seatsWithUpdates: 2 }));
    expect(await screen.findByTestId("role-pack-banner")).toHaveTextContent("Role prompts have updates for 2 seats");
  });

  it("rolepack_check_runs_once_when_the_section_opens_and_never_polls", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const d = renderSection(() => staleLeadCheck());
    await act(async () => {
      await vi.advanceTimersByTimeAsync(50);
    });
    expect(count(d, "profile.rolePack.check")).toBe(1);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(10 * 60 * 1000);
      window.dispatchEvent(new Event("focus"));
      document.dispatchEvent(new Event("visibilitychange"));
      await vi.advanceTimersByTimeAsync(50);
    });
    expect(count(d, "profile.rolePack.check")).toBe(1);
    expect(count(d, "profile.rolePack.apply")).toBe(0);
  });

  it("rolepack_check_error_offers_retry", async () => {
    let n = 0;
    const d = renderSection(() => {
      n += 1;
      if (n === 1) throw new RpcError("profile.rolePack.check: boom", "internal");
      return staleLeadCheck();
    });
    expect(await screen.findByTestId("role-pack-error")).toHaveTextContent("boom");
    fireEvent.click(screen.getByTestId("role-pack-retry"));
    expect(await screen.findByTestId("role-pack-banner")).toBeInTheDocument();
    expect(count(d, "profile.rolePack.check")).toBe(2);
  });
});
