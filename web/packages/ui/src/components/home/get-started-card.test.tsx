import { cleanup, fireEvent, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { GetStartedCard } from "@/components/home/get-started-card";
import { renderWithClientProp as render } from "@/test/render-with-daemon";
import type { ProviderHealth } from "@/lib/model-browser";
import type { ProviderInfo, Workspace } from "@/lib/types";
import { useGetStartedDismissedStore } from "@/stores/get-started-store";

function ws(ID: number, Title: string): Workspace {
  return { ID, Title, Path: `/repos/${Title.toLowerCase()}`, RoutingPolicy: "", CreatedAt: "", UpdatedAt: "" };
}

const PROVIDERS: ProviderInfo[] = [
  { id: "claude-native", label: "Claude Code" },
  { id: "glm", label: "GLM" },
  { id: "kimi", label: "Kimi" },
  { id: "opencode", label: "OpenCode" },
];

const HEALTH: Record<string, ProviderHealth | undefined> = {
  "claude-native": { ok: true, detail: "ok" },
  glm: { ok: false, detail: '"glm" not found on PATH' },
  kimi: { ok: false, detail: "not logged in" },
  opencode: undefined,
};

/** The untested mix minus the ready one: every failure vocabulary in one render. */
const UNREADY: Record<string, ProviderHealth | undefined> = {
  glm: HEALTH.glm,
  kimi: HEALTH.kimi,
  opencode: undefined,
};

function cardProps(over: Partial<Parameters<typeof GetStartedCard>[0]> = {}) {
  return {
    workspaces: [],
    providers: PROVIDERS,
    health: HEALTH,
    rpcFailed: new Set<string>() as ReadonlySet<string>,
    ensure: vi.fn(),
    onAddProject: vi.fn(),
    ...over,
  };
}

describe("GetStartedCard", () => {
  beforeEach(() => {
    window.localStorage.clear();
    useGetStartedDismissedStore.getState().reload();
  });
  afterEach(() => {
    cleanup();
    window.localStorage.clear();
    useGetStartedDismissedStore.getState().reload();
  });

  it("renders both rows with a status word per provider and the project step", () => {
    const props = cardProps({ providers: PROVIDERS.slice(1), health: UNREADY, workspaces: [ws(1, "Alpha")] });
    render(<GetStartedCard {...props} />);
    const card = screen.getByTestId("get-started-card");
    expect(card).toHaveTextContent("Sign in to an agent");
    expect(card).toHaveTextContent("GLM - Not installed");
    expect(card).toHaveTextContent("Kimi - Sign-in needed");
    expect(card).toHaveTextContent("OpenCode - Checking");
    expect(card).toHaveTextContent("Open Settings, then Providers");
    expect(card).toHaveTextContent("Add a project");
    expect(card).toHaveTextContent("Done - Alpha");
  });

  it("a ready agent shows Done on the sign-in row while the project step stays open", () => {
    render(<GetStartedCard {...cardProps({ providers: [PROVIDERS[0]], health: { "claude-native": HEALTH["claude-native"] } })} />);
    const card = screen.getByTestId("get-started-card");
    expect(card).toHaveTextContent("Done");
    expect(screen.getByTestId("get-started-add-project")).toBeInTheDocument();
  });

  it("hidden when an agent is ready and a project exists (and no ensure call)", () => {
    const props = cardProps({ workspaces: [ws(1, "Alpha")], providers: [PROVIDERS[0]], health: { "claude-native": HEALTH["claude-native"] } });
    render(<GetStartedCard {...props} />);
    expect(screen.queryByTestId("get-started-card")).not.toBeInTheDocument();
    expect(props.ensure).not.toHaveBeenCalled();
  });

  it("Dismiss hides the card and the dismissal survives a remount", () => {
    const props = cardProps();
    const view = render(<GetStartedCard {...props} />);
    fireEvent.click(screen.getByRole("button", { name: "Dismiss get started" }));
    expect(screen.queryByTestId("get-started-card")).not.toBeInTheDocument();
    view.unmount();

    render(<GetStartedCard {...cardProps()} />);
    expect(screen.queryByTestId("get-started-card")).not.toBeInTheDocument();
    expect(window.localStorage.getItem("smind:get-started-dismissed")).toBe("1");
  });

  it("ensure runs exactly once across re-renders", () => {
    const props = cardProps();
    const view = render(<GetStartedCard {...props} />);
    view.rerender(<GetStartedCard {...props} />);
    view.rerender(<GetStartedCard {...props} />);
    expect(props.ensure).toHaveBeenCalledTimes(1);
  });

  it("ensure never runs for a dismissed card", () => {
    useGetStartedDismissedStore.getState().set(true);
    const props = cardProps();
    render(<GetStartedCard {...props} />);
    expect(screen.queryByTestId("get-started-card")).not.toBeInTheDocument();
    expect(props.ensure).not.toHaveBeenCalled();
  });

  it("Add a project calls onAddProject", () => {
    const props = cardProps();
    render(<GetStartedCard {...props} />);
    fireEvent.click(screen.getByTestId("get-started-add-project"));
    expect(props.onAddProject).toHaveBeenCalledTimes(1);
  });
});
