import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { GrantControl } from "@/components/permission/grant-control";
import { PermissionCard } from "@/components/permission/permission-card";
import type { PendingPermission } from "@/hooks/use-run-timeline";
import { GRANT_BUTTON_LABEL, GRANT_OPT_IN_FALLBACK } from "@/lib/grant-copy";
import type { GrantOffer } from "@/lib/types";

const READONLY: GrantOffer = {
  command: "git status",
  project: "smind",
  folder: "/repo/smind",
  expiresInDays: 30,
  class: "readonly",
  requiresOptIn: false,
  note: "Saves this exact read-only command for this project for 30 days.",
};

const RUNNER: GrantOffer = {
  command: "make build",
  project: "smind",
  folder: "/repo/smind",
  expiresInDays: 30,
  class: "runner",
  requiresOptIn: true,
  optInLabel: "Also allow running this project's code (the agent can change it)",
  note: "This command runs code defined in this project (a Makefile, script or test) that the agent can change later. Saving it needs the extra opt-in.",
};

function pending(grant?: GrantOffer, extra: Partial<PendingPermission> = {}): PendingPermission {
  return {
    requestId: "req-1",
    summary: "run Bash",
    options: [
      { id: "allow-1", label: "Allow", kind: "allow_once" },
      { id: "deny-1", label: "Deny", kind: "reject_once" },
    ],
    detail: { kind: "execute", command: grant?.command ?? "ls", ...(grant ? { grant } : {}) },
    ...extra,
  };
}

describe("GrantControl", () => {
  it("states the exact scope: command, project and folder, how long, and the class as text", () => {
    render(<GrantControl offer={READONLY} disabled={false} onSave={vi.fn()} />);
    const c = screen.getByTestId("grant-control");
    expect(within(c).getByTestId("grant-command")).toHaveTextContent("git status");
    expect(within(c).getByTestId("grant-project")).toHaveTextContent("smind (/repo/smind)");
    expect(within(c).getByTestId("grant-expiry")).toHaveTextContent("30 days, or until you revoke it");
    expect(within(c).getByTestId("grant-class")).toHaveTextContent("Read-only");
    expect(within(c).getByTestId("grant-note")).toHaveTextContent(READONLY.note);
    expect(within(c).queryByTestId("grant-optin")).toBeNull();
  });

  it("a read-only grant saves with no opt-in and sends allowProjectCode false", () => {
    const onSave = vi.fn();
    render(<GrantControl offer={READONLY} disabled={false} onSave={onSave} />);
    const save = screen.getByRole("button", { name: GRANT_BUTTON_LABEL });
    expect(save).toBeEnabled();
    fireEvent.click(save);
    expect(onSave).toHaveBeenCalledWith(false);
  });

  it("a runner shows the opt-in UNTICKED, says what it means, and cannot be saved until it is ticked", () => {
    const onSave = vi.fn();
    render(<GrantControl offer={RUNNER} disabled={false} onSave={onSave} />);
    expect(screen.getByTestId("grant-class")).toHaveTextContent("Runs project code");
    const optin = screen.getByTestId("grant-optin");
    expect(optin).toHaveAttribute("aria-checked", "false");
    expect(screen.getByText(RUNNER.optInLabel as string)).toBeInTheDocument();
    expect(screen.getByTestId("grant-note")).toHaveTextContent("the agent can change later");
    const save = screen.getByRole("button", { name: GRANT_BUTTON_LABEL });
    expect(save).toBeDisabled();
    expect(screen.getByTestId("grant-hint")).toHaveTextContent("Tick the box above");
    fireEvent.click(save);
    expect(onSave).not.toHaveBeenCalled();
    fireEvent.click(optin);
    expect(save).toBeEnabled();
    expect(screen.queryByTestId("grant-hint")).toBeNull();
    fireEvent.click(save);
    expect(onSave).toHaveBeenCalledWith(true);
  });

  it("an unknown class is treated as a runner (never as read-only), with the fallback label", () => {
    render(<GrantControl offer={{ ...RUNNER, class: "mystery", requiresOptIn: false, optInLabel: undefined }} disabled={false} onSave={vi.fn()} />);
    expect(screen.getByText(GRANT_OPT_IN_FALLBACK)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: GRANT_BUTTON_LABEL })).toBeDisabled();
  });

  it("a runner whose flag says no opt-in but whose class is runner still needs it (the class decides)", () => {
    render(<GrantControl offer={{ ...RUNNER, requiresOptIn: false }} disabled={false} onSave={vi.fn()} />);
    expect(screen.getByRole("button", { name: GRANT_BUTTON_LABEL })).toBeDisabled();
  });

  it("a read-only offer whose flag claims an opt-in shows none (the daemon would refuse it): the class decides both ways", () => {
    const onSave = vi.fn();
    render(<GrantControl offer={{ ...READONLY, requiresOptIn: true }} disabled={false} onSave={onSave} />);
    expect(screen.queryByTestId("grant-optin")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: GRANT_BUTTON_LABEL }));
    expect(onSave).toHaveBeenCalledWith(false);
  });

  it("renders agent-authored text inertly (no markup, no links)", () => {
    render(<GrantControl offer={{ ...READONLY, command: "echo <b>x</b> https://evil.example [a](https://evil.example)", project: "<i>p</i>" }} disabled={false} onSave={vi.fn()} />);
    const c = screen.getByTestId("grant-control");
    expect(c.querySelector("b, i, a")).toBeNull();
    expect(c).toHaveTextContent("echo <b>x</b> https://evil.example");
  });

  it("is disabled while an answer is in flight", () => {
    render(<GrantControl offer={READONLY} disabled onSave={vi.fn()} />);
    expect(screen.getByRole("button", { name: GRANT_BUTTON_LABEL })).toBeDisabled();
  });
});

describe("PermissionCard with a grant offer", () => {
  it("offers the control only when the daemon sent detail.grant (the demo and every other card have none)", () => {
    const { rerender } = render(<PermissionCard runId="r" pending={pending()} onRespond={vi.fn()} onAlwaysAllow={vi.fn()} onChat={vi.fn()} />);
    expect(screen.queryByTestId("grant-control")).toBeNull();
    rerender(<PermissionCard runId="r" pending={pending(READONLY)} onRespond={vi.fn()} onAlwaysAllow={vi.fn()} onChat={vi.fn()} />);
    expect(screen.getByTestId("grant-control")).toBeInTheDocument();
  });

  it("is not offered when the host gave no handler", () => {
    render(<PermissionCard runId="r" pending={pending(READONLY)} onRespond={vi.fn()} onChat={vi.fn()} />);
    expect(screen.queryByTestId("grant-control")).toBeNull();
  });

  it("is not offered for a git rule, even if an offer is present", () => {
    const withRule = pending(READONLY, { detail: { kind: "execute", command: "git push", grant: READONLY, gitRule: { id: "x", class: "always-ask", label: "push", command: "git push", perCall: true } } });
    render(<PermissionCard runId="r" pending={withRule} onRespond={vi.fn()} onAlwaysAllow={vi.fn()} onChat={vi.fn()} />);
    expect(screen.queryByTestId("grant-control")).toBeNull();
  });

  it("a ticked opt-in does not carry to the next request (a new requestId remounts the control, unticked)", () => {
    const first = pending(RUNNER, { requestId: "req-A" });
    const { rerender } = render(<PermissionCard runId="r" pending={first} onRespond={vi.fn()} onAlwaysAllow={vi.fn()} onChat={vi.fn()} />);
    fireEvent.click(screen.getByTestId("grant-optin"));
    expect(screen.getByTestId("grant-optin")).toHaveAttribute("aria-checked", "true");
    rerender(<PermissionCard runId="r" pending={pending(RUNNER, { requestId: "req-B" })} onRespond={vi.fn()} onAlwaysAllow={vi.fn()} onChat={vi.fn()} />);
    expect(screen.getByTestId("grant-optin")).toHaveAttribute("aria-checked", "false");
    expect(screen.getByTestId("grant-save")).toBeDisabled();
  });

  it("saving answers with the request's own single-allow option, not a typed one, and the runner's opt-in", async () => {
    const onAlwaysAllow = vi.fn().mockResolvedValue(undefined);
    render(<PermissionCard runId="run-9" pending={pending(RUNNER)} onRespond={vi.fn()} onAlwaysAllow={onAlwaysAllow} onChat={vi.fn()} />);
    fireEvent.click(screen.getByTestId("grant-optin"));
    fireEvent.click(screen.getByTestId("grant-save"));
    await waitFor(() => expect(onAlwaysAllow).toHaveBeenCalledWith("run-9", "req-1", "allow-1", true));
  });

  it("shows the daemon's refusal and keeps the card usable", async () => {
    const onAlwaysAllow = vi.fn().mockRejectedValue(new Error("this command runs project code: saving it needs the explicit opt-in"));
    render(<PermissionCard runId="r" pending={pending(READONLY)} onRespond={vi.fn()} onAlwaysAllow={onAlwaysAllow} onChat={vi.fn()} />);
    fireEvent.click(screen.getByTestId("grant-save"));
    await waitFor(() => expect(screen.getByTestId("permission-respond-error")).toHaveTextContent("this command runs project code"));
    expect(screen.getByTestId("permission-respond-error")).toHaveTextContent("Could not send your answer");
    expect(screen.getByTestId("grant-save")).toBeEnabled();
  });
});
