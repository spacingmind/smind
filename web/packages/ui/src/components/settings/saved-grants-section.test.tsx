import { act } from "react";
import { fireEvent, screen, within } from "@testing-library/react";
import { renderWithClientProp as render } from "@/test/render-with-daemon";
import { describe, expect, it } from "vitest";

import { listSettingsSections } from "@/components/settings/settings-registry";
import "@/components/settings/saved-grants-section";
import { FakeWsClient } from "@/test/fake-ws-client";
import type { SavedGrant } from "@/lib/types";

function grant(over: Partial<SavedGrant> = {}): SavedGrant {
  return {
    id: 1,
    workspaceId: 2,
    command: "git status",
    folder: "/repo/smind",
    class: "readonly",
    runnerOptIn: false,
    createdAt: new Date(Date.now() - 2 * 86_400_000).toISOString(),
    expiresAt: new Date(Date.now() + 28 * 86_400_000).toISOString(),
    createdBy: "local",
    useCount: 3,
    lastUsedAt: new Date(Date.now() - 3_600_000).toISOString(),
    status: "active",
    ...over,
  };
}

async function flush() {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

function renderSection() {
  const client = new FakeWsClient();
  const section = listSettingsSections().find((s) => s.id === "saved-grants");
  if (!section) throw new Error("saved-grants did not register");
  render(<>{section.render({ client: client as never, events: null })}</>);
  return client;
}

describe("saved-grants-section", () => {
  it("registers under Safety & usage", () => {
    const s = listSettingsSections().find((x) => x.id === "saved-grants");
    expect(s?.label).toBe("Saved grants");
    expect(s?.groupLabel).toBe("Safety & usage");
  });

  it("says so when nothing is saved", async () => {
    const client = renderSection();
    client.nth("permission.grants.list").resolve({ grants: [] });
    await flush();
    expect(screen.getByTestId("grants-empty")).toHaveTextContent("Nothing saved");
  });

  it("lists the exact command, class, status, expiry and uses of every grant, as text", async () => {
    const client = renderSection();
    client.nth("permission.grants.list").resolve({
      grants: [
        grant(),
        grant({ id: 2, command: "make build", class: "runner", runnerOptIn: true }),
        grant({ id: 3, command: "make test", class: "runner", runnerOptIn: false, status: "void" }),
        grant({ id: 4, command: "ls -la", status: "expired", expiresAt: new Date(Date.now() - 86_400_000).toISOString() }),
        grant({ id: 5, command: "cat <b>x</b>", status: "revoked", revokedAt: new Date().toISOString() }),
      ],
    });
    await flush();
    expect(screen.getByTestId("grant-1-status")).toHaveTextContent("Active");
    expect(screen.getByTestId("grant-1-class")).toHaveTextContent("Read-only");
    expect(screen.getByTestId("grant-2-class")).toHaveTextContent("Runs project code");
    expect(screen.getByTestId("grant-2")).toHaveTextContent("You allowed this to run the project's code");
    expect(screen.getByTestId("grant-3-status")).toHaveTextContent("Void: tool setup changed");
    expect(screen.getByTestId("grant-3")).toHaveTextContent("No project-code opt-in: this is never applied.");
    expect(screen.getByTestId("grant-4-status")).toHaveTextContent("Expired");
    expect(screen.getByTestId("grant-4")).toHaveTextContent("this command asks again");
    expect(screen.getByTestId("grant-5-command")).toHaveTextContent("cat <b>x</b>");
    expect(screen.getByTestId("grant-5").querySelector("b")).toBeNull();
    expect(screen.getByTestId("grant-1")).toHaveTextContent("Used 3 times");
    expect(screen.getByTestId("grant-1")).toHaveTextContent("Project folder: /repo/smind");
    // a revoked grant has no Revoke; the others do
    expect(screen.queryByTestId("grant-5-revoke")).toBeNull();
    expect(screen.getByTestId("grant-1-revoke")).toBeInTheDocument();
  });

  it.each([
    ["void", "make test", "Void: tool setup changed"],
    ["expired", "ls -la", "Expired"],
    ["revoked", "git log", "Revoked"],
  ])("a %s grant shows only its reason, never an expiry countdown that suggests it still works", async (status, command, label) => {
    const client = renderSection();
    client.nth("permission.grants.list").resolve({ grants: [grant({ id: 9, command, status })] });
    await flush();
    const row = screen.getByTestId("grant-9");
    expect(within(row).getByTestId("grant-9-status")).toHaveTextContent(label);
    expect(within(row).queryByTestId("grant-9-expiry")).toBeNull();
    expect(row).not.toHaveTextContent(/Expires in|Expired \d|Expired just now/);
    expect(row).toHaveTextContent("asks again");
  });

  it("an active grant still shows its countdown", async () => {
    const client = renderSection();
    client.nth("permission.grants.list").resolve({ grants: [grant({ id: 9 })] });
    await flush();
    expect(screen.getByTestId("grant-9-expiry")).toHaveTextContent(/^Expires in 2\dd$/);
  });

  it("revokes after a confirmation that says what will ask again, then refreshes", async () => {
    const client = renderSection();
    client.nth("permission.grants.list").resolve({ grants: [grant({ id: 7, command: "make build", class: "runner", runnerOptIn: true })] });
    await flush();
    fireEvent.click(screen.getByTestId("grant-7-revoke"));
    expect(await screen.findByTestId("grant-revoke-copy")).toHaveTextContent("make build will ask again in this project.");
    fireEvent.click(screen.getByTestId("grant-revoke-confirm"));
    await flush();
    const call = client.nth("permission.grants.revoke");
    expect(call.params).toEqual({ grantId: 7 });
    call.resolve({ revoked: true });
    await flush();
    expect(client.calls.filter((c) => c.method === "permission.grants.list").length).toBeGreaterThanOrEqual(2);
  });

  it("shows the daemon's refusal in the dialog and keeps the grant", async () => {
    const client = renderSection();
    client.nth("permission.grants.list").resolve({ grants: [grant({ id: 8 })] });
    await flush();
    fireEvent.click(screen.getByTestId("grant-8-revoke"));
    fireEvent.click(await screen.findByTestId("grant-revoke-confirm"));
    await flush();
    client.nth("permission.grants.revoke").reject(new Error("permission grant not found"));
    await flush();
    expect(within(screen.getByRole("dialog")).getByText(/permission grant not found/)).toBeInTheDocument();
    expect(screen.getByTestId("grant-8")).toBeInTheDocument();
  });

  it("an error loading is shown, not an empty list", async () => {
    const client = renderSection();
    client.nth("permission.grants.list").reject(new Error("boom"));
    await flush();
    expect(screen.getByTestId("grants-error")).toBeInTheDocument();
    expect(screen.queryByTestId("grants-empty")).toBeNull();
  });

  it("ui_cleanup_b2_saved_grants_one_group_row_per_grant", async () => {
    const section = listSettingsSections().find((s) => s.id === "saved-grants");
    if (!section) throw new Error("saved-grants did not register");
    const client = new FakeWsClient();
    const mounted = render(<>{section.render({ client: client as never, events: null })}</>);
    client.nth("permission.grants.list").resolve({
      grants: [grant(), grant({ id: 2, command: "make build" }), grant({ id: 3, command: "make test" })],
    });
    await flush();

    // One group card, one row per grant -- not one card per grant.
    const groups = document.querySelectorAll("[data-slot='settings-group']");
    expect(groups).toHaveLength(1);
    expect(groups[0]!.querySelectorAll("[data-slot='settings-row']")).toHaveLength(3);
    for (const id of [1, 2, 3]) {
      expect(within(screen.getByTestId(`grant-${id}`)).getByTestId(`grant-${id}-revoke`)).toBeInTheDocument();
    }
    // No grant hand-rolls its own rounded box outside the group's card.
    expect(
      Array.from(document.querySelectorAll(".rounded-xl")).filter((el) => !el.closest("[data-slot='settings-group']")),
    ).toHaveLength(0);

    mounted.unmount();

    const client2 = new FakeWsClient();
    render(<>{section.render({ client: client2 as never, events: null })}</>);
    client2.nth("permission.grants.list").resolve({ grants: [] });
    await flush();

    expect(document.querySelector("[data-slot='settings-empty-card']")).not.toBeNull();
    expect(screen.getByTestId("grants-empty")).toHaveTextContent("Nothing saved");
  });
});
