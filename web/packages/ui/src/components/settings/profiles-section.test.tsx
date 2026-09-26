import { act } from "react";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { listSettingsSections } from "@/components/settings/settings-registry";
import "@/components/settings/profiles-section";
import type { DaemonEvents } from "@/hooks/use-daemon-events";
import { FakeWsClient } from "@/test/fake-ws-client";

const PROFILE = {
  ID: 1,
  Name: "UI work",
  Provider: "claude-native",
  ApprovalPolicy: "manual",
  ThinkingLevel: "standard",
  Notes: "For UI polish.",
  CreatedAt: "2026-09-25T00:00:00Z",
  UpdatedAt: "2026-09-25T00:00:00Z",
};

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

/** A minimal DaemonEvents fake: records listeners per topic and lets a test fire one directly, mirroring useDaemonEvents' real subscribe/dispatch shape without a real connection. */
function fakeDaemonEvents(): DaemonEvents & { fire(topic: string, payload: unknown): void } {
  const listeners = new Map<string, Set<(payload: unknown) => void>>();
  return {
    subscribe(topic, listener) {
      let set = listeners.get(topic);
      if (!set) {
        set = new Set();
        listeners.set(topic, set);
      }
      set.add(listener);
      return () => set!.delete(listener);
    },
    fire(topic, payload) {
      for (const fn of listeners.get(topic) ?? []) fn(payload);
    },
  };
}

function renderSection(client: FakeWsClient, events?: DaemonEvents) {
  const section = listSettingsSections().find((s) => s.id === "agents");
  if (!section) throw new Error("profiles section did not register");
  render(<>{section.render({ client: client as never, events })}</>);
}

describe("profiles-section", () => {
  it("renders an empty state with zero profiles", async () => {
    const client = new FakeWsClient();
    renderSection(client);
    client.nth("profile.list").resolve([]);
    await flush();

    expect(screen.getByTestId("profiles-empty-state")).toBeInTheDocument();
  });

  it("lists profiles from profile.list", async () => {
    const client = new FakeWsClient();
    renderSection(client);
    client.nth("profile.list").resolve([PROFILE]);
    await flush();

    expect(screen.getByTestId("profile-row-1")).toBeInTheDocument();
    expect(screen.getByText("UI work")).toBeInTheDocument();
  });

  it("the header's + New agent button opens the add card at the top of the list, and creating a profile through it appears below", async () => {
    const client = new FakeWsClient();
    renderSection(client);
    client.nth("profile.list").resolve([PROFILE]);
    await flush();

    expect(screen.queryByTestId("profile-form-name")).not.toBeInTheDocument();
    fireEvent.click(screen.getByTestId("profile-new-button"));
    await flush();

    // The add card renders as the list's own first item, above the
    // existing profile rows -- not a separate section below them.
    const list = screen.getByRole("list");
    const rows = within(list).getAllByRole("listitem");
    expect(rows[0]).toHaveAttribute("data-testid", "profile-new-form");
    expect(within(rows[0]).getByTestId("profile-form-name")).toBeInTheDocument();

    fireEvent.change(screen.getByTestId("profile-form-name"), { target: { value: "New profile" } });
    fireEvent.click(screen.getByTestId("profile-form-submit"));
    await flush();

    expect(client.nth("profile.create").params).toMatchObject({ name: "New profile", provider: "claude-native" });

    client.nth("profile.create").resolve({ ...PROFILE, ID: 2, Name: "New profile" });
    await flush();

    expect(within(screen.getByTestId("profile-row-2")).getByText("New profile")).toBeInTheDocument();
    // The card closes on save.
    expect(screen.queryByTestId("profile-new-form")).not.toBeInTheDocument();
  });

  it("Cancel closes the add card without creating anything, and opening Edit on a row closes an open add card", async () => {
    const client = new FakeWsClient();
    renderSection(client);
    client.nth("profile.list").resolve([PROFILE]);
    await flush();

    fireEvent.click(screen.getByTestId("profile-new-button"));
    await flush();
    fireEvent.click(screen.getByTestId("profile-form-cancel"));
    expect(screen.queryByTestId("profile-new-form")).not.toBeInTheDocument();
    expect(client.calls.some((c) => c.method === "profile.create")).toBe(false);

    // Only one card is ever open -- opening a row's Edit while the add
    // card is open closes the add card.
    fireEvent.click(screen.getByTestId("profile-new-button"));
    await flush();
    expect(screen.getByTestId("profile-new-form")).toBeInTheDocument();

    fireEvent.pointerDown(screen.getByTestId("profile-menu-1"), { button: 0 });
    fireEvent.click(await screen.findByTestId("profile-edit-1"));
    await flush();
    expect(screen.queryByTestId("profile-new-form")).not.toBeInTheDocument();
    expect(within(screen.getByTestId("profile-row-1")).getByTestId("profile-edit-form-1-name")).toBeInTheDocument();
  });

  it("empty state shows a sentence and its own + New agent button", async () => {
    const client = new FakeWsClient();
    renderSection(client);
    client.nth("profile.list").resolve([]);
    await flush();

    expect(screen.getByTestId("profiles-empty-state")).toBeInTheDocument();
    fireEvent.click(screen.getByTestId("profiles-empty-new-button"));
    await flush();

    expect(screen.getByTestId("profile-new-form")).toBeInTheDocument();
    expect(screen.queryByTestId("profiles-empty-state")).not.toBeInTheDocument();
  });

  // run-config IA: Edit/Delete moved behind a "⋯" menu (profile-menu-{id}),
  // and Edit now expands inline in the row (profile-edit-form-{id}-*)
  // rather than repurposing the bottom "New agent" form.
  it("editing a profile opens inline in the row and persists changes", async () => {
    const client = new FakeWsClient();
    renderSection(client);
    client.nth("profile.list").resolve([PROFILE]);
    await flush();

    fireEvent.pointerDown(screen.getByTestId("profile-menu-1"), { button: 0 });
    fireEvent.click(await screen.findByTestId("profile-edit-1"));
    await flush();

    // Inline, inside the row -- not the header's "+ New agent" card, which
    // isn't open at all right now.
    expect(within(screen.getByTestId("profile-row-1")).getByTestId("profile-edit-form-1-name")).toHaveValue("UI work");
    expect(screen.queryByTestId("profile-new-form")).not.toBeInTheDocument();

    fireEvent.change(screen.getByTestId("profile-edit-form-1-name"), { target: { value: "Renamed" } });
    fireEvent.click(screen.getByTestId("profile-edit-form-1-submit"));
    await flush();

    expect(client.nth("profile.update").params).toMatchObject({ id: 1, name: "Renamed" });

    client.nth("profile.update").resolve({ ...PROFILE, Name: "Renamed" });
    await flush();

    expect(screen.getByText("Renamed")).toBeInTheDocument();
    expect(screen.queryByText("UI work")).not.toBeInTheDocument();
    // Save collapses the inline form back to the summary row.
    expect(screen.queryByTestId("profile-edit-form-1-name")).not.toBeInTheDocument();
  });

  it("deleting a profile from the ⋯ menu calls profile.delete with its id", async () => {
    const client = new FakeWsClient();
    renderSection(client);
    client.nth("profile.list").resolve([PROFILE]);
    await flush();

    fireEvent.pointerDown(screen.getByTestId("profile-menu-1"), { button: 0 });
    fireEvent.click(await screen.findByTestId("profile-delete-1"));
    await flush();

    expect(client.nth("profile.delete").params).toEqual({ id: 1 });

    client.nth("profile.delete").resolve({});
    await flush();

    expect(screen.queryByTestId("profile-row-1")).not.toBeInTheDocument();
  });

  it("reflects a profile.created event from outside its own mutation, without a manual refresh", async () => {
    const client = new FakeWsClient();
    const events = fakeDaemonEvents();
    renderSection(client, events);
    client.nth("profile.list").resolve([]);
    await flush();

    expect(screen.getByTestId("profiles-empty-state")).toBeInTheDocument();

    act(() => {
      events.fire("profile.created", { profile: PROFILE });
    });
    await flush();

    expect(screen.getByTestId("profile-row-1")).toBeInTheDocument();
    expect(screen.queryByTestId("profiles-empty-state")).not.toBeInTheDocument();
  });

  it("reflects a profile.deleted event by removing the row", async () => {
    const client = new FakeWsClient();
    const events = fakeDaemonEvents();
    renderSection(client, events);
    client.nth("profile.list").resolve([PROFILE]);
    await flush();
    expect(screen.getByTestId("profile-row-1")).toBeInTheDocument();

    act(() => {
      events.fire("profile.deleted", { id: 1 });
    });
    await flush();

    expect(screen.queryByTestId("profile-row-1")).not.toBeInTheDocument();
  });
});

describe("profiles-section ★ default agent (run-config IA)", () => {
  it("marks a profile default, and clicking its star again clears it", async () => {
    const client = new FakeWsClient();
    renderSection(client);
    client.nth("profile.list").resolve([PROFILE]);
    await flush();

    const star = screen.getByTestId("profile-default-1");
    expect(star).toHaveAttribute("aria-pressed", "false");

    fireEvent.click(star);
    expect(screen.getByTestId("profile-default-1")).toHaveAttribute("aria-pressed", "true");
    expect(window.localStorage.getItem("smind:settings:defaultAgentId")).toBe("1");

    fireEvent.click(screen.getByTestId("profile-default-1"));
    expect(screen.getByTestId("profile-default-1")).toHaveAttribute("aria-pressed", "false");
    expect(window.localStorage.getItem("smind:settings:defaultAgentId")).toBeNull();
  });

  it("clears the default when its agent is deleted", async () => {
    const client = new FakeWsClient();
    renderSection(client);
    client.nth("profile.list").resolve([PROFILE]);
    await flush();

    fireEvent.click(screen.getByTestId("profile-default-1"));
    expect(window.localStorage.getItem("smind:settings:defaultAgentId")).toBe("1");

    fireEvent.pointerDown(screen.getByTestId("profile-menu-1"), { button: 0 });
    fireEvent.click(await screen.findByTestId("profile-delete-1"));
    await flush();
    client.nth("profile.delete").resolve({});
    await flush();

    expect(window.localStorage.getItem("smind:settings:defaultAgentId")).toBeNull();
  });
});

describe("profiles-section inline edit's provider health dot (run-config IA)", () => {
  it("tests the profile's provider and renders ok/failed from provider.test", async () => {
    const client = new FakeWsClient();
    renderSection(client);
    client.nth("profile.list").resolve([PROFILE]);
    client.nth("provider.list").resolve({
      providers: [{ id: "claude-native", label: "Claude Code", credentialKind: "oauth", accountProvider: "anthropic" }],
    });
    await flush();

    fireEvent.pointerDown(screen.getByTestId("profile-menu-1"), { button: 0 });
    fireEvent.click(await screen.findByTestId("profile-edit-1"));
    await flush();

    // claude-native is credential-backed -- provider.test is called with
    // the accounts-vocabulary id (accountProvider), not the raw taskrunner
    // provider id, per ProviderInfo.accountProvider's own doc comment.
    const testCall = client.nth("provider.test", 0);
    expect(testCall.params).toEqual({ provider: "anthropic" });

    await act(async () => {
      testCall.resolve({ ok: true, detail: "using saved credential" });
    });
    expect(screen.getByTestId("profile-edit-form-1-provider-health")).toHaveAttribute("data-status", "ok");
  });
});
