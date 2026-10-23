import { render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { OptionsCard } from "@/components/permission/options-card";
import type { PendingPermission } from "@/hooks/use-run-timeline";
import type { PermissionDetail } from "@/lib/types";

function renderCard(detail?: PermissionDetail) {
  const pending: PendingPermission = {
    requestId: "req-1",
    summary: "Write file: a.sql",
    options: [
      { id: "allow-1", label: "Allow", kind: "allow_once" },
      { id: "deny-1", label: "Deny", kind: "reject_once" },
    ],
    detail,
  };
  render(<OptionsCard runId="run-1" pending={pending} onRespond={vi.fn()} />);
}

describe("PermissionDetailView in OptionsCard", () => {
  it("renders the overwrite warning for a replacing write", () => {
    renderCard({ kind: "write", path: "internal/store/schema.sql", replacesExisting: true, existingLines: 30, newLines: 28 });
    expect(screen.getByTestId("permission-detail-replaces").textContent).toBe("Replaces existing file");
    expect(screen.getByTestId("permission-detail-path").textContent).toBe("internal/store/schema.sql");
    expect(screen.queryByTestId("permission-detail-shrink")).toBeNull();
  });

  it("escalates a large shrink to a danger callout", () => {
    renderCard({ kind: "write", path: "a.sql", replacesExisting: true, largeShrink: true, existingLines: 240, newLines: 7, diff: "-a\n+b" });
    const callout = screen.getByRole("alert");
    expect(callout.textContent).toContain("Overwrites 240 lines with 7");
    expect(callout.className).toContain("text-destructive");
    expect((screen.getByTestId("permission-detail-body") as HTMLDetailsElement).open).toBe(true);
  });

  it("renders the size delta and a collapsible diff", () => {
    renderCard({
      kind: "edit",
      path: "a.go",
      existingLines: 240,
      newLines: 7,
      existingBytes: 3174,
      newBytes: 205,
      diff: "@@ -1 +1 @@\n-old\n+new\n ctx",
    });
    expect(screen.getByTestId("permission-detail-size").textContent).toBe("240 → 7 lines · 3.1 KB → 0.2 KB");
    const body = screen.getByTestId("permission-detail-body") as HTMLDetailsElement;
    expect(body.open).toBe(false);
    expect(within(body).getByText("Diff")).toBeTruthy();
    expect(screen.getByText("+new").className).toContain("text-diff-added");
    expect(screen.getByText("-old").className).toContain("text-diff-removed");
  });

  it("shows new file summary without a replace badge", () => {
    renderCard({ kind: "write", path: "new.txt", newLines: 12, newBytes: 410, preview: "hello" });
    expect(screen.getByTestId("permission-detail-size").textContent).toBe("new file · 12 lines · 0.4 KB");
    expect(screen.queryByTestId("permission-detail-replaces")).toBeNull();
    expect(screen.getByText("Preview")).toBeTruthy();
  });

  it("flags a write outside the task folder", () => {
    renderCard({ kind: "write", path: "/etc/hosts", outsideWorktree: true });
    expect(screen.getByTestId("permission-detail-outside").textContent).toBe("Outside the task folder");
  });

  it("renders nothing extra for a permission with no detail", () => {
    renderCard(undefined);
    expect(screen.queryByTestId("permission-detail")).toBeNull();
    expect(screen.getByRole("button", { name: "Allow" })).toBeTruthy();
  });

  it("shows the command for an execute request", () => {
    renderCard({ kind: "execute", command: "rm -rf build" });
    expect(screen.getByTestId("permission-detail-command").textContent).toContain("rm -rf build");
  });

  it("shows truncated and redacted notes", () => {
    renderCard({ kind: "write", path: "/x", redacted: true, truncated: true, preview: "p" });
    expect(screen.getByTestId("permission-detail-redacted").textContent).toBe("Existing contents hidden (restricted path)");
    expect(screen.getByTestId("permission-detail-truncated").textContent).toBe("Truncated");
  });

  it("shows the git rule line and no always-allow for a git rule", () => {
    renderCard({
      kind: "execute",
      command: "git commit -m x",
      gitRule: {
        id: "git-commit",
        class: "commit",
        label: "git commit",
        command: "git commit -m x",
        remote: "origin",
        branch: "smind/task-x",
        perCall: true,
        stagedFiles: ["a.go", "b.go"],
      },
    });
    expect(screen.getByTestId("permission-git-rule-label").textContent).toContain("git commit");
    expect(screen.getByTestId("permission-git-rule-label").textContent).toContain("asks before committing");
    expect(screen.getByTestId("permission-git-rule-command").textContent).toBe("git commit -m x");
    expect(screen.getByTestId("permission-git-rule-target").textContent).toBe("Remote origin · Branch smind/task-x");
    expect(screen.getByTestId("permission-git-rule-staged").textContent).toContain("a.go");
    expect(screen.getByTestId("permission-git-rule-staged").textContent).toContain("b.go");
  });

  it("hides always-allow options when a git rule is present", () => {
    const pending: PendingPermission = {
      requestId: "req-2",
      summary: "git push",
      options: [
        { id: "a1", label: "Allow once", kind: "allow_once" },
        { id: "a2", label: "Always allow", kind: "allow_always" },
        { id: "r1", label: "Reject", kind: "reject_once" },
        { id: "r2", label: "Always reject", kind: "reject_always" },
      ],
      detail: {
        kind: "execute",
        gitRule: { id: "git-push", class: "always-ask", label: "git push always needs your approval", command: "git push", perCall: true },
      },
    };
    render(<OptionsCard runId="run-1" pending={pending} onRespond={vi.fn()} />);
    expect(screen.getByText("Allow once")).toBeTruthy();
    expect(screen.queryByText("Always allow")).toBeNull();
    expect(screen.queryByText("Always reject")).toBeNull();
  });
});
