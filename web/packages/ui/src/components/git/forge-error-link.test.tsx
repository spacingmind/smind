import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { flush, gitState, Harness } from "@/components/git/pr-test-support";
import { PrChip } from "@/components/git/pr-chip";
import { describePrError } from "@/components/git/pr-error";
import { PrErrorLine } from "@/components/git/pr-error-line";
import { RpcError } from "@/lib/ws-client";
import { FakeWsClient } from "@/test/fake-ws-client";
import { renderWithClientProp } from "@/test/render-with-daemon";
import { openPr } from "@/components/git/pr-test-support";

/** The one deep-link event App.tsx turns into "open Settings at this section". */
function captureOpenSettings() {
  const seen: unknown[] = [];
  const listener = (e: Event) => seen.push((e as CustomEvent<{ sectionId?: string }>).detail.sectionId);
  window.addEventListener("smind:open-settings", listener);
  return { seen, stop: () => window.removeEventListener("smind:open-settings", listener) };
}

let stop: (() => void) | undefined;
afterEach(() => stop?.());

describe("forge setup links", () => {
  it("links forge_unauthenticated to Settings → GitHub", () => {
    const cap = captureOpenSettings();
    stop = cap.stop;
    const view = describePrError(new RpcError("not signed in", "forge_unauthenticated"), "create");
    render(<PrErrorLine error={view} testId="e" />);
    fireEvent.click(screen.getByTestId("e-settings-link"));
    expect(cap.seen).toEqual(["github"]);
  });

  it("links forge_cli_missing, but not unrelated codes", () => {
    render(<PrErrorLine error={describePrError(new RpcError("x", "forge_cli_missing"), "create")} testId="a" />);
    expect(screen.getByTestId("a-settings-link")).toBeInTheDocument();
    render(<PrErrorLine error={describePrError(new RpcError("x", "forge_timeout"), "create")} testId="b" />);
    expect(screen.queryByTestId("b-settings-link")).not.toBeInTheDocument();
  });

  it("links a disabled Create PR reason", async () => {
    const cap = captureOpenSettings();
    stop = cap.stop;
    const client = new FakeWsClient();
    renderWithClientProp(<Harness client={client} />);
    client.nth("task.git", 0).resolve(
      gitState({ actions: { createPr: { enabled: false, code: "forge_unauthenticated", reason: "GitHub isn't signed in on the daemon host." } } }),
    );
    await flush();
    fireEvent.click(screen.getByTestId("pr-action-settings-link"));
    expect(cap.seen).toEqual(["github"]);
  });

  it("links a stale unauthenticated PR chip, and only that", () => {
    const cap = captureOpenSettings();
    stop = cap.stop;
    const { rerender } = render(<PrChip pr={openPr({ stale: "unauthenticated" })} />);
    fireEvent.click(screen.getByTestId("pr-chip-settings-link"));
    expect(cap.seen).toEqual(["github"]);
    rerender(<PrChip pr={openPr({ stale: "" })} />);
    expect(screen.queryByTestId("pr-chip-settings-link")).not.toBeInTheDocument();
  });
});
