import { act } from "react";
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { listSettingsSections } from "@/components/settings/settings-registry";
import "@/components/settings/profiles-section";
import { RpcError } from "@/lib/ws-client";
import { renderWithDaemon } from "@/test/render-with-daemon";
import { rolePackCheck, rolePackDaemon, rolePackField, rolePackSeat, staleLeadCheck } from "@/test/role-pack-test-client";
import type { RolePackCheck } from "@/lib/role-pack-types";

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
    expect(await screen.findByTestId("role-pack-error")).toHaveTextContent("Boom.");
    fireEvent.click(screen.getByTestId("role-pack-retry"));
    expect(await screen.findByTestId("role-pack-banner")).toBeInTheDocument();
    expect(count(d, "profile.rolePack.check")).toBe(2);
  });
});

async function openReview(handler: (method: string, params: unknown) => unknown) {
  const d = renderSection(handler);
  fireEvent.click(await screen.findByTestId("role-pack-review-open"));
  await screen.findByTestId("role-pack-review");
  return d;
}

const applies = (d: { calls: { method: string; params?: unknown }[] }) => d.calls.filter((c) => c.method === "profile.rolePack.apply");
const okApply = { backupDir: "", dryRun: false, results: [] };
const stale = (method: string) => (method === "profile.rolePack.check" ? staleLeadCheck() : okApply);

/** Nothing is applied by default: an untouched all-current lead and a validator with customised notes. */
const noDefaultCheck = (): RolePackCheck =>
  rolePackCheck({
    seatsWithUpdates: 1,
    seats: [
      rolePackSeat(),
      rolePackSeat({
        name: "validator",
        id: 13,
        untouched: false,
        fields: [
          rolePackField("instructions"),
          rolePackField("notes", { state: "customised", diff: "-mine\n+pack" }),
          rolePackField("skills"),
          rolePackField("model"),
          rolePackField("permissionMode"),
          rolePackField("orchestrationTools"),
          rolePackField("childPolicy"),
          rolePackField("onRateLimit"),
        ],
      }),
    ],
  });

describe("role-pack review dialog", () => {
  it("rolepack_review_lists_stale_and_customised_fields_with_their_diff", async () => {
    await openReview(stale);
    const lead = screen.getByTestId("role-pack-field-lead-instructions");
    expect(lead).toHaveTextContent("Instructions");
    expect(lead).toHaveTextContent("Update available");
    const lines = [...lead.querySelectorAll("[data-diff]")];
    expect(lines.map((l) => l.getAttribute("data-diff"))).toEqual(["ctx", "del", "add", "ctx"]);
    expect(lines.map((l) => l.textContent)).toEqual([" line one", "-old line", "+new line", " line three"]);
    const notes = screen.getByTestId("role-pack-field-validator-notes");
    expect(notes).toHaveTextContent("Notes");
    expect(notes).toHaveTextContent("Edited by you");
    expect(notes.querySelectorAll("[data-diff]")).toHaveLength(2);
    expect(screen.queryByTestId("role-pack-field-lead-notes")).toBeNull();
  });

  it("rolepack_capability_fields_are_marked", async () => {
    await openReview(stale);
    const f = screen.getByTestId("role-pack-field-lead-permissionMode");
    expect(f).toHaveTextContent("capability");
    expect(f).toHaveTextContent("Stored: default -> Pack: plan");
  });

  it("rolepack_state_badges_do_not_wrap", async () => {
    await openReview(stale);
    const f = screen.getByTestId("role-pack-field-lead-permissionMode");
    const state = within(f).getByText("Update available");
    const capability = within(f).getByText("capability");
    expect(state.className).toContain("whitespace-nowrap");
    expect(capability.className).toContain("whitespace-nowrap");
    expect(screen.getByTestId("role-pack-badges-lead-permissionMode").className).toContain("flex-wrap");
  });

  it("rolepack_apply_sends_no_take", async () => {
    const d = await openReview(stale);
    const btn = screen.getByTestId("role-pack-apply");
    expect(btn).toHaveTextContent("Apply 1 update");
    fireEvent.click(btn);
    await waitFor(() => expect(applies(d)).toHaveLength(1));
    expect((applies(d)[0].params as { take: string[] }).take).toEqual([]);
  });

  it("rolepack_take_sends_the_field", async () => {
    const d = await openReview(stale);
    fireEvent.click(screen.getByTestId("role-pack-take-validator-notes"));
    await waitFor(() => expect(applies(d)).toHaveLength(1));
    expect((applies(d)[0].params as { take: string[] }).take).toEqual(["validator.notes"]);
  });

  it("rolepack_capability_take_asks_first", async () => {
    const d = await openReview(stale);
    fireEvent.click(screen.getByTestId("role-pack-take-lead-permissionMode"));
    const confirm = await screen.findByTestId("role-pack-capability-confirm");
    expect(applies(d)).toHaveLength(0);
    expect(confirm).toHaveTextContent("default");
    expect(confirm).toHaveTextContent("plan");
    fireEvent.click(screen.getByTestId("role-pack-capability-confirm-cancel"));
    await waitFor(() => expect(screen.queryByTestId("role-pack-capability-confirm")).toBeNull());
    expect(applies(d)).toHaveLength(0);
    fireEvent.click(screen.getByTestId("role-pack-take-lead-permissionMode"));
    fireEvent.click(await screen.findByTestId("role-pack-capability-confirm-confirm"));
    await waitFor(() => expect(applies(d)).toHaveLength(1));
    expect((applies(d)[0].params as { take: string[] }).take).toEqual(["lead.permissionMode"]);
  });

  it("rolepack_nothing_is_applied_without_a_click", async () => {
    const d = await openReview(stale);
    fireEvent.click(screen.getByTestId("role-pack-close"));
    await waitFor(() => expect(screen.queryByTestId("role-pack-review")).toBeNull());
    expect(applies(d)).toHaveLength(0);
  });

  it("rolepack_backup_path_and_outcomes_are_shown", async () => {
    let checks = 0;
    const d = await openReview((method) => {
      if (method === "profile.rolePack.check") {
        checks += 1;
        return checks === 1 ? staleLeadCheck() : rolePackCheck();
      }
      return {
        backupDir: "/home/u/backups/role-pack/20261126T101500Z-pack-v9",
        dryRun: false,
        results: [{ seat: "lead", id: 12, fields: ["instructions"], outcome: "updated" }],
      };
    });
    fireEvent.click(screen.getByTestId("role-pack-apply"));
    expect(await screen.findByTestId("role-pack-backup")).toHaveTextContent("/home/u/backups/role-pack/20261126T101500Z-pack-v9");
    expect(screen.getByTestId("role-pack-result")).toHaveTextContent("Updated lead: instructions");
    await waitFor(() => expect(count(d, "profile.rolePack.check")).toBe(2));
    await waitFor(() => expect(screen.queryByTestId("role-pack-banner")).toBeNull());
  });

  it("rolepack_changed_meanwhile_is_explained", async () => {
    let fail = false;
    const d = await openReview((method) => {
      if (method === "profile.rolePack.check") return staleLeadCheck();
      if (fail) throw new RpcError("profile.rolePack.apply: local only", "forbidden");
      return {
        backupDir: "",
        dryRun: false,
        results: [
          { seat: "lead", id: 12, fields: ["instructions"], outcome: "changed_meanwhile" },
          { seat: "validator", id: 13, fields: ["notes"], outcome: "failed", error: "disk full" },
        ],
      };
    });
    fireEvent.click(screen.getByTestId("role-pack-apply"));
    const res = await screen.findByTestId("role-pack-result");
    expect(res).toHaveTextContent("lead changed since the check; the list was refreshed, review it again");
    expect(res).toHaveTextContent("Failed: Disk full.");
    fail = true;
    fireEvent.click(screen.getByTestId("role-pack-apply"));
    expect(await screen.findByTestId("role-pack-apply-error")).toHaveTextContent("Local only.");
    expect(screen.getByTestId("role-pack-review")).toBeInTheDocument();
    expect(applies(d)).toHaveLength(2);
  });

  it("rolepack_no_apply_button_when_nothing_is_applied_by_default", async () => {
    await openReview(noDefaultCheck);
    expect(screen.queryByTestId("role-pack-apply")).toBeNull();
    expect(screen.getByTestId("role-pack-close")).toBeInTheDocument();
    expect(screen.getByTestId("role-pack-nothing-by-default")).toHaveTextContent(
      "Nothing to apply by default. Use Take on a field to change it.",
    );
  });

  it("rolepack_hint_is_above_the_footer_and_gone_after_an_apply", async () => {
    await openReview(noDefaultCheck);
    const hint = screen.getByTestId("role-pack-nothing-by-default");
    const close = screen.getByTestId("role-pack-close");
    expect(hint.compareDocumentPosition(close) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    cleanup();

    let checks = 0;
    await openReview((method) => {
      if (method === "profile.rolePack.check") {
        checks += 1;
        return checks === 1 ? staleLeadCheck() : rolePackCheck();
      }
      return okApply;
    });
    fireEvent.click(screen.getByTestId("role-pack-apply"));
    expect(await screen.findByTestId("role-pack-result")).toBeInTheDocument();
    expect(screen.queryByTestId("role-pack-apply")).toBeNull();
    expect(screen.queryByTestId("role-pack-nothing-by-default")).toBeNull();
  });

  it("rolepack_errors_drop_the_method_label", async () => {
    renderSection(() => {
      throw new RpcError("profile.rolePack.check: database is locked", "internal");
    });
    const bannerError = await screen.findByTestId("role-pack-error");
    expect(bannerError).toHaveTextContent("Database is locked.");
    expect(bannerError.textContent).not.toContain("profile.rolePack.check");
    cleanup();

    let n = 0;
    await openReview((method) => {
      if (method === "profile.rolePack.check") return staleLeadCheck();
      n += 1;
      if (n === 1)
        return {
          backupDir: "",
          dryRun: false,
          results: [{ seat: "lead", id: 12, fields: ["instructions"], outcome: "failed", error: "profile.rolePack.apply: skill nope not found" }],
        };
      throw new RpcError("profile.rolePack.apply: backup directory /root/x: permission denied", "internal");
    });
    fireEvent.click(screen.getByTestId("role-pack-apply"));
    expect(await screen.findByTestId("role-pack-result")).toHaveTextContent("Failed: Skill nope not found.");
    fireEvent.click(screen.getByTestId("role-pack-apply"));
    const applyError = await screen.findByTestId("role-pack-apply-error");
    expect(applyError).toHaveTextContent("Backup directory /root/x: permission denied.");
    expect(applyError.textContent).not.toContain("profile.rolePack.apply");
  });
});
