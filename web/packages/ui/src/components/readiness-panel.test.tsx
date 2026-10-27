import { act } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { FakeWsClient } from "@/test/fake-ws-client";
import { ReadinessPanel } from "@/components/readiness-panel";
import type { WorkspaceReadiness } from "@/lib/types";
import type { WsClient } from "@/lib/ws-client";

const RESULT: WorkspaceReadiness = {
  workspaceId: 7,
  checkedAt: "2026-10-27T00:00:00Z",
  findings: [
    {
      id: "docs",
      status: "pass",
      title: "README or docs folder",
      evidence: "README.md",
      fix: "",
    },
    {
      id: "ci-config",
      status: "warn",
      title: "CI configuration is present",
      evidence: "no CI config found",
      fix: "Add a CI workflow.",
    },
    {
      id: "conventions",
      status: "info",
      title: "Conventions",
      evidence: "none found (optional)",
      fix: "",
    },
  ],
};

function mount() {
  const client = new FakeWsClient();
  render(
    <ReadinessPanel client={client as unknown as WsClient} workspaceId={7} />,
  );
  return client;
}

describe("ReadinessPanel", () => {
  it("shows loading, then findings with status and the fix line", async () => {
    const client = mount();
    expect(client.nth("workspace.readiness")).toMatchObject({
      params: { workspaceId: 7 },
    });
    expect(screen.getByText("Checking…")).toBeInTheDocument();
    expect(screen.getByTestId("readiness-recheck")).toBeDisabled();
    await act(async () => client.nth("workspace.readiness").resolve(RESULT));
    expect(screen.getByTestId("readiness-docs")).toHaveAttribute(
      "data-status",
      "pass",
    );
    expect(screen.getByTestId("readiness-ci-config")).toHaveAttribute(
      "data-status",
      "warn",
    );
    expect(screen.getByText("Add a CI workflow.")).toBeInTheDocument();
    expect(screen.getByText(/1 suggestion\./)).toBeInTheDocument();
    expect(screen.getByTestId("readiness-recheck")).toBeEnabled();
  });

  it("shows the error and recovers on Re-check", async () => {
    const client = mount();
    await act(async () =>
      client
        .nth("workspace.readiness")
        .reject(new Error("workspace.readiness: boom")),
    );
    expect(screen.getByTestId("readiness-error")).toHaveTextContent("boom");
    fireEvent.click(screen.getByTestId("readiness-recheck"));
    expect(
      client.calls.filter((c) => c.method === "workspace.readiness"),
    ).toHaveLength(2);
    await act(async () => client.nth("workspace.readiness", 1).resolve(RESULT));
    expect(screen.queryByTestId("readiness-error")).toBeNull();
    expect(screen.getByTestId("readiness-list")).toBeInTheDocument();
  });

  it("Re-check calls the RPC again and keeps the old list visible while it runs", async () => {
    const client = mount();
    await act(async () => client.nth("workspace.readiness").resolve(RESULT));
    fireEvent.click(screen.getByTestId("readiness-recheck"));
    expect(
      client.calls.filter((c) => c.method === "workspace.readiness"),
    ).toHaveLength(2);
    expect(screen.getByTestId("readiness-list")).toHaveAttribute(
      "aria-busy",
      "true",
    );
    await act(async () =>
      client
        .nth("workspace.readiness", 1)
        .resolve({ ...RESULT, findings: [RESULT.findings[0]!] }),
    );
    expect(screen.getByText("Nothing to fix.")).toBeInTheDocument();
    expect(screen.queryByTestId("readiness-ci-config")).toBeNull();
  });
});
