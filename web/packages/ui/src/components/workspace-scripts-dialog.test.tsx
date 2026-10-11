import { act } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { FakeWsClient } from "@/test/fake-ws-client";
import { WorkspaceScriptsDialog } from "@/components/workspace-scripts-dialog";
import type { Workspace, WorkspaceWithScripts } from "@/lib/types";
import type { WsClient } from "@/lib/ws-client";

const WORKSPACE: Workspace = {
  ID: 7,
  Path: "/repo",
  Title: "Repo",
  RoutingPolicy: "",
  HasSetupScript: true,
  HasTeardownScript: false,
  CreatedAt: "2024-01-01T00:00:00Z",
  UpdatedAt: "2024-01-01T00:00:00Z",
};

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
  });
}

async function openDialog(scripts: { SetupScript: string; TeardownScript: string }) {
  const client = new FakeWsClient();
  const onSaved = vi.fn();
  const onOpenChange = vi.fn();
  render(
    <WorkspaceScriptsDialog
      client={client as unknown as WsClient}
      workspace={WORKSPACE}
      open
      onOpenChange={onOpenChange}
      onSaved={onSaved}
    />,
  );
  expect(client.calls[0]).toMatchObject({ method: "workspace.get", params: { id: 7 } });
  const loaded: WorkspaceWithScripts = { ...WORKSPACE, ...scripts };
  await act(async () => client.calls[0]!.resolve(loaded));
  await flush();
  return { client, onSaved, onOpenChange };
}

describe("workspace setup editor", () => {
  it("saves via workspace.setScripts", async () => {
    const { client, onSaved, onOpenChange } = await openDialog({ SetupScript: "npm ci", TeardownScript: "" });

    expect(screen.getByLabelText("Setup script")).toHaveValue("npm ci");
    fireEvent.change(screen.getByLabelText("Teardown script"), { target: { value: "rm -rf node_modules" } });
    fireEvent.click(screen.getByTestId("workspace-scripts-save"));

    expect(client.calls[1]).toMatchObject({
      method: "workspace.setScripts",
      params: { workspaceId: 7, setupScript: "npm ci", teardownScript: "rm -rf node_modules" },
    });
    await act(async () => client.calls[1]!.resolve(WORKSPACE));
    await flush();
    expect(onSaved).toHaveBeenCalled();
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it("clearing sends empty string", async () => {
    const { client } = await openDialog({ SetupScript: "npm ci", TeardownScript: "echo bye" });

    fireEvent.change(screen.getByLabelText("Setup script"), { target: { value: "" } });
    fireEvent.click(screen.getByTestId("workspace-scripts-save"));

    expect(client.calls[1]!.params).toEqual({ workspaceId: 7, setupScript: "", teardownScript: "echo bye" });
  });

  it("shows the daemon error", async () => {
    const { client, onSaved, onOpenChange } = await openDialog({ SetupScript: "", TeardownScript: "" });

    fireEvent.change(screen.getByLabelText("Setup script"), { target: { value: "x" } });
    fireEvent.click(screen.getByTestId("workspace-scripts-save"));
    await act(async () => client.calls[1]!.reject(new Error("workspace.setScripts: script too large")));
    await flush();

    expect(screen.getByRole("alert")).toHaveTextContent("script too large");
    expect(onSaved).not.toHaveBeenCalled();
    expect(onOpenChange).not.toHaveBeenCalled();
    expect(screen.getByTestId("workspace-scripts-save")).toBeEnabled();
  });
});
