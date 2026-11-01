import { act } from "react";
import { screen, within } from "@testing-library/react";
import { renderWithClientProp as render } from "@/test/render-with-daemon";
import { describe, expect, it } from "vitest";

import { listSettingsSections } from "@/components/settings/settings-registry";
import "@/components/settings/policy-section";
import { FakeWsClient } from "@/test/fake-ws-client";
import type { PolicyAuditRow, PolicyStatus, PolicyRule } from "@/lib/policy-types";

function policyStatus(overrides: Partial<PolicyStatus> = {}): PolicyStatus {
  return {
    enabled: true,
    valid: true,
    exists: true,
    error: "",
    path: "/home/lou/.smind/policy.yaml",
    sha: "abc123",
    rules: [],
    ...overrides,
  };
}

function rule(overrides: Partial<PolicyRule> = {}): PolicyRule {
  return {
    id: "allow-git-status",
    action: "allow",
    why: "Read-only git is safe",
    ...overrides,
  };
}

function decision(overrides: Partial<PolicyAuditRow> = {}): PolicyAuditRow {
  return {
    id: 1,
    createdAt: new Date(Date.now() - 5 * 60_000).toISOString(),
    runId: "run-1",
    taskId: 11,
    requestId: "req-1",
    seat: "lou",
    workspaceId: 1,
    tool: "Bash",
    target: "git status",
    paths: [],
    decision: "allow",
    ruleId: "allow-git-status",
    policySha: "abc123",
    reasons: "",
    latencyMs: 2,
    ...overrides,
  };
}

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

function renderSection() {
  const client = new FakeWsClient();
  const section = listSettingsSections().find((s) => s.id === "permission-policy");
  if (!section) throw new Error("permission-policy section did not register");
  render(<>{section.render({ client: client as never, events: null })}</>);
  return client;
}

describe("policy-section", () => {
  it("registers as the permission-policy section in Agents & providers", () => {
    const section = listSettingsSections().find((s) => s.id === "permission-policy");
    expect(section).toBeDefined();
    expect(section?.label).toBe("Permission policy");
    expect(section?.groupLabel).toBe("Agents & providers");
  });

  it("policy settings page is read-only", async () => {
    const client = renderSection();
    client.nth("policy.get").resolve(
      policyStatus({
        rules: [
          rule({ id: "allow-git-status", tool: "Bash", argv0: "git", subcommand: "status" }),
          rule({ id: "deny-rm", action: "deny", why: "Never delete files unattended", seats: ["lou"] }),
        ],
      }),
    );
    client.nth("policy.audit").resolve({
      rows: [decision(), decision({ id: 2, decision: "ask", ruleId: "", tool: "WebFetch", target: "https://example.com" })],
      next: 0,
    });
    await flush();

    const rules = screen.getByTestId("policy-rules");
    expect(within(rules).getByTestId("policy-rule-allow-git-status")).toHaveTextContent("allow-git-status");
    expect(within(rules).getByTestId("policy-rule-allow-git-status")).toHaveTextContent("tool Bash, argv0 git, subcommand status");
    expect(within(rules).getByTestId("policy-rule-deny-rm")).toHaveTextContent("Never delete files unattended");
    expect(within(rules).getByTestId("policy-rule-deny-rm")).toHaveTextContent("seats lou");

    const audit = screen.getByTestId("policy-audit");
    expect(within(audit).getByTestId("policy-decision-1")).toHaveTextContent("allow");
    expect(within(audit).getByTestId("policy-decision-1")).toHaveTextContent("Bash git status");
    expect(within(audit).getByTestId("policy-decision-2")).toHaveTextContent("ask");
    expect(within(audit).getByTestId("policy-decision-2")).toHaveTextContent("—");

    expect(screen.getByTestId("policy-path")).toHaveTextContent("/home/lou/.smind/policy.yaml");

    // Read-only: nothing on the page writes anything.
    expect(screen.queryAllByRole("button")).toHaveLength(0);
    expect(screen.queryAllByRole("switch")).toHaveLength(0);
    expect(screen.queryAllByRole("checkbox")).toHaveLength(0);
    expect(screen.queryAllByRole("textbox")).toHaveLength(0);
  });

  it("policy settings page shows invalid banner", async () => {
    const client = renderSection();
    client.nth("policy.get").resolve(policyStatus({ valid: false, error: "line 3: unknown field" }));
    client.nth("policy.audit").resolve({ rows: [], next: 0 });
    await flush();

    const banner = screen.getByTestId("policy-validity");
    expect(banner).toHaveTextContent("Policy file is invalid");
    expect(banner).toHaveTextContent("line 3: unknown field");
  });

  it("policy settings page tolerates an unknown action", async () => {
    const client = renderSection();
    client.nth("policy.get").resolve(policyStatus({ rules: [rule({ id: "frob", action: "frobnicate" })] }));
    client.nth("policy.audit").resolve({ rows: [], next: 0 });
    await flush();

    const row = screen.getByTestId("policy-rule-frob");
    expect(row).toHaveTextContent("frobnicate");
    expect(within(row).getByText("frobnicate").closest("[data-slot='status-badge']")).toHaveAttribute("data-status", "neutral");
  });

  it("policy settings page no file", async () => {
    const client = renderSection();
    client.nth("policy.get").resolve(policyStatus({ exists: false }));
    client.nth("policy.audit").resolve({ rows: [], next: 0 });
    await flush();

    const banner = screen.getByTestId("policy-validity");
    expect(banner).toHaveTextContent("No policy file yet");
    expect(banner).toHaveTextContent("Every request that is not floor-denied asks you.");
  });
});
