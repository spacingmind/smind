import { act } from "react";
import { fireEvent, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { listSettingsSections } from "@/components/settings/settings-registry";
import "@/components/settings/profiles-section";
import { setSettingsIntent, takeSettingsIntent } from "@/lib/settings-intent";
import { useRunProviderTest } from "@/hooks/use-accounts";
import { renderHookWithDaemon, renderWithDaemon } from "@/test/render-with-daemon";
import { FakeUsageDaemon } from "@/test/usage-test-client";

const PROFILE = {
  ID: 1,
  Name: "UI work",
  Provider: "claude-native",
  PermissionMode: "plan",
  AutoAccept: false,
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

function renderSection(daemon: FakeUsageDaemon) {
  const section = listSettingsSections().find((s) => s.id === "agents");
  if (!section) throw new Error("profiles section did not register");
  return renderWithDaemon(<>{section.render({ client: daemon.client, events: daemon.events })}</>, daemon);
}

/** A daemon with the profiles (and optionally providers) preloaded and answered, for tests that don't script them by hand. */
async function renderWithProfiles(profiles: unknown[], providers: { providers: unknown[] } | null = null) {
  const daemon = new FakeUsageDaemon([]);
  daemon.profiles = profiles as never;
  if (providers) daemon.providers = providers as never;
  const view = renderSection(daemon);
  await flush();
  return { daemon, view };
}

describe("profiles-section", () => {
  afterEach(() => {
    takeSettingsIntent();
  });

  it("a pending new-profile intent opens the add card prefilled", async () => {
    setSettingsIntent({
      kind: "newProfile",
      seed: { name: "Sonnet 5", provider: "claude-native", model: "sonnet-5", permissionMode: "plan", thinkingLevel: "extended" },
    });
    await renderWithProfiles([PROFILE]);

    expect(screen.getByTestId("profile-new-form")).toBeInTheDocument();
    expect(screen.getByTestId("profile-form-name")).toHaveValue("Sonnet 5");
    // The shared provider+model picker (field variant) names the provider and the seeded model.
    expect(screen.getByTestId("profile-form-provider")).toHaveAttribute("title", expect.stringMatching(/claude.*sonnet-5/i));
    expect(takeSettingsIntent()).toBeNull();
  });

  it("a pending edit intent opens that profile's inline editor", async () => {
    setSettingsIntent({ kind: "editProfile", id: 1 });
    await renderWithProfiles([PROFILE]);

    expect(screen.getByTestId("profile-edit-form-1-name")).toHaveValue("UI work");
    expect(screen.queryByTestId("profile-new-form")).not.toBeInTheDocument();
  });

  it("an intent arriving while the section is open is applied", async () => {
    await renderWithProfiles([PROFILE]);
    expect(screen.queryByTestId("profile-edit-form-1-name")).not.toBeInTheDocument();

    act(() => setSettingsIntent({ kind: "editProfile", id: 1 }));
    await flush();
    expect(screen.getByTestId("profile-edit-form-1-name")).toBeInTheDocument();
  });

  it("renders an empty state with zero profiles", async () => {
    await renderWithProfiles([]);

    expect(screen.getByTestId("profiles-empty-state")).toBeInTheDocument();
  });

  it("lists profiles from profile.list", async () => {
    await renderWithProfiles([PROFILE]);

    expect(screen.getByTestId("profile-row-1")).toBeInTheDocument();
    expect(screen.getByText("UI work")).toBeInTheDocument();
  });

  it("the header's + New agent button opens the add card at the top of the list, and creating a profile through it appears below", async () => {
    const { daemon } = await renderWithProfiles([PROFILE]);

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

    expect(daemon.calls.find((c) => c.method === "profile.create")?.params).toMatchObject({ name: "New profile", provider: "claude-native" });

    daemon.emit("profile.created", { profile: { ...PROFILE, ID: 2, Name: "New profile" } });
    await flush();

    expect(within(screen.getByTestId("profile-row-2")).getByText("New profile")).toBeInTheDocument();
    // The card closes on save.
    expect(screen.queryByTestId("profile-new-form")).not.toBeInTheDocument();
  });

  it("the form's permission-mode field lists the provider's own modes, saves permissionMode, and the card names the mode (W4)", async () => {
    const { daemon } = await renderWithProfiles([PROFILE]);

    // The stored profile's card line uses the provider's own mode label.
    expect(screen.getByTestId("profile-row-1")).toHaveTextContent("Plan Mode");

    fireEvent.click(screen.getByTestId("profile-new-button"));
    await flush();
    fireEvent.change(screen.getByTestId("profile-form-name"), { target: { value: "Bypass bot" } });
    fireEvent.click(screen.getByTestId("profile-form-permission-mode"));
    const options = await screen.findAllByRole("option");
    expect(options.map((o) => o.textContent)).toEqual(["Provider default", "Accept File Edits", "Always Ask", "Plan Mode", "Bypass"]);
    fireEvent.click(options[4]);
    await flush();
    // Claude takes no auto-accept.
    expect(screen.queryByTestId("profile-form-auto-accept")).not.toBeInTheDocument();
    fireEvent.click(screen.getByTestId("profile-form-submit"));
    await flush();

    expect(daemon.calls.find((c) => c.method === "profile.create")?.params).toMatchObject({
      name: "Bypass bot",
      provider: "claude-native",
      permissionMode: "bypassPermissions",
      autoAccept: false,
    });
  });

  it("Cancel closes the add card without creating anything, and opening Edit on a row closes an open add card", async () => {
    const { daemon } = await renderWithProfiles([PROFILE]);

    fireEvent.click(screen.getByTestId("profile-new-button"));
    await flush();
    fireEvent.click(screen.getByTestId("profile-form-cancel"));
    expect(screen.queryByTestId("profile-new-form")).not.toBeInTheDocument();
    expect(daemon.calls.some((c) => c.method === "profile.create")).toBe(false);

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

  it("keeps its own title when rendered outside the settings screen", async () => {
    await renderWithProfiles([]);

    expect(screen.getByRole("heading", { level: 3, name: "Agents" })).toBeInTheDocument();
  });

  it("empty state shows a sentence and the one + New agent button (no second CTA in the body)", async () => {
    await renderWithProfiles([]);

    expect(screen.getByTestId("profiles-empty-state")).toBeInTheDocument();
    expect(screen.queryByTestId("profiles-empty-new-button")).not.toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: /New agent/ })).toHaveLength(1);
    fireEvent.click(screen.getByTestId("profile-new-button"));
    await flush();

    expect(screen.getByTestId("profile-new-form")).toBeInTheDocument();
    expect(screen.queryByTestId("profiles-empty-state")).not.toBeInTheDocument();
  });

  // run-config IA: Edit/Delete moved behind a "⋯" menu (profile-menu-{id}),
  // and Edit now expands inline in the row (profile-edit-form-{id}-*)
  // rather than repurposing the bottom "New agent" form.
  it("editing a profile opens inline in the row and persists changes", async () => {
    const { daemon } = await renderWithProfiles([PROFILE]);

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

    expect(daemon.calls.find((c) => c.method === "profile.update")?.params).toMatchObject({ id: 1, name: "Renamed" });

    daemon.emit("profile.updated", { profile: { ...PROFILE, Name: "Renamed" } });
    await flush();

    expect(screen.getByText("Renamed")).toBeInTheDocument();
    expect(screen.queryByText("UI work")).not.toBeInTheDocument();
    // Save collapses the inline form back to the summary row.
    expect(screen.queryByTestId("profile-edit-form-1-name")).not.toBeInTheDocument();
  });

  it("deleting a profile from the ⋯ menu calls profile.delete with its id", async () => {
    const { daemon } = await renderWithProfiles([PROFILE]);

    fireEvent.pointerDown(screen.getByTestId("profile-menu-1"), { button: 0 });
    fireEvent.click(await screen.findByTestId("profile-delete-1"));
    await flush();

    // The menu item only opens the confirmation -- nothing is sent until it is confirmed.
    expect(daemon.calls.some((c) => c.method === "profile.delete")).toBe(false);
    expect(screen.getByTestId("profile-delete-dialog")).toHaveTextContent("UI work");
    fireEvent.click(screen.getByTestId("profile-delete-confirm-1"));
    await flush();

    expect(daemon.calls.find((c) => c.method === "profile.delete")?.params).toEqual({ id: 1 });

    daemon.emit("profile.deleted", { id: 1 });
    await flush();

    expect(screen.queryByTestId("profile-row-1")).not.toBeInTheDocument();
    expect(screen.queryByTestId("profile-delete-dialog")).not.toBeInTheDocument();
  });

  it("cancelling the delete confirmation keeps the profile and sends nothing", async () => {
    const { daemon } = await renderWithProfiles([PROFILE]);
    fireEvent.pointerDown(screen.getByTestId("profile-menu-1"), { button: 0 });
    fireEvent.click(await screen.findByTestId("profile-delete-1"));
    await flush();
    fireEvent.click(screen.getByTestId("profile-delete-dialog-cancel"));
    await flush();

    expect(screen.queryByTestId("profile-delete-dialog")).not.toBeInTheDocument();
    expect(screen.getByTestId("profile-row-1")).toBeInTheDocument();
    expect(daemon.calls.some((c) => c.method === "profile.delete")).toBe(false);
  });

  it("a failed delete stays open with the daemon's message visible (it used to vanish into console.error)", async () => {
    const { daemon } = await renderWithProfiles([PROFILE]);
    fireEvent.pointerDown(screen.getByTestId("profile-menu-1"), { button: 0 });
    fireEvent.click(await screen.findByTestId("profile-delete-1"));
    await flush();
    daemon.failNonUsageWith = new Error("profile 1 is in use by a running task");
    fireEvent.click(screen.getByTestId("profile-delete-confirm-1"));
    await flush();

    expect(screen.getByTestId("profile-delete-dialog-error")).toHaveTextContent("profile 1 is in use by a running task");
    expect(screen.getByTestId("profile-row-1")).toBeInTheDocument();
    // And it can be retried once the daemon allows it.
    daemon.failNonUsageWith = null;
    fireEvent.click(screen.getByTestId("profile-delete-confirm-1"));
    await flush();
    expect(daemon.calls.filter((c) => c.method === "profile.delete")).toHaveLength(2);
    // The retry succeeded: the dialog closes and the row goes once the daemon's event arrives.
    daemon.emit("profile.deleted", { id: 1 });
    await flush();
    expect(screen.queryByTestId("profile-delete-dialog")).not.toBeInTheDocument();
    expect(screen.queryByTestId("profile-row-1")).not.toBeInTheDocument();
  });

  it("reflects a profile.created event from outside its own mutation, without a manual refresh", async () => {
    const { daemon } = await renderWithProfiles([]);

    expect(screen.getByTestId("profiles-empty-state")).toBeInTheDocument();

    act(() => {
      daemon.emit("profile.created", { profile: PROFILE });
    });
    await flush();

    expect(screen.getByTestId("profile-row-1")).toBeInTheDocument();
    expect(screen.queryByTestId("profiles-empty-state")).not.toBeInTheDocument();
  });

  it("reflects a profile.deleted event by removing the row", async () => {
    const { daemon } = await renderWithProfiles([PROFILE]);
    expect(screen.getByTestId("profile-row-1")).toBeInTheDocument();

    act(() => {
      daemon.emit("profile.deleted", { id: 1 });
    });
    await flush();

    expect(screen.queryByTestId("profile-row-1")).not.toBeInTheDocument();
  });
});

describe("profiles-section model field (ADR-0025)", () => {
  const PROVIDERS = {
    providers: [
      {
        id: "claude-native",
        label: "Claude Code",
        models: [
          { id: "sonnet", label: "Sonnet" },
          { id: "opus", label: "Opus" },
        ],
      },
      { id: "glm", label: "GLM" },
    ],
  };

  it("profile editor saves model", async () => {
    const { daemon } = await renderWithProfiles([PROFILE], PROVIDERS);

    fireEvent.click(screen.getByTestId("profile-new-button"));
    await flush();
    fireEvent.change(screen.getByTestId("profile-form-name"), { target: { value: "Cheap" } });
    // The shared provider+model picker (field variant): opens on the current provider's models.
    fireEvent.click(screen.getByTestId("profile-form-provider"));
    const rows = (await screen.findAllByRole("option")).map((o) => o.textContent);
    expect(rows).toEqual(expect.arrayContaining(["Default model", "Sonnet", "Opus"].map((t) => expect.stringContaining(t))));
    fireEvent.click(screen.getByTestId("model-row-claude-native:sonnet"));
    await flush();
    expect(screen.getByTestId("profile-form-provider")).toHaveTextContent("Sonnet");
    fireEvent.click(screen.getByTestId("profile-form-submit"));
    await flush();

    expect(daemon.calls.find((c) => c.method === "profile.create")?.params).toMatchObject({ name: "Cheap", provider: "claude-native", model: "sonnet" });
  });

  it("choosing another provider in the picker resets the model, mode and auto-accept", async () => {
    const providers = {
      providers: [
        { id: "claude-native", label: "Claude Code", credentialKind: "oauth", accountProvider: "anthropic", models: [{ id: "opus", label: "Opus" }] },
        {
          id: "glm",
          label: "GLM",
          kind: "cli",
          supportsAutoAccept: true,
          defaultMode: "default",
          modes: [
            { id: "default", label: "Ask" },
            { id: "bypass", label: "Auto", autoApproves: true },
          ],
          models: [{ id: "glm-5.3", label: "GLM 5.3" }],
        },
      ],
    };
    const { daemon } = await renderWithProfiles([PROFILE], providers);

    fireEvent.click(screen.getByTestId("profile-new-button"));
    await flush();
    fireEvent.change(screen.getByTestId("profile-form-name"), { target: { value: "Other" } });
    // Pick GLM, its model, a non-default mode and auto-accept...
    fireEvent.click(screen.getByTestId("profile-form-provider"));
    fireEvent.click(await screen.findByTestId("model-picker-back"));
    fireEvent.click(screen.getByTestId("model-provider-glm"));
    fireEvent.click(screen.getByTestId("model-row-glm:glm-5.3"));
    await flush();
    fireEvent.click(screen.getByTestId("profile-form-permission-mode"));
    fireEvent.click(await screen.findByRole("option", { name: "Auto" }));
    await flush();
    fireEvent.click(screen.getByTestId("profile-form-auto-accept"));
    expect(screen.getByTestId("profile-form-auto-accept")).toBeChecked();

    // ...then switch back to Claude: the provider-scoped fields do not carry over.
    // (A just-closed Radix Select returns focus late, which would dismiss a popover opened in the same tick.)
    await act(async () => {
      await new Promise((r) => setTimeout(r, 50));
    });
    fireEvent.click(screen.getByTestId("profile-form-provider"));
    fireEvent.click(await screen.findByTestId("model-picker-back"));
    fireEvent.click(screen.getByTestId("model-provider-claude-native"));
    fireEvent.click(screen.getByTestId("model-row-claude-native:"));
    await flush();
    expect(screen.queryByTestId("profile-form-auto-accept")).not.toBeInTheDocument();
    fireEvent.click(screen.getByTestId("profile-form-submit"));
    await flush();

    expect(daemon.calls.find((c) => c.method === "profile.create")?.params).toMatchObject({
      name: "Other",
      provider: "claude-native",
      model: "",
      permissionMode: "",
      autoAccept: false,
    });
  });

  it("an edit form keeps a saved model id the catalog does not list, shows it, and saves it unchanged", async () => {
    const { daemon } = await renderWithProfiles([{ ...PROFILE, Model: "retired-model" }], PROVIDERS);
    fireEvent.pointerDown(screen.getByTestId("profile-menu-1"), { button: 0 });
    fireEvent.click(await screen.findByTestId("profile-edit-1"));
    await flush();
    expect(screen.getByTestId("profile-edit-form-1-provider")).toHaveTextContent("retired-model");
    fireEvent.click(screen.getByTestId("profile-edit-form-1-submit"));
    await flush();
    expect(daemon.calls.find((c) => c.method === "profile.update")?.params).toMatchObject({ id: 1, model: "retired-model" });
  });

  it("profile list shows the model", async () => {
    await renderWithProfiles([{ ...PROFILE, Model: "opus" }], PROVIDERS);

    expect(screen.getByTestId("profile-row-1")).toHaveTextContent("Opus");
  });
});

describe("profiles-section ★ default agent (run-config IA)", () => {
  it("marks a profile default, and clicking its star again clears it", async () => {
    await renderWithProfiles([PROFILE]);

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
    const { daemon } = await renderWithProfiles([PROFILE]);

    fireEvent.click(screen.getByTestId("profile-default-1"));
    expect(window.localStorage.getItem("smind:settings:defaultAgentId")).toBe("1");

    fireEvent.pointerDown(screen.getByTestId("profile-menu-1"), { button: 0 });
    fireEvent.click(await screen.findByTestId("profile-delete-1"));
    await flush();
    fireEvent.click(screen.getByTestId("profile-delete-confirm-1"));
    await flush();
    daemon.emit("profile.deleted", { id: 1 });
    await flush();

    expect(window.localStorage.getItem("smind:settings:defaultAgentId")).toBeNull();
  });
});

describe("profiles-section inline edit's provider health dot (run-config IA)", () => {
  it("renders the profile's provider health from the shared provider.test cache", async () => {
    // claude-native is credential-backed -- the cached test's key uses the
    // accounts-vocabulary id (accountProvider), not the raw taskrunner
    // provider id, per ProviderInfo.accountProvider's own doc comment.
    const { daemon, view } = await renderWithProfiles(
      [PROFILE],
      { providers: [{ id: "claude-native", label: "Claude Code", credentialKind: "oauth", accountProvider: "anthropic" }] },
    );

    // Opening the editor makes sure its provider has a health answer: one local check, run once however
    // often the form re-renders, written to the shared cache.
    daemon.testResults = { anthropic: { ok: false, detail: "no credential" } };
    fireEvent.pointerDown(screen.getByTestId("profile-menu-1"), { button: 0 });
    fireEvent.click(await screen.findByTestId("profile-edit-1"));
    await flush();
    expect(daemon.countCalls("provider.test")).toBe(1);
    expect(screen.getByTestId("profile-edit-form-1-provider-health")).toHaveAttribute("data-status", "failed");

    // A result landing in the shared cache by another path (a Test button, the sidebar's count, the
    // picker) flips the dot live -- write it the way useRunProviderTest does.
    const { runTest } = renderHookWithDaemon(() => useRunProviderTest(), daemon, {
      initialProps: undefined,
      queryClient: view.queryClient(),
    }).result.current;
    daemon.testResults = { anthropic: { ok: true, detail: "using saved credential" } };
    await act(async () => {
      runTest("anthropic");
    });
    await flush();
    // One more call: the Test button's own. The dot re-read the shared cache; it did not fetch again.
    expect(daemon.countCalls("provider.test")).toBe(2);
    expect(screen.getByTestId("profile-edit-form-1-provider-health")).toHaveAttribute("data-status", "ok");
  });
});

describe("profile form uses the shared run-config pickers (ADR-0032 S4)", () => {
  it("profile form uses the shared provider/model, mode and thinking pickers", async () => {
    await renderWithProfiles([PROFILE]);
    fireEvent.click(screen.getByTestId("profile-new-button"));
    await flush();

    // Provider + model: the composer's picker (its trigger names "Model: <provider · model>", glyph included),
    // in the field variant (full width, opens downwards).
    const picker = screen.getByTestId("profile-form-provider");
    expect(picker).toHaveAttribute("aria-label", expect.stringMatching(/^Model: /));
    expect(within(picker).getByTestId("provider-glyph")).toBeInTheDocument();
    expect(picker.className).toContain("w-full");
    fireEvent.click(picker);
    expect(screen.getByTestId("model-picker-popover")).toHaveAttribute("data-side", "bottom");
    expect(screen.queryByTestId("model-profiles-band")).not.toBeInTheDocument();
    fireEvent.keyDown(screen.getByTestId("model-search-input"), { key: "Escape" });

    // Mode and thinking: the same controls as the toolbar, same labels and help text.
    const mode = screen.getByTestId("profile-form-permission-mode");
    expect(mode).toHaveAttribute("aria-label", "Permission mode");
    expect(screen.getByTestId("profile-form-thinking-level")).toHaveAttribute("aria-label", "Thinking level");

    // The dangerous-mode styling is the toolbar's too: an auto-approving mode flags the field.
    expect(mode).not.toHaveAttribute("data-danger");
    fireEvent.click(mode);
    fireEvent.click(await screen.findByRole("option", { name: "Bypass" }));
    await flush();
    expect(screen.getByTestId("profile-form-permission-mode")).toHaveAttribute("data-danger", "true");
  });
});
