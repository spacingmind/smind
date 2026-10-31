import { act } from "react";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { listSettingsSections } from "@/components/settings/settings-registry";
import "@/components/settings/hosts-section";
import { FleetDaemon, fleetHost } from "@/test/fleet-fixtures";
import { renderWithDaemon } from "@/test/render-with-daemon";

async function flush(): Promise<void> {
  await act(async () => {
    for (let i = 0; i < 6; i++) await Promise.resolve();
  });
}

async function click(el: HTMLElement): Promise<void> {
  fireEvent.click(el);
  await flush();
}

async function openMenuItem(menuId: string, itemId: string): Promise<void> {
  fireEvent.pointerDown(screen.getByTestId(menuId), { button: 0 });
  fireEvent.click(await screen.findByTestId(itemId));
  await flush();
}

async function renderSection(daemon: FleetDaemon) {
  const section = listSettingsSections().find((s) => s.id === "hosts");
  if (!section) throw new Error("hosts section did not register");
  const view = renderWithDaemon(<>{section.render({ client: daemon.client, events: daemon.events })}</>, daemon);
  await flush();
  return view;
}

/** Every text node in the tree, for the "no key or secret is ever rendered" assertions. */
function allText(): string {
  return Array.from(document.body.querySelectorAll("*"))
    .map((el) => el.textContent ?? "")
    .join("\n");
}

describe("settings-hosts section", () => {
  it("TestHostsPage_RendersStates", async () => {
    const d = new FleetDaemon();
    d.hosts = [
      fleetHost(),
      fleetHost({ hostId: "h2linux", alias: "linux", label: "Linux box", link: "down" }),
      fleetHost({
        hostId: "h3win",
        alias: "win",
        label: "Windows",
        link: "refused",
        linkCode: "fleet_version_unsupported",
        linkGuidance: "update smind on win (has 0.16, needs >= 0.17)",
      }),
      fleetHost({ hostId: "h4nas", alias: "nas", label: "NAS", state: "pending", link: "connecting", sas: "123456" }),
      fleetHost({ hostId: "h5box", alias: "box", label: "Box", state: "pending", pendingApproval: true, link: "connecting", sas: "654321" }),
    ];
    await renderSection(d);

    expect(screen.getByTestId("host-link-mac")).toHaveTextContent("Connected");
    expect(screen.getByTestId("host-link-linux")).toHaveTextContent("Unreachable");
    expect(screen.getByTestId("host-refused-win")).toHaveTextContent("update smind on win (has 0.16, needs >= 0.17)");
    expect(screen.getByTestId("host-sas-nas")).toHaveTextContent("Confirm this code on nas");
    expect(screen.getByTestId("host-sas-code-nas")).toHaveTextContent("123 456");
    expect(screen.getByTestId("host-pending-nas")).toHaveTextContent("Waiting for approval on nas");
    expect(screen.getByTestId("host-pending-box")).toHaveTextContent("Awaiting your approval");
    expect(screen.getByTestId("host-review-box")).toBeInTheDocument();
    expect(screen.queryByTestId("host-review-nas")).toBeNull();
    // the wire view carries no key or secret, and neither does the page
    expect(allText()).not.toMatch(/pub|secret/i);
  });

  it("TestHostsPage_FleetOffState", async () => {
    const d = new FleetDaemon();
    d.hosts = [fleetHost()];
    d.off = true;
    await renderSection(d);

    const off = screen.getByTestId("hosts-off");
    expect(off).toHaveTextContent("Fleet is off");
    expect(off).toHaveTextContent("fleet.enabled: true");
    // no rows, so none of the row actions exist in any form
    expect(document.querySelectorAll('[data-testid^="host-row-"]')).toHaveLength(0);
    expect(screen.queryByRole("button", { name: "Revoke" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Remove" })).toBeNull();
    expect(screen.queryByTestId("hosts-list")).toBeNull();
  });

  it("TestHostsPage_ReverseToggleShowsNarrowerGrant", async () => {
    const d = new FleetDaemon();
    d.hosts = [fleetHost()];
    await renderSection(d);

    await click(screen.getByTestId("host-inbound-mac"));
    const dialog = await screen.findByTestId("host-reverse-dialog");
    const copy = within(dialog).getByTestId("host-reverse-copy");
    expect(copy).toHaveTextContent("read only");
    expect(copy).toHaveTextContent("0 live agents");
    expect(d.callsOf("fleet.host.enable_reverse")).toHaveLength(0);

    await click(within(dialog).getByTestId("host-reverse-dialog-confirm"));
    const calls = d.callsOf("fleet.host.enable_reverse");
    expect(calls).toHaveLength(1);
    expect(calls[0]!.params).toEqual({ hostId: "h1abc" });
  });

  it("TestHostsPage_RevokeConfirms", async () => {
    const d = new FleetDaemon();
    d.hosts = [fleetHost()];
    await renderSection(d);

    await openMenuItem("host-menu-mac", "host-revoke-mac");
    let dialog = await screen.findByTestId("host-revoke-dialog");
    expect(d.callsOf("fleet.host.revoke")).toHaveLength(0);
    await click(within(dialog).getByTestId("host-revoke-dialog-cancel"));
    await waitFor(() => expect(screen.queryByTestId("host-revoke-dialog")).toBeNull());
    expect(d.callsOf("fleet.host.revoke")).toHaveLength(0);

    await openMenuItem("host-menu-mac", "host-revoke-mac");
    dialog = await screen.findByTestId("host-revoke-dialog");
    await click(within(dialog).getByTestId("host-revoke-dialog-confirm"));
    const revokes = d.callsOf("fleet.host.revoke");
    expect(revokes).toHaveLength(1);
    expect(revokes[0]!.params).toEqual({ hostId: "h1abc" });

    // the same shape for Remove
    await openMenuItem("host-menu-mac", "host-remove-mac");
    let removeDialog = await screen.findByTestId("host-remove-dialog");
    expect(d.callsOf("fleet.host.rm")).toHaveLength(0);
    await click(within(removeDialog).getByTestId("host-remove-dialog-cancel"));
    await waitFor(() => expect(screen.queryByTestId("host-remove-dialog")).toBeNull());
    expect(d.callsOf("fleet.host.rm")).toHaveLength(0);

    await openMenuItem("host-menu-mac", "host-remove-mac");
    removeDialog = await screen.findByTestId("host-remove-dialog");
    await click(within(removeDialog).getByTestId("host-remove-dialog-confirm"));
    const removals = d.callsOf("fleet.host.rm");
    expect(removals).toHaveLength(1);
    expect(removals[0]!.params).toEqual({ hostId: "h1abc" });
  });

  const FORWARD_GRANT = {
    ops: ["read", "create", "send", "stop"],
    workspacesRead: { all: true },
    workspacesWrite: [7],
    providers: [],
    maxLiveAgents: 2,
    maxWallSeconds: 7200,
    maxCostUsd: 5,
  };

  function pendingBox(grant = FORWARD_GRANT): FleetDaemon {
    const d = new FleetDaemon();
    d.workspaceRows = [{ ID: 7, Title: "api" }];
    d.hosts = [
      fleetHost({
        hostId: "h5box",
        alias: "box",
        label: "Box",
        state: "pending",
        pendingApproval: true,
        link: "connecting",
        sas: "123456",
        inbound: { enabled: false, grant },
      }),
    ];
    return d;
  }

  function type(value: string) {
    fireEvent.change(screen.getByTestId("approve-sas-input"), { target: { value } });
  }

  it("TestHostsPage_SASNeverAutoConfirmed", async () => {
    const d = pendingBox();
    await renderSection(d);

    await click(screen.getByTestId("host-review-box"));
    expect(screen.getByTestId("approve-host-dialog")).toBeInTheDocument();
    expect(screen.getByTestId("approve-sas")).toHaveTextContent("123 456");
    expect(screen.getByTestId("approve-sas-input")).toHaveValue("");

    const approve = screen.getByTestId("approve-confirm");
    expect(approve).toBeDisabled();
    await click(approve);
    expect(d.callsOf("fleet.host.approve")).toHaveLength(0);

    type("123 457");
    await flush();
    expect(approve).toBeDisabled();

    type("123 456");
    await flush();
    expect(approve).toBeEnabled();

    await click(approve);
    const calls = d.callsOf("fleet.host.approve");
    expect(calls).toHaveLength(1);
    expect(calls[0].params).toEqual({ hostId: "h5box", sas: "123456", grant: FORWARD_GRANT });
    await waitFor(() => expect(screen.queryByTestId("approve-host-dialog")).toBeNull());
  });

  it("TestHostsPage_GrantRequiresWriteWorkspace", async () => {
    const d = pendingBox({ ...FORWARD_GRANT, workspacesWrite: [] });
    await renderSection(d);

    await click(screen.getByTestId("host-review-box"));
    type("123456");
    await flush();
    const approve = screen.getByTestId("approve-confirm");
    expect(approve).toBeDisabled();
    expect(screen.getByTestId("grant-write-required")).toBeInTheDocument();

    await click(screen.getByTestId("grant-ws-7"));
    expect(approve).toBeEnabled();
    expect(screen.queryByTestId("grant-write-required")).toBeNull();

    await click(screen.getByTestId("grant-ws-7"));
    expect(approve).toBeDisabled();
    for (const op of ["create", "send", "stop"]) await click(screen.getByTestId(`grant-op-${op}`));
    expect(approve).toBeEnabled();
    expect(screen.queryByTestId("grant-write-required")).toBeNull();
  });

  it("TestHostsPage_InviteShowsFragmentInviteNoPassword", async () => {
    const d = new FleetDaemon();
    await renderSection(d);

    await click(screen.getByTestId("hosts-invite"));
    expect(screen.getByTestId("invite-host-dialog")).toHaveTextContent("no password");
    await click(screen.getByTestId("invite-create"));

    expect(screen.getByTestId("invite-value")).toHaveValue("smind-host://v1#payload");
    expect(d.callsOf("fleet.host.invite")[0].params).toEqual({ window: "10m" });
  });

  it("TestHostsPage_AddShowsSasAndDoesNotApprove", async () => {
    const d = new FleetDaemon();
    await renderSection(d);

    await click(screen.getByTestId("hosts-add"));
    fireEvent.change(screen.getByTestId("add-invite"), { target: { value: "  smind-host://v1#payload  " } });
    fireEvent.change(screen.getByTestId("add-alias"), { target: { value: "Bad Alias" } });
    await flush();
    expect(screen.getByTestId("add-submit")).toBeDisabled();

    fireEvent.change(screen.getByTestId("add-alias"), { target: { value: "office" } });
    await flush();
    await click(screen.getByTestId("add-submit"));

    expect(d.callsOf("fleet.host.add")[0].params).toEqual({ invite: "smind-host://v1#payload", alias: "office" });
    expect(screen.getByTestId("add-sas")).toHaveTextContent("123 456");
    expect(screen.getByTestId("add-host-dialog")).toHaveTextContent("cannot approve");
    expect(d.callsOf("fleet.host.approve")).toHaveLength(0);
  });

  it("TestHostsPage_EditGrantSaves", async () => {
    const d = new FleetDaemon();
    d.hosts = [fleetHost({ inbound: { enabled: true, grant: { ...fleetHost().inbound.grant, maxLiveAgents: 3 } } })];
    await renderSection(d);

    await openMenuItem("host-menu-mac", "host-edit-grant-mac");
    expect(screen.getByTestId("edit-grant-dialog")).toHaveTextContent("Access for mac");
    fireEvent.change(screen.getByTestId("grant-max-agents"), { target: { value: "1" } });
    await flush();
    await click(screen.getByTestId("edit-grant-save"));

    const calls = d.callsOf("fleet.host.grant");
    expect(calls).toHaveLength(1);
    const params = calls[0].params as { hostId: string; grant: { maxLiveAgents: number } };
    expect(params.hostId).toBe("h1abc");
    expect(params.grant.maxLiveAgents).toBe(1);
  });
});
