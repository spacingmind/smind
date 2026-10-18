import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";
import { act } from "react";
import { fireEvent, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { AppSidebar } from "@/components/app-sidebar";
import { Composer } from "@/components/composer/composer";
import "@/components/settings/profiles-section";
import "@/components/settings/providers-section";
import { listSettingsSections } from "@/components/settings/settings-registry";
import { TaskDetailPane } from "@/components/task-detail";
import { SidebarProvider } from "@/components/ui/sidebar";
import { KeyboardProvider } from "@/keyboard/keyboard-provider";
import { PaletteProvider, useRegisteredCommands } from "@/palette/palette-provider";
import type { AgentProfile, Chat, Task } from "@/lib/types";
import { FakeWsClient } from "@/test/fake-ws-client";
import { useProviderCatalog } from "@/hooks/use-provider-catalog";
import { createQueryClient } from "@/lib/query/query-client";
import { renderHookWithDaemon, renderWithClientProp, renderWithDaemon } from "@/test/render-with-daemon";
import { FakeUsageDaemon } from "@/test/usage-test-client";

/**
 * ADR-0032 S3 (AC10-AC13): provider.list / profile.list / provider.test are ONE cache entry per daemon
 * connection however many surfaces mount, and a Settings edit reaches every other surface live.
 */

const TASK: Task = {
  ID: 1,
  WorkspaceID: 1,
  SpaceID: null,
  Title: "Task",
  Status: "active",
  WorktreePath: "/tmp/a",
  Branch: "task",
  CreatedAt: "2024-01-01T00:00:00Z",
  UpdatedAt: "2024-01-01T00:00:00Z",
  ArchivedAt: null,
};
const CHAT: Chat = { ID: 10, TaskID: 1, Title: "Chat", Provider: null, AgentSession: null, CreatedAt: "2024-01-01T00:00:00Z", ArchivedAt: null };

const PROVIDERS = {
  providers: [
    { id: "claude-native" as const, label: "Claude Code", credentialKind: "oauth" as const, accountProvider: "anthropic" },
    { id: "codex-native" as const, label: "Codex", credentialKind: "oauth" as const, accountProvider: "openai" },
    { id: "glm" as const, label: "GLM", kind: "cli" as const },
  ],
};

function profile(id: number, name: string): AgentProfile {
  return {
    ID: id,
    Name: name,
    Provider: "claude-native",
    PermissionMode: "",
    AutoAccept: false,
    ThinkingLevel: "",
    Model: "",
    Notes: "",
    CreatedAt: "2026-09-25T00:00:00Z",
    UpdatedAt: "2026-09-25T00:00:00Z",
  } as AgentProfile;
}

function account(id: number, provider: string, label: string) {
  return { id, provider, label, credentialType: "oauth", createdAt: "", updatedAt: "" };
}

async function flush(): Promise<void> {
  await act(async () => {
    for (let i = 0; i < 6; i++) await Promise.resolve();
  });
}

function section(id: string, d: FakeUsageDaemon) {
  return listSettingsSections().find((s) => s.id === id)!.render({ client: d.client, events: d.events });
}

function composer(client: unknown) {
  return (
    <Composer
      client={client as never}
      taskId={1}
      chatId={10}
      isDefaultChat
      boundProvider={null}
      connected
      runningRunId={null}
      queuedItems={[]}
      onCancelQueued={async () => {}}
      onSubmit={async () => ({ kind: "started", runId: "r" })}
      onStop={async () => {}}
    />
  );
}

describe("shared catalog / profile / health cache", () => {
  it("provider.list fetched once with composer, sidebar, task header and settings mounted", async () => {
    const d = new FakeUsageDaemon([]);
    d.providers = PROVIDERS as never;
    d.profiles = [profile(1, "UI work")];
    renderWithDaemon(
      <KeyboardProvider>
        <PaletteProvider>
          <SidebarProvider>
            <AppSidebar client={d.client as never} selectedTaskId={null} />
          </SidebarProvider>
          <TaskDetailPane client={d.client as never} task={TASK} chat={CHAT} isDefaultChat />
          {composer(d.client)}
          {section("providers", d)}
          {section("agents", d)}
        </PaletteProvider>
      </KeyboardProvider>,
      d,
    );
    await flush();

    const count = (method: string) => d.calls.filter((c) => c.method === method).length;
    expect(count("provider.list")).toBe(1);
    expect(count("profile.list")).toBe(1);
    // One local health check per provider key (anthropic, openai, glm), run by the sidebar's ensure() and
    // shared by the picker and the dots: not zero, not one per surface.
    expect(count("provider.test")).toBe(3);
    // And nothing here may ever ask a provider for plan usage (ADR-0028).
    expect(d.count("touch")).toBe(0);
    expect(d.count("refresh")).toBe(0);
  });

  it("profile created in settings appears in composer picker and palette", async () => {
    const d = new FakeUsageDaemon([]);
    d.providers = PROVIDERS as never;
    d.profiles = [];
    let commands: ReturnType<typeof useRegisteredCommands> = [];
    function Probe() {
      commands = useRegisteredCommands();
      return null;
    }
    renderWithDaemon(
      <KeyboardProvider>
        <PaletteProvider>
          <Probe />
          <SidebarProvider>
            <AppSidebar client={d.client as never} selectedTaskId={null} />
          </SidebarProvider>
          {composer(d.client)}
          {section("agents", d)}
        </PaletteProvider>
      </KeyboardProvider>,
      d,
    );
    await flush();
    expect(commands.some((c) => c.title.startsWith("Use agent:"))).toBe(false);

    // Create through the real Settings -> Agents form.
    fireEvent.click(screen.getByTestId("profile-new-button"));
    await flush();
    fireEvent.change(screen.getByTestId("profile-form-name"), { target: { value: "Reviewer" } });
    fireEvent.click(screen.getByTestId("profile-form-submit"));
    await flush();

    // The palette's "Use agent:" entry and the composer picker both have it, with no remount.
    expect(commands.some((c) => c.title === "Use agent: Reviewer")).toBe(true);
    await act(async () => {
      await new Promise((r) => setTimeout(r, 50));
    });
    // Before the picker opens "Reviewer" is on screen once (the Settings row); inside it, a second time.
    expect(screen.getAllByText("Reviewer")).toHaveLength(1);
    fireEvent.click(screen.getByTestId("composer-model-picker"));
    fireEvent.click(await screen.findByTestId("model-picker-back"));
    expect((await screen.findAllByText("Reviewer")).length).toBeGreaterThanOrEqual(2);

    // A rename and a delete (events from another tab or the CLI) reach them live too.
    const created = d.profiles[0]!;
    await act(async () => d.emit("profile.updated", { profile: { ...created, Name: "Reviewer 2" } }));
    expect(commands.some((c) => c.title === "Use agent: Reviewer 2")).toBe(true);
    expect(commands.some((c) => c.title === "Use agent: Reviewer")).toBe(false);
    // The open picker shows the renamed entry too (Settings row + picker entry), and not the old name.
    expect((await screen.findAllByText("Reviewer 2")).length).toBeGreaterThanOrEqual(2);
    expect(screen.queryAllByText("Reviewer")).toHaveLength(0);
    d.profiles = []; // the daemon's truth after the delete
    await act(async () => d.emit("profile.deleted", { id: created.ID }));
    expect(commands.some((c) => c.title.startsWith("Use agent:"))).toBe(false);
    expect(screen.queryAllByText("Reviewer 2")).toHaveLength(0);
    // A late update for the deleted profile does not bring it back (one re-read settles it).
    await act(async () => d.emit("profile.updated", { profile: { ...created, Name: "Reviewer 3" } }));
    await flush();
    expect(commands.some((c) => c.title.startsWith("Use agent:"))).toBe(false);
  });

  it("sidebar health count matches providers dots", async () => {
    const d = new FakeUsageDaemon([]);
    d.providers = PROVIDERS as never;
    d.accounts = [account(1, "anthropic", "work"), account(2, "openai", "codex")];
    d.testResults = {
      anthropic: { ok: true, detail: "ok" },
      openai: { ok: false, detail: "no credential" },
      glm: { ok: true, detail: "ok" },
    };
    renderWithDaemon(
      <KeyboardProvider>
        <PaletteProvider>
          <SidebarProvider>
            <AppSidebar client={d.client as never} selectedTaskId={null} />
          </SidebarProvider>
          {section("providers", d)}
        </PaletteProvider>
      </KeyboardProvider>,
      d,
    );
    await flush();

    fireEvent.click(screen.getByTestId("sidebar-footer-host"));
    const healthy = Number(/(\d+) healthy/.exec(screen.getByTestId("sidebar-footer-host-providers").textContent ?? "")?.[1]);
    // Providers' account rows: claude (anthropic) ok, codex (openai) failing; GLM has no account row.
    const okDots = screen.getAllByTestId(/^provider-row-dot-/).filter((dot) => dot.getAttribute("data-status") === "ok");
    // The footer counts providers through the same cache: every provider whose test answered ok (anthropic, glm).
    const okProviders = Object.values(d.testResults).filter((r) => r.ok).length;
    expect(okDots).toHaveLength(1);
    expect(healthy).toBe(okProviders);
    expect(within(screen.getByTestId("provider-row-2")).getByTestId("provider-row-dot-2")).toHaveAttribute("data-status", "failed");

    // Pressing Test on the failing row after it recovers flips the dot AND the footer count together.
    d.testResults = { ...d.testResults, openai: { ok: true, detail: "ok" } };
    fireEvent.click(within(screen.getByTestId("provider-row-2")).getByRole("button", { name: /test/i }));
    await flush();
    expect(screen.getByTestId("provider-row-dot-2")).toHaveAttribute("data-status", "ok");
    expect(screen.getByTestId("sidebar-footer-host-providers")).toHaveTextContent("3 healthy");
  });

  it("a provider.test that errors counts as not healthy: the footer count still shows", async () => {
    const d = new FakeUsageDaemon([]);
    d.providers = PROVIDERS as never;
    d.testRpcErrors = { glm: new Error("provider.test: spawn failed") };
    renderWithDaemon(
      <KeyboardProvider>
        <PaletteProvider>
          <SidebarProvider>
            <AppSidebar client={d.client as never} selectedTaskId={null} />
          </SidebarProvider>
        </PaletteProvider>
      </KeyboardProvider>,
      d,
    );
    await flush();
    fireEvent.click(screen.getByTestId("sidebar-footer-host"));
    // anthropic + openai answer ok, glm's check errored: 2 of 3, and the count is not hidden.
    expect(screen.getByTestId("sidebar-footer-host-providers")).toHaveTextContent("2 healthy");
  });

  it("account.updated drops that provider's health answer and the footer re-tests it", async () => {
    const d = new FakeUsageDaemon([]);
    d.providers = PROVIDERS as never;
    d.accounts = [account(1, "anthropic", "work")];
    renderWithDaemon(
      <KeyboardProvider>
        <PaletteProvider>
          <SidebarProvider>
            <AppSidebar client={d.client as never} selectedTaskId={null} />
          </SidebarProvider>
        </PaletteProvider>
      </KeyboardProvider>,
      d,
    );
    await flush();
    const tests = (key: string) => d.calls.filter((c) => c.method === "provider.test" && (c.params as { provider: string }).provider === key).length;
    expect(tests("anthropic")).toBe(1);
    // Credential re-login: the provider's earlier answer is stale; exactly that provider is re-tested.
    await act(async () => d.emit("account.updated", { account: account(1, "anthropic", "work") }));
    await flush();
    expect(tests("anthropic")).toBe(2);
    expect(tests("openai")).toBe(1);
  });

  it("a failed provider.list is not re-sent by every component that mounts afterwards", async () => {
    const d = new FakeUsageDaemon([]);
    d.failNonUsageWith = new Error("provider.list down");
    const qc = createQueryClient();
    const first = renderHookWithDaemon(() => useProviderCatalog(), d, { initialProps: undefined, queryClient: qc });
    await flush();
    expect(d.countCalls("provider.list")).toBe(1);
    expect(first.result.current.settled).toBe(true);
    first.unmount();
    // Another surface mounts later against the same connection's cache: the failure is remembered, not retried.
    renderHookWithDaemon(() => useProviderCatalog(), d, { initialProps: undefined, queryClient: qc });
    await flush();
    expect(d.countCalls("provider.list")).toBe(1);
  });

  it("one fallback provider list constant", () => {
    const root = join(__dirname, "..");
    const declarations: string[] = [];
    const walk = (dir: string) => {
      for (const name of readdirSync(dir)) {
        const path = join(dir, name);
        if (statSync(path).isDirectory()) walk(path);
        else if (/\.tsx?$/.test(name) && !/\.test\./.test(name) && /const FALLBACK_PROVIDERS\b/.test(readFileSync(path, "utf8"))) {
          declarations.push(path.slice(root.length + 1));
        }
      }
    };
    walk(root);
    expect(declarations).toEqual(["lib/provider-catalog-fallback.ts"]);
    // And no inline copy of the two-provider list under another name.
    const inline: string[] = [];
    const walkInline = (dir: string) => {
      for (const name of readdirSync(dir)) {
        const path = join(dir, name);
        if (statSync(path).isDirectory()) walkInline(path);
        else if (/\.tsx?$/.test(name) && !/\.test\./.test(name) && /\[\s*\{\s*id:\s*"claude-native"\s*\}\s*,\s*\{\s*id:\s*"glm"\s*\}\s*\]/.test(readFileSync(path, "utf8"))) {
          inline.push(path.slice(root.length + 1));
        }
      }
    };
    walkInline(root);
    expect(inline).toEqual(["lib/provider-catalog-fallback.ts"]);
  });
});

// A bare FakeWsClient variant: nothing answers, so exactly what each surface *asked* is visible.
describe("shared catalog requests (unanswered client)", () => {
  it("composer and task header share one provider.list and one profile.list request", async () => {
    const client = new FakeWsClient();
    renderWithClientProp(
      <KeyboardProvider>
        <TaskDetailPane client={client as never} task={TASK} chat={CHAT} isDefaultChat />
      </KeyboardProvider>,
    );
    await flush();
    expect(client.calls.filter((c) => c.method === "provider.list")).toHaveLength(1);
    expect(client.calls.filter((c) => c.method === "profile.list")).toHaveLength(1);
  });
});
