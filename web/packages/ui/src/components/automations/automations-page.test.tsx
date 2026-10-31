import { act } from "react";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { AutomationsPage } from "@/components/automations/automations-page";
import { AUTOMATION_PAGE_SIZE } from "@/lib/automation-types";
import { renderWithDaemon } from "@/test/render-with-daemon";
import { auditRow, automation, AutomationDaemon, ciAutomation, forbidden } from "@/test/automation-fixtures";
import { RpcError } from "@/lib/ws-client";

async function flush(): Promise<void> {
  await act(async () => {
    for (let i = 0; i < 8; i++) await Promise.resolve();
  });
}

async function click(el: HTMLElement): Promise<void> {
  fireEvent.click(el);
  await flush();
}

async function type(el: HTMLElement, value: string): Promise<void> {
  fireEvent.change(el, { target: { value } });
  await flush();
}

async function renderPage(daemon: AutomationDaemon) {
  const view = renderWithDaemon(
    <AutomationsPage onNavigateBack={() => {}} closeSettings={() => {}} />,
    daemon,
  );
  await flush();
  return view;
}

function daemonWith(automations = [automation()], over: Partial<AutomationDaemon["listing"]> = {}): AutomationDaemon {
  const d = new AutomationDaemon();
  d.listing = { enabled: true, paused: false, automations, ...over };
  return d;
}

const callsOf = (d: AutomationDaemon, method: string) => d.calls.filter((c) => c.method === method);

async function fillForm(): Promise<void> {
  await click(screen.getByTestId("automation-new-button"));
  await type(await screen.findByTestId("automation-name"), "Weekly sweep");
  await type(screen.getByTestId("automation-workspace"), "7");
  await type(screen.getByTestId("automation-profile"), "3");
  await type(screen.getByTestId("automation-model"), "haiku");
  await type(screen.getByTestId("automation-cron"), "0 9 * * 1");
  await type(screen.getByTestId("automation-title"), "Sweep {{signal.scheduledAt}}");
  await type(screen.getByTestId("automation-prompt"), "Sweep the repo.");
}

describe("AutomationsPage", () => {
  it("off state shows the card, not the list", async () => {
    await renderPage(daemonWith([automation()], { enabled: false }));
    const off = screen.getByTestId("automations-off-card");
    expect(off).toHaveTextContent("Automations are off");
    expect(off).toHaveTextContent("smind config set automations.enabled true");
    expect(screen.queryByTestId("automations-list")).toBeNull();
    // No row, no switch, and nothing that would flip the daemon's own switch from here.
    expect(screen.queryByRole("switch")).toBeNull();
    expect(screen.queryByTestId("automation-fire-1")).toBeNull();
    expect(screen.queryByRole("button", { name: /turn on|enable the daemon|enable automations/i })).toBeNull();
    // Everything that would start or change something stays read-only.
    expect(screen.getByTestId("automation-new-button")).toBeDisabled();
  });

  it("ListRowsAndStatus: name, status badge, signal summary, last fired and next run", async () => {
    const fired = new Date(Date.now() - 3 * 3_600_000).toISOString();
    await renderPage(
      daemonWith([
        automation({ lastFiredAt: fired }),
        ciAutomation({ enabled: false }),
        automation({ id: 3, name: "Held", status: "paused", pausedReason: "3 failures in a row" }),
      ]),
    );
    expect(screen.getByTestId("automation-name-1")).toHaveTextContent("Nightly triage");
    expect(screen.getByTestId("automation-status-1")).toHaveTextContent("Active");
    expect(screen.getByTestId("automation-signal-1")).toHaveTextContent("cron 0 3 * * * UTC");
    expect(screen.getByTestId("automation-times-1")).toHaveTextContent("Last fired 3h ago");
    expect(screen.getByTestId("automation-times-1")).toHaveTextContent("next run Oct 28 03:00 UTC");
    expect(screen.getByTestId("automation-status-2")).toHaveTextContent("Disabled");
    expect(screen.getByTestId("automation-signal-2")).toHaveTextContent("ci on develop");
    expect(screen.getByTestId("automation-times-2")).toHaveTextContent("Never fired");
    expect(screen.getByTestId("automation-times-2")).not.toHaveTextContent("next run");
    expect(screen.getByTestId("automation-status-3")).toHaveTextContent("Paused");
    expect(screen.queryByTestId("automation-capped-1")).toBeNull();
  });

  it("lists rows with the switch, UTC and local next run", async () => {
    await renderPage(daemonWith([automation({ nextRunAt: "2026-11-01T09:00:00Z", nextRuns: ["2026-11-01T09:00:00Z"] })]));
    const times = screen.getByTestId("automation-times-1");
    expect(times).toHaveTextContent("next run Nov 1 09:00 UTC");
    expect(times).toHaveTextContent("your time");
    expect(screen.getByTestId("automation-switch-1")).toHaveAttribute("aria-checked", "true");
  });

  it("new automation opens the form", async () => {
    await renderPage(daemonWith([]));
    await click(screen.getByTestId("automation-new-button"));
    expect(await screen.findByTestId("automation-form")).toBeInTheDocument();
    expect(screen.getByTestId("automation-cron")).toBeInTheDocument();
  });

  it("ListRowsAndStatus: a budget-blocked row says the last run reached its budget; a globally paused daemon says so", async () => {
    await renderPage(daemonWith([automation({ budgetBlocked: true })], { paused: true }));
    expect(screen.getByTestId("automation-capped-1")).toHaveTextContent("capped: last run reached its budget");
    expect(screen.getByTestId("automations-paused")).toHaveTextContent("automations.paused");
  });

  it("PausedRowShowsReasonAndResume: the reason is visible with a Resume button that calls automation.resume", async () => {
    const d = daemonWith([automation({ status: "paused", pausedReason: "paused after 3 failed runs in a row" }), automation({ id: 2, name: "Calm" })]);
    await renderPage(d);
    const alert = screen.getByTestId("automation-paused-1");
    expect(alert).toHaveTextContent("paused after 3 failed runs in a row");
    expect(screen.queryByTestId("automation-resume-2")).toBeNull();
    await click(within(alert).getByRole("button", { name: "Resume" }));
    expect(callsOf(d, "automation.resume")[0].params).toEqual({ id: 1 });
    await waitFor(() => expect(screen.queryByTestId("automation-paused-1")).toBeNull());
  });

  it("ForbiddenGoesReadOnly_DisableStillWorks: a refusal turns the page read-only with the daemon's message; Disable still works", async () => {
    const d = daemonWith([ciAutomation({ enabled: false }), automation()]);
    d.refuse = forbidden("automation.enable is only available on a direct connection, not over the relay");
    d.refuseOnly = ["automation.enable", "automation.fire", "automation.create"];
    await renderPage(d);
    await click(screen.getByTestId("automation-switch-2"));
    expect(screen.getByTestId("automations-read-only")).toHaveTextContent("only available on a direct connection, not over the relay");
    expect(screen.getByTestId("automation-new-button")).toBeDisabled();
    expect(screen.getByTestId("automation-fire-1")).toBeDisabled();
    expect(screen.getByTestId("automation-edit-1")).toBeDisabled();
    expect(screen.getByTestId("automation-delete-1")).toBeDisabled();
    // The enabled row's switch is the brake: still available, and it goes through.
    expect(screen.getByTestId("automation-switch-1")).toBeEnabled();
    await click(screen.getByTestId("automation-switch-1"));
    expect(callsOf(d, "automation.disable")).toHaveLength(1);
    // The disabled row cannot be enabled any more.
    expect(screen.getByTestId("automation-switch-2")).toBeDisabled();
    expect(callsOf(d, "automation.enable")).toHaveLength(1);
  });

  it("CreateStartsDisabledNotice: creating sends the definition and says it was created disabled", async () => {
    const d = daemonWith([]);
    await renderPage(d);
    await fillForm();
    expect(screen.getByTestId("automation-cron-preview")).toBeInTheDocument();
    await click(screen.getByTestId("automation-form-submit"));
    const sent = callsOf(d, "automation.create")[0].params as { name: string; workspaceId: number; signal: unknown; template: { profileId: number; model: string; mode: string } };
    expect(sent.name).toBe("Weekly sweep");
    expect(sent.workspaceId).toBe(7);
    expect(sent.signal).toEqual({ kind: "cron", cron: "0 9 * * 1" });
    expect(sent.template).toMatchObject({ profileId: 3, model: "haiku", mode: "default", budgetUsd: 0, maxRunMinutes: 0 });
    expect(screen.getByTestId("automations-notice")).toHaveTextContent("Created disabled. Enable it when ready.");
    expect(screen.queryByTestId("automation-form")).toBeNull();
    expect(screen.getByTestId("automation-status-99")).toHaveTextContent("Disabled");
  });

  it("the form shows the placeholders, the defaults and the signal-is-data note", async () => {
    await renderPage(daemonWith([]));
    await click(screen.getByTestId("automation-new-button"));
    const ph = await screen.findByTestId("automation-placeholders");
    for (const p of ["repo", "workflow", "branch", "headSha", "runId", "runUrl", "conclusion", "event", "scheduledAt", "firedAt", "catchup"]) {
      expect(ph).toHaveTextContent(`{{signal.${p}}}`);
    }
    expect(ph).toHaveTextContent("Signal text is data");
    expect(screen.getByTestId("automation-max-runs")).toHaveAttribute("placeholder", "2");
    expect(screen.getByTestId("automation-budget")).toHaveAttribute("placeholder", "3");
    expect(screen.getByTestId("automation-max-minutes")).toHaveAttribute("placeholder", "45");
    expect(screen.getByTestId("automation-pause-after")).toHaveAttribute("placeholder", "3");
    expect(screen.getByTestId("automation-memory")).toBeChecked();
    await click(screen.getByTestId("automation-kind-github.workflow_run"));
    expect(screen.getByTestId("automation-dedupe")).toHaveAttribute("placeholder", "1440");
    expect(screen.getByTestId("automation-branch")).toHaveValue("develop");
    expect(screen.getByTestId("automation-conclusion-failure")).toBeChecked();
    expect(screen.getByTestId("automation-mode").querySelectorAll("option")).toHaveLength(3);
  });

  it("FormRejectsCronUnderFiveMinutes: inline error, nothing is sent", async () => {
    const d = daemonWith([]);
    await renderPage(d);
    await fillForm();
    await type(screen.getByTestId("automation-cron"), "*/4 * * * *");
    expect(screen.getByTestId("automation-cron-error")).toHaveTextContent("minimum cadence is 5 min");
    expect(screen.queryByTestId("automation-cron-preview")).toBeNull();
    expect(screen.getByTestId("automation-form-submit")).toBeDisabled();
    fireEvent.submit(screen.getByTestId("automation-form"));
    await flush();
    expect(callsOf(d, "automation.create")).toHaveLength(0);
    await type(screen.getByTestId("automation-cron"), "*/5 * * * *");
    expect(screen.getByTestId("automation-cron-preview").querySelectorAll("li")).toHaveLength(3);
    expect(screen.getByTestId("automation-form-submit")).toBeEnabled();
  });

  it("UnattendedModeErrorShownInline: the daemon's refusal stays in the open form", async () => {
    const d = daemonWith([]);
    d.onMutation["automation.create"] = () => {
      throw new RpcError('automation.create: template.mode "full-access" / autoAccept is an unattended mode: set automations.allowUnattendedModes: true in config.yaml to allow it', "invalid");
    };
    await renderPage(d);
    await fillForm();
    await click(screen.getByTestId("automation-form-submit"));
    expect(screen.getByTestId("automation-form-error")).toHaveTextContent("automations.allowUnattendedModes");
    expect(screen.getByTestId("automation-form")).toBeInTheDocument();
    expect(screen.queryByTestId("automations-read-only")).toBeNull();
  });

  it("DeleteNeedsConfirm: nothing is deleted until the dialog is confirmed", async () => {
    const d = daemonWith([automation(), ciAutomation()]);
    await renderPage(d);
    await click(screen.getByTestId("automation-delete-1"));
    expect(screen.getByTestId("automation-delete-dialog")).toHaveTextContent("Delete Nightly triage?");
    expect(callsOf(d, "automation.delete")).toHaveLength(0);
    await click(screen.getByTestId("automation-delete-dialog-cancel"));
    expect(callsOf(d, "automation.delete")).toHaveLength(0);
    expect(screen.getByTestId("automation-row-1")).toBeInTheDocument();
    await click(screen.getByTestId("automation-delete-1"));
    await click(screen.getByTestId("automation-delete-dialog-confirm"));
    expect(callsOf(d, "automation.delete")[0].params).toEqual({ id: 1 });
    await waitFor(() => expect(screen.queryByTestId("automation-row-1")).toBeNull());
    expect(screen.getByTestId("automation-row-2")).toBeInTheDocument();
  });

  it("HistoryFiltersAndPages: pages with before = the last id, filters by decision, badges and a task link", async () => {
    const d = daemonWith([automation()]);
    d.audit = Array.from({ length: AUTOMATION_PAGE_SIZE + 5 }, (_, i) =>
      auditRow({
        id: i + 1,
        decision: i % 5 === 0 ? "capped" : "fire",
        reason: i % 5 === 0 ? "2 runs in the last hour" : "",
        manual: i === AUTOMATION_PAGE_SIZE + 4,
        catchup: i === AUTOMATION_PAGE_SIZE + 3,
        taskId: i % 5 === 0 ? null : 40 + i,
      }),
    );
    await renderPage(d);
    await click(screen.getByTestId("automation-history-1"));
    const list = await screen.findByTestId("automation-audit-list");
    expect(within(list).getAllByRole("listitem")).toHaveLength(AUTOMATION_PAGE_SIZE);
    expect(screen.getByTestId("automation-audit-30")).toBeInTheDocument();
    expect(screen.getByTestId("automation-audit-manual-30")).toHaveTextContent("manual");
    expect(screen.getByTestId("automation-audit-catchup-29")).toBeInTheDocument();
    expect(screen.getByTestId("automation-audit-decision-30")).toHaveAttribute("data-status", "success");
    expect(screen.getByTestId("automation-audit-task-30")).toHaveTextContent("Task #69");
    expect(screen.getByTestId("automation-audit-task-30")).toHaveAttribute("href", "#/workspace/7/task/69/files");

    await click(screen.getByTestId("automation-audit-more"));
    expect(callsOf(d, "automation.audit")[1].params).toEqual({ id: 1, limit: AUTOMATION_PAGE_SIZE, before: 6 });
    expect(within(screen.getByTestId("automation-audit-list")).getAllByRole("listitem")).toHaveLength(AUTOMATION_PAGE_SIZE + 5);
    expect(screen.queryByTestId("automation-audit-more")).toBeNull();

    await type(screen.getByTestId("automation-audit-filter"), "capped");
    const rows = within(screen.getByTestId("automation-audit-list")).getAllByRole("listitem");
    expect(rows).toHaveLength(6);
    for (const r of rows) expect(r).toHaveAttribute("data-decision", "capped");
    expect(screen.getByTestId("automation-audit-decision-1")).toHaveAttribute("data-status", "warning");
  });

  it("MemoryClearNeedsConfirm: shows the memory, and clears only after the dialog is confirmed; absent when memory is off", async () => {
    const d = daemonWith([automation(), ciAutomation()]);
    d.memory = "Flaky test: TestRelay; retry once.";
    await renderPage(d);
    await click(screen.getByTestId("automation-history-1"));
    expect(await screen.findByTestId("automation-memory-text")).toHaveTextContent("Flaky test: TestRelay; retry once.");
    await click(screen.getByTestId("automation-memory-clear"));
    expect(callsOf(d, "automation.memory.clear")).toHaveLength(0);
    await click(screen.getByTestId("automation-memory-clear-dialog-cancel"));
    expect(callsOf(d, "automation.memory.clear")).toHaveLength(0);
    await click(screen.getByTestId("automation-memory-clear"));
    await click(screen.getByTestId("automation-memory-clear-dialog-confirm"));
    expect(callsOf(d, "automation.memory.clear")[0].params).toEqual({ id: 1 });
    await waitFor(() => expect(screen.getByTestId("automation-memory-text")).toHaveTextContent("Empty."));
  });

  it("MemoryClearNeedsConfirm: no memory panel for an automation with memory off", async () => {
    await renderPage(daemonWith([ciAutomation()]));
    await click(screen.getByTestId("automation-history-2"));
    await screen.findByTestId("automation-audit-empty");
    expect(screen.queryByTestId("automation-memory-panel")).toBeNull();
  });

  it("FireNowShowsDecision: a capped answer is text, not success; a fire names the task", async () => {
    const d = daemonWith([automation()]);
    d.onMutation["automation.fire"] = () => auditRow({ id: 5, manual: true, decision: "capped", reason: "2 runs in the last hour" });
    await renderPage(d);
    await click(screen.getByTestId("automation-fire-1"));
    const result = screen.getByTestId("automation-fire-result-1");
    expect(result).toHaveTextContent("Not started: capped (2 runs in the last hour).");
    expect(result).not.toHaveTextContent("Started");
    expect(result.className).not.toContain("text-success");
    expect(screen.queryByTestId("automation-row-error-1")).toBeNull();

    d.onMutation["automation.fire"] = () => auditRow({ id: 6, manual: true, decision: "fire", taskId: 55 });
    await click(screen.getByTestId("automation-fire-1"));
    expect(screen.getByTestId("automation-fire-result-1")).toHaveTextContent("Started task #55.");
  });
});
