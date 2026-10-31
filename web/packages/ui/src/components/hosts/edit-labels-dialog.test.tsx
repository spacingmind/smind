import { act } from "react";
import { fireEvent, screen } from "@testing-library/react";
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

async function renderSection(daemon: FleetDaemon) {
  const section = listSettingsSections().find((s) => s.id === "hosts");
  if (!section) throw new Error("hosts section did not register");
  const view = renderWithDaemon(<>{section.render({ client: daemon.client, events: daemon.events })}</>, daemon);
  await flush();
  return view;
}

function type(testId: string, value: string) {
  fireEvent.change(screen.getByTestId(testId), { target: { value } });
}

async function openEditor(alias = "mac") {
  fireEvent.click(screen.getByTestId(`host-edit-labels-${alias}`));
  await flush();
}

describe("host labels editor", () => {
  it("labels_row_shows_chips_and_offers_edit_for_approved_hosts_only", async () => {
    const d = new FleetDaemon();
    d.hosts = [
      fleetHost({ labels: { os: "windows", gpu: "cuda" } }),
      fleetHost({ hostId: "h2", alias: "bare" }),
      fleetHost({ hostId: "h3", alias: "wait", state: "pending", link: "connecting", sas: "123456" }),
      fleetHost({ hostId: "h4", alias: "gone", state: "revoked", link: "revoked" }),
    ];
    await renderSection(d);
    expect(screen.getByTestId("host-labels-mac")).toHaveTextContent("gpu:cuda");
    expect(screen.getByTestId("host-labels-mac")).toHaveTextContent("os:windows");
    expect(screen.getByTestId("host-labels-none-bare")).toBeInTheDocument();
    expect(screen.getByTestId("host-edit-labels-bare")).toBeInTheDocument();
    expect(screen.queryByTestId("host-edit-labels-wait")).toBeNull();
    expect(screen.queryByTestId("host-edit-labels-gone")).toBeNull();
  });

  it("labels_edit_sends_only_the_difference", async () => {
    const d = new FleetDaemon();
    d.hosts = [fleetHost({ labels: { os: "linux", gpu: "cuda", arch: "x64" } })];
    await renderSection(d);
    await openEditor();

    expect(screen.getByTestId("edit-labels-save")).toBeDisabled(); // nothing changed yet
    expect(screen.getByTestId("edit-labels-count")).toHaveTextContent("3 of 16");
    // rows are sorted: arch, gpu, os
    type("edit-labels-value-2", "windows"); // os -> windows
    fireEvent.click(screen.getByTestId("edit-labels-remove-0")); // drop arch
    fireEvent.click(screen.getByTestId("edit-labels-add"));
    type("edit-labels-key-2", "zone");
    type("edit-labels-value-2", "eu");
    await flush();
    expect(screen.getByTestId("edit-labels-save")).toBeEnabled();
    fireEvent.click(screen.getByTestId("edit-labels-save"));
    await flush();

    const calls = d.callsOf("fleet.host.labels");
    expect(calls).toHaveLength(1);
    expect(calls[0].params).toEqual({ hostId: "h1abc", set: { os: "windows", zone: "eu" }, remove: ["arch"] });
    expect(screen.queryByTestId("edit-labels-dialog")).toBeNull();
    expect(screen.getByTestId("host-labels-mac")).toHaveTextContent("os:windows");
    expect(screen.getByTestId("host-labels-mac")).toHaveTextContent("zone:eu");
    expect(screen.getByTestId("host-labels-mac")).not.toHaveTextContent("arch");
  });

  it("labels_invalid_rows_block_save_and_say_why", async () => {
    const d = new FleetDaemon();
    d.hosts = [fleetHost({ labels: { os: "linux" } })];
    await renderSection(d);
    await openEditor();

    fireEvent.click(screen.getByTestId("edit-labels-add"));
    type("edit-labels-key-1", "api-token");
    type("edit-labels-value-1", "x");
    await flush();
    expect(screen.getByTestId("edit-labels-row-error-1")).toHaveTextContent('labels: key "api-token" looks like a secret');
    expect(screen.getByTestId("edit-labels-save")).toBeDisabled();

    type("edit-labels-key-1", "os"); // duplicate of row 0
    await flush();
    expect(screen.getByTestId("edit-labels-row-error-1")).toHaveTextContent("Duplicate key");
    expect(screen.getByTestId("edit-labels-save")).toBeDisabled();

    type("edit-labels-key-1", "zone");
    type("edit-labels-value-1", "Eu");
    await flush();
    expect(screen.getByTestId("edit-labels-row-error-1")).toHaveTextContent('labels: value for key "zone" must match ^[a-z0-9][a-z0-9._+-]{0,31}$');
    expect(screen.getByTestId("edit-labels-save")).toBeDisabled();
    expect(d.callsOf("fleet.host.labels")).toHaveLength(0);
  });

  it("labels_cap_of_16_disables_add", async () => {
    const d = new FleetDaemon();
    const labels = Object.fromEntries(Array.from({ length: 16 }, (_, i) => [`k${String(i).padStart(2, "0")}`, "v"]));
    d.hosts = [fleetHost({ labels })];
    await renderSection(d);
    await openEditor();
    expect(screen.getByTestId("edit-labels-count")).toHaveTextContent("16 of 16");
    expect(screen.getByTestId("edit-labels-add")).toBeDisabled();
    fireEvent.click(screen.getByTestId("edit-labels-remove-0"));
    await flush();
    expect(screen.getByTestId("edit-labels-add")).toBeEnabled();
  });

  it("labels_daemon_refusal_is_shown_and_keeps_the_dialog", async () => {
    const d = new FleetDaemon();
    d.hosts = [fleetHost()];
    d.labelsError = "fleet.host.labels: labels: too many labels (max 16)";
    await renderSection(d);
    await openEditor();
    fireEvent.click(screen.getByTestId("edit-labels-add"));
    type("edit-labels-key-0", "os");
    type("edit-labels-value-0", "linux");
    await flush();
    fireEvent.click(screen.getByTestId("edit-labels-save"));
    await flush();
    expect(screen.getByTestId("edit-labels-error")).toHaveTextContent("too many labels");
    expect(screen.getByTestId("edit-labels-dialog")).toBeInTheDocument();
  });

  it("labels_edits_are_dropped_on_close", async () => {
    const d = new FleetDaemon();
    d.hosts = [fleetHost({ labels: { os: "linux" } })];
    await renderSection(d);
    await openEditor();
    type("edit-labels-value-0", "windows");
    fireEvent.keyDown(screen.getByTestId("edit-labels-dialog"), { key: "Escape" });
    await flush();
    expect(screen.queryByTestId("edit-labels-dialog")).toBeNull();
    await openEditor();
    expect((screen.getByTestId("edit-labels-value-0") as HTMLInputElement).value).toBe("linux");
    expect(d.callsOf("fleet.host.labels")).toHaveLength(0);
  });

  it("labels_reopen_on_another_host_starts_from_its_labels", async () => {
    const d = new FleetDaemon();
    d.hosts = [fleetHost({ labels: { os: "linux" } }), fleetHost({ hostId: "h2", alias: "bare" })];
    await renderSection(d);
    await openEditor("mac");
    fireEvent.keyDown(screen.getByTestId("edit-labels-dialog"), { key: "Escape" });
    await flush();
    expect(screen.queryByTestId("edit-labels-dialog")).toBeNull();
    await openEditor("bare");
    expect(screen.getByTestId("edit-labels-empty")).toBeInTheDocument();
    expect(screen.queryByTestId("edit-labels-key-0")).toBeNull();
  });
});
