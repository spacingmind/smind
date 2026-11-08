import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { optionVariant, recommendedOptionId } from "@/components/permission/permission-option-button";
import { PermissionCard } from "@/components/permission/permission-card";
import { PLAN_REVIEW_APPROVE, PLAN_REVIEW_REFUSE } from "@/components/permission/plan-review-card";
import type { PendingPermission } from "@/hooks/use-run-timeline";

function pending(overrides: Partial<PendingPermission> = {}): PendingPermission {
  return {
    requestId: "req-1",
    summary: "Run a risky command",
    options: [
      { id: "allow-1", label: "Allow", kind: "allow_once" },
      { id: "deny-1", label: "Deny", kind: "reject_once" },
    ],
    ...overrides,
  };
}

async function flush(): Promise<void> {
  await Promise.resolve();
  await Promise.resolve();
}

describe("optionVariant / recommendedOptionId", () => {
  it("styles reject kinds as destructive regardless of position", () => {
    expect(optionVariant("reject_once", false)).toBe("destructive");
    expect(optionVariant("reject_always", true)).toBe("destructive");
  });

  it("styles the first allow-kind option as the recommended (primary) action", () => {
    const options = [
      { id: "a", label: "Deny", kind: "reject_once" },
      { id: "b", label: "Allow once", kind: "allow_once" },
      { id: "c", label: "Allow always", kind: "allow_always" },
    ];
    expect(recommendedOptionId(options)).toBe("b");
    expect(optionVariant("allow_once", true)).toBe("approval");
    expect(optionVariant("allow_always", false)).toBe("outline");
  });
});

describe("PermissionCard: options variant", () => {
  it("renders every option and styles allow/deny distinctly", () => {
    render(<PermissionCard runId="run-1" pending={pending()} onRespond={vi.fn()} onChat={vi.fn()} />);

    const card = screen.getByTestId("pending-permission");
    const allow = within(card).getByRole("button", { name: "Allow" });
    const deny = within(card).getByRole("button", { name: "Deny" });
    expect(allow).toHaveAttribute("data-option-kind", "allow_once");
    expect(deny).toHaveAttribute("data-option-kind", "reject_once");
    expect(allow.className).not.toBe(deny.className);
  });

  // visual-identity-console Item 7: the recommended-allow option adopts
  // the "approval" variant (Item 4) instead of the generic "default".
  it("the recommended allow option renders with the approval variant", () => {
    render(<PermissionCard runId="run-1" pending={pending()} onRespond={vi.fn()} onChat={vi.fn()} />);
    expect(screen.getByRole("button", { name: "Allow" })).toHaveAttribute("data-variant", "approval");
  });

  // visual-identity-console Item 7: the card itself is the "floating"
  // elevation tier (§13) -- surface-1 background plus shadow-lg -- not an
  // ad hoc shadow/border of its own.
  it("the outer card carries the floating elevation recipe", () => {
    render(<PermissionCard runId="run-1" pending={pending()} onRespond={vi.fn()} onChat={vi.fn()} />);
    const outer = screen.getByTestId("permission-card");
    expect(outer.className).toContain("bg-card");
    expect(outer.className).toContain("shadow-lg");
  });

  // reference-visual-parity P3: every blocking request reads as the same
  // "waiting" state, in ZCode's one green confirmation treatment --
  // never the generic success color.
  it("carries the confirmation-tone waiting badge", () => {
    render(<PermissionCard runId="run-1" pending={pending()} onRespond={vi.fn()} onChat={vi.fn()} />);
    const badge = screen.getByTestId("permission-waiting-badge");
    expect(badge.className).toContain("bg-interaction-confirmation-surface");
    expect(badge.className).toContain("text-interaction-confirmation-foreground");
  });

  // Item 21: touch targets need a stated minimum below the compact
  // breakpoint -- jsdom has no layout, so this asserts the class list
  // carries both the 44px compact rule and the `md:`-scoped revert to the
  // original dense size, rather than a computed pixel height.
  it("options carry the compact 44px touch-target class, reverting to the dense size at md:", () => {
    render(<PermissionCard runId="run-1" pending={pending()} onRespond={vi.fn()} onChat={vi.fn()} />);

    const allow = screen.getByRole("button", { name: "Allow" });
    expect(allow.className).toContain("h-11");
    expect(allow.className).toContain("md:h-6");
  });

  it("renders every option even when no kind is recognised", () => {
    render(
      <PermissionCard
        runId="run-1"
        pending={pending({ options: [{ id: "x", label: "Do it", kind: "" }] })}
        onRespond={vi.fn()}
        onChat={vi.fn()}
      />,
    );
    expect(screen.getByRole("button", { name: "Do it" })).toBeInTheDocument();
  });

  // ADR-0047's "what we are NOT doing": no bulk approve, no "always allow", no merge on any card.
  it("permission card has no always-allow or approve-all", () => {
    render(
      <PermissionCard
        runId="run-1"
        pending={pending({
          summary: "git push origin feat",
          detail: {
            kind: "execute",
            command: "git push origin feat",
            policy: { ruleId: "git-push-own", why: "Lead pushes its own task branch", normalized: "git push origin feat" },
          },
        })}
        onRespond={vi.fn()}
        onChat={vi.fn()}
      />,
    );
    expect(screen.getByTestId("permission-policy-rule").textContent).toContain("git-push-own");
    expect(screen.queryByRole("button", { name: /always allow|allow all|approve all|merge/i })).toBeNull();
    expect(screen.queryByText(/always allow|approve all/i)).toBeNull();
  });

  it("is focusable as a group and moves focus to itself when a new request arrives", () => {
    const { rerender } = render(
      <PermissionCard runId="run-1" pending={pending({ requestId: "req-1" })} onRespond={vi.fn()} onChat={vi.fn()} />,
    );
    const group = screen.getByRole("group", { name: "Run a risky command" });
    expect(group).toHaveAttribute("tabindex", "-1");
    expect(document.activeElement).toBe(group);

    // A second, different request re-focuses the card.
    (group as HTMLElement).blur();
    rerender(
      <PermissionCard
        runId="run-1"
        pending={pending({ requestId: "req-2", summary: "Run another risky command" })}
        onRespond={vi.fn()}
        onChat={vi.fn()}
      />,
    );
    expect(document.activeElement).toBe(screen.getByRole("group", { name: "Run another risky command" }));
  });

  it("does not steal focus from a text field the user is actively typing in when a new request arrives", () => {
    function Harness({ requestId }: { requestId: string }) {
      return (
        <>
          <textarea aria-label="composer" />
          <PermissionCard runId="run-1" pending={pending({ requestId })} onRespond={vi.fn()} onChat={vi.fn()} />
        </>
      );
    }

    const { rerender } = render(<Harness requestId="req-1" />);
    const composer = screen.getByLabelText("composer");
    composer.focus();
    expect(document.activeElement).toBe(composer);

    // A second, different request must not yank focus off the field the
    // user is mid-keystroke in.
    rerender(<Harness requestId="req-2" />);
    expect(document.activeElement).toBe(composer);
  });

  it("options are real buttons, reachable in tab order and activatable by click", async () => {
    const onRespond = vi.fn().mockResolvedValue(undefined);
    render(<PermissionCard runId="run-1" pending={pending()} onRespond={onRespond} onChat={vi.fn()} />);

    const allow = screen.getByRole("button", { name: "Allow" });
    expect(allow.tagName).toBe("BUTTON");
    expect(allow).not.toHaveAttribute("tabindex", "-1");

    allow.focus();
    expect(document.activeElement).toBe(allow);
    fireEvent.click(allow);
    await flush();
    expect(onRespond).toHaveBeenCalledWith("run-1", "req-1", "allow-1");
  });

  it("deny_reason_is_sent_only_with_deny", async () => {
    const onRespond = vi.fn().mockResolvedValue(undefined);
    const { unmount } = render(<PermissionCard runId="run-1" pending={pending()} onRespond={onRespond} onChat={vi.fn()} />);

    fireEvent.change(screen.getByTestId("permission-deny-reason"), { target: { value: "  use the staged schema instead  " } });
    fireEvent.click(screen.getByRole("button", { name: "Deny" }));
    await flush();
    expect(onRespond).toHaveBeenCalledWith("run-1", "req-1", "deny-1", "use the staged schema instead");
    expect(onRespond).toHaveBeenCalledTimes(1);

    // A whitespace-only reason is no reason: the exact old three-arg call.
    onRespond.mockClear();
    unmount();
    render(<PermissionCard runId="run-1" pending={pending()} onRespond={onRespond} onChat={vi.fn()} />);
    fireEvent.change(screen.getByTestId("permission-deny-reason"), { target: { value: "   " } });
    fireEvent.click(screen.getByRole("button", { name: "Deny" }));
    await flush();
    expect(onRespond).toHaveBeenCalledTimes(1);
    expect(onRespond).toHaveBeenCalledWith("run-1", "req-1", "deny-1");
  });

  it("deny_reason_is_capped_at_500", () => {
    render(<PermissionCard runId="run-1" pending={pending()} onRespond={vi.fn()} onChat={vi.fn()} />);
    expect(screen.getByTestId("permission-deny-reason")).toHaveAttribute("maxlength", "500");
  });

  it("allow_never_sends_the_reason", async () => {
    const onRespond = vi.fn().mockResolvedValue(undefined);
    render(<PermissionCard runId="run-1" pending={pending()} onRespond={onRespond} onChat={vi.fn()} />);

    fireEvent.change(screen.getByTestId("permission-deny-reason"), { target: { value: "not for allow to see" } });
    fireEvent.click(screen.getByRole("button", { name: "Allow" }));
    await flush();
    expect(onRespond).toHaveBeenCalledTimes(1);
    expect(onRespond).toHaveBeenCalledWith("run-1", "req-1", "allow-1");
  });

  it("old_onRespond_callers_unchanged", async () => {
    const onRespond = vi.fn().mockResolvedValue(undefined);
    const { unmount } = render(<PermissionCard runId="run-1" pending={pending()} onRespond={onRespond} onChat={vi.fn()} />);

    fireEvent.click(screen.getByRole("button", { name: "Deny" }));
    await flush();
    expect(onRespond).toHaveBeenCalledWith("run-1", "req-1", "deny-1");

    unmount();
    render(<PermissionCard runId="run-1" pending={pending()} onRespond={onRespond} onChat={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: "Allow" }));
    await flush();
    expect(onRespond).toHaveBeenCalledWith("run-1", "req-1", "allow-1");
  });
});

describe("PermissionCard: plan-review variant", () => {
  const withPlan = pending({ plan: "# Do the thing\n\n- step one\n- step two" });

  it("renders the plan as markdown with the three actions", () => {
    render(<PermissionCard runId="run-1" pending={withPlan} onRespond={vi.fn()} onChat={vi.fn()} />);

    expect(within(screen.getByTestId("plan-review")).getByRole("heading", { level: 1 })).toHaveTextContent("Do the thing");
    expect(screen.getByTestId("plan-review-chat")).toBeInTheDocument();
    expect(screen.getByTestId("plan-review-refuse")).toBeInTheDocument();
    expect(screen.getByTestId("plan-review-approve")).toBeInTheDocument();
  });

  it("approve/refuse resolve the permission; chat only calls back without resolving", async () => {
    const onRespond = vi.fn().mockResolvedValue(undefined);
    const onChat = vi.fn();
    render(<PermissionCard runId="run-1" pending={withPlan} onRespond={onRespond} onChat={onChat} />);

    fireEvent.click(screen.getByTestId("plan-review-chat"));
    expect(onChat).toHaveBeenCalledTimes(1);
    expect(onRespond).not.toHaveBeenCalled();

    fireEvent.click(screen.getByTestId("plan-review-approve"));
    await flush();
    expect(onRespond).toHaveBeenCalledWith("run-1", "req-1", PLAN_REVIEW_APPROVE);
  });

  it("refuse sends the refuse option", async () => {
    const onRespond = vi.fn().mockResolvedValue(undefined);
    render(<PermissionCard runId="run-1" pending={withPlan} onRespond={onRespond} onChat={vi.fn()} />);

    fireEvent.click(screen.getByTestId("plan-review-refuse"));
    await flush();
    expect(onRespond).toHaveBeenCalledWith("run-1", "req-1", PLAN_REVIEW_REFUSE);
  });

  it("its actions carry the same compact touch-target class as the options variant (Item 21)", () => {
    render(<PermissionCard runId="run-1" pending={withPlan} onRespond={vi.fn()} onChat={vi.fn()} />);
    expect(screen.getByTestId("plan-review-approve").className).toContain("h-11");
    expect(screen.getByTestId("plan-review-approve").className).toContain("md:h-6");
  });
});
