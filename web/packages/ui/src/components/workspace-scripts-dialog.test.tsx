import { act } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { FakeWsClient } from "@/test/fake-ws-client";
import { WorkspaceScriptsDialog } from "@/components/workspace-scripts-dialog";
import type {
  Workspace,
  WorkspaceDefaultBase,
  WorkspaceWithScripts,
} from "@/lib/types";
import type { CallOptions, EventFunc, WsClient } from "@/lib/ws-client";

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

const AUTO_BASE: WorkspaceDefaultBase = {
  ref: "develop",
  detected: true,
  override: "",
};
const OVERRIDE_BASE: WorkspaceDefaultBase = {
  ref: "develop",
  detected: false,
  override: "main",
};

/** workspace.readiness (the panel's own call) is answered at once and unrecorded, so call indexes below stay the editor's. */
class ScriptsClient extends FakeWsClient {
  override callStream<T = unknown>(
    method: string,
    params?: unknown,
    onEvent?: EventFunc,
    options?: CallOptions,
  ): Promise<T> {
    if (method === "workspace.readiness")
      return Promise.resolve({
        workspaceId: 7,
        findings: [],
        checkedAt: "",
      } as T);
    return super.callStream<T>(method, params, onEvent, options);
  }
}

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
  });
}

async function openDialog(
  scripts: { SetupScript: string; TeardownScript: string },
  base: WorkspaceDefaultBase = AUTO_BASE,
) {
  const client = new ScriptsClient();
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
  expect(client.calls[0]).toMatchObject({
    method: "workspace.get",
    params: { id: 7 },
  });
  expect(client.calls[1]).toMatchObject({
    method: "workspace.defaultBase",
    params: { workspaceId: 7 },
  });
  const loaded: WorkspaceWithScripts = { ...WORKSPACE, ...scripts };
  await act(async () => {
    client.calls[0]!.resolve(loaded);
    client.calls[1]!.resolve(base);
  });
  await flush();
  return { client, onSaved, onOpenChange };
}

describe("workspace setup editor", () => {
  it("saves via workspace.setScripts", async () => {
    const { client, onSaved, onOpenChange } = await openDialog({
      SetupScript: "npm ci",
      TeardownScript: "",
    });

    expect(screen.getByText("Worktree settings")).toBeInTheDocument();
    expect(screen.getByLabelText("Setup script")).toHaveValue("npm ci");
    fireEvent.change(screen.getByLabelText("Teardown script"), {
      target: { value: "rm -rf node_modules" },
    });
    fireEvent.click(screen.getByTestId("workspace-scripts-save"));

    expect(client.calls[2]).toMatchObject({
      method: "workspace.setScripts",
      params: {
        workspaceId: 7,
        setupScript: "npm ci",
        teardownScript: "rm -rf node_modules",
      },
    });
    await act(async () => client.calls[2]!.resolve(WORKSPACE));
    await flush();
    expect(onSaved).toHaveBeenCalled();
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it("clearing sends empty string", async () => {
    const { client } = await openDialog({
      SetupScript: "npm ci",
      TeardownScript: "echo bye",
    });

    fireEvent.change(screen.getByLabelText("Setup script"), {
      target: { value: "" },
    });
    fireEvent.click(screen.getByTestId("workspace-scripts-save"));

    expect(client.calls[2]!.params).toEqual({
      workspaceId: 7,
      setupScript: "",
      teardownScript: "echo bye",
    });
  });

  it("shows the daemon error", async () => {
    const { client, onSaved, onOpenChange } = await openDialog({
      SetupScript: "",
      TeardownScript: "",
    });

    fireEvent.change(screen.getByLabelText("Setup script"), {
      target: { value: "x" },
    });
    fireEvent.click(screen.getByTestId("workspace-scripts-save"));
    await act(async () =>
      client.calls[2]!.reject(
        new Error("workspace.setScripts: script too large"),
      ),
    );
    await flush();

    expect(screen.getByRole("alert")).toHaveTextContent("script too large");
    expect(onSaved).not.toHaveBeenCalled();
    expect(onOpenChange).not.toHaveBeenCalled();
    expect(screen.getByTestId("workspace-scripts-save")).toBeEnabled();
  });

  it("shows the detected default base and its helper text", async () => {
    await openDialog({ SetupScript: "", TeardownScript: "" });

    const input = screen.getByTestId("workspace-default-base");
    expect(input).toHaveValue("");
    expect(input).toHaveAttribute("placeholder", "develop");
    expect(
      screen.getByText(
        "Auto-detected: develop. Leave empty to keep auto-detecting.",
      ),
    ).toBeInTheDocument();
  });

  it("shows the override helper text when an override is stored", async () => {
    await openDialog({ SetupScript: "", TeardownScript: "" }, OVERRIDE_BASE);

    expect(screen.getByTestId("workspace-default-base")).toHaveValue("main");
    expect(
      screen.getByText(
        "Override in use. Clear the field to go back to auto-detect (develop).",
      ),
    ).toBeInTheDocument();
  });

  it("saves an override through workspace.setDefaultBase before the scripts", async () => {
    const { client, onSaved } = await openDialog({
      SetupScript: "npm ci",
      TeardownScript: "",
    });

    fireEvent.change(screen.getByTestId("workspace-default-base"), {
      target: { value: " release/x " },
    });
    fireEvent.click(screen.getByTestId("workspace-scripts-save"));

    expect(client.calls[2]).toMatchObject({
      method: "workspace.setDefaultBase",
      params: { workspaceId: 7, branch: "release/x" },
    });
    expect(client.calls[3]).toBeUndefined();
    await act(async () =>
      client.calls[2]!.resolve({
        ref: "release/x",
        detected: false,
        override: "release/x",
      }),
    );
    await flush();
    expect(client.calls[3]).toMatchObject({ method: "workspace.setScripts" });
    await act(async () => client.calls[3]!.resolve(WORKSPACE));
    await flush();
    expect(onSaved).toHaveBeenCalled();
  });

  it("clears the override with an empty branch", async () => {
    const { client } = await openDialog(
      { SetupScript: "", TeardownScript: "" },
      OVERRIDE_BASE,
    );

    fireEvent.change(screen.getByTestId("workspace-default-base"), {
      target: { value: "" },
    });
    fireEvent.click(screen.getByTestId("workspace-scripts-save"));

    expect(client.calls[2]).toMatchObject({
      method: "workspace.setDefaultBase",
      params: { workspaceId: 7, branch: "" },
    });
  });

  it("shows the daemon's validation sentence and keeps the dialog open", async () => {
    const { client, onSaved, onOpenChange } = await openDialog({
      SetupScript: "",
      TeardownScript: "",
    });

    fireEvent.change(screen.getByTestId("workspace-default-base"), {
      target: { value: "nope" },
    });
    fireEvent.click(screen.getByTestId("workspace-scripts-save"));
    await act(async () =>
      client.calls[2]!.reject(
        new Error(
          'set default base: branch "nope" not found locally or as origin/nope',
        ),
      ),
    );
    await flush();

    expect(screen.getByRole("alert")).toHaveTextContent(
      'set default base: branch "nope" not found',
    );
    expect(
      client.calls.filter((c) => c.method === "workspace.setScripts"),
    ).toHaveLength(0);
    expect(onSaved).not.toHaveBeenCalled();
    expect(onOpenChange).not.toHaveBeenCalled();
  });

  it("does not call setDefaultBase when the field is unchanged", async () => {
    const { client } = await openDialog(
      { SetupScript: "", TeardownScript: "" },
      OVERRIDE_BASE,
    );

    fireEvent.click(screen.getByTestId("workspace-scripts-save"));

    expect(
      client.calls.filter((c) => c.method === "workspace.setDefaultBase"),
    ).toHaveLength(0);
    expect(client.calls[2]).toMatchObject({ method: "workspace.setScripts" });
  });
});
