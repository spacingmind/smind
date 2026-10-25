import { act } from "react";
import { cleanup } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { useSlashPaletteCommands } from "@/hooks/use-slash-palette-commands";
import type { PluginCommand } from "@/lib/plugin-contrib";
import { useComposerInsertStore } from "@/stores/composer-insert-store";
import { PaletteProvider, useRegisteredCommands } from "@/palette/palette-provider";
import { FakeWsClient } from "@/test/fake-ws-client";
import { renderWithClientProp } from "@/test/render-with-daemon";

const COMMANDS: PluginCommand[] = [
  { plugin: "acme", name: "review", qualified: "acme:review", description: "Review the diff", prompt: "Review it." },
  { plugin: "zeta", name: "review", qualified: "zeta:review", prompt: "Zeta." },
];

let listed: ReturnType<typeof useRegisteredCommands> = [];
function Probe({ client }: { client: FakeWsClient }) {
  void client;
  useSlashPaletteCommands();
  listed = useRegisteredCommands();
  return null;
}

async function flush() {
  await act(async () => {
    await new Promise((r) => setTimeout(r, 0));
  });
}

async function mount() {
  const client = new FakeWsClient();
  renderWithClientProp(
    <PaletteProvider>
      <Probe client={client} />
    </PaletteProvider>,
  );
  client.nth("plugin.commands", 0).resolve(COMMANDS);
  await flush();
}

beforeEach(() => useComposerInsertStore.setState({ mounted: [], target: null, request: null }));
afterEach(cleanup);

describe("slash commands in the command palette", () => {
  it("TestSlashCommands_PaletteInsertsNeverSends: 'Insert command' entries ask the focused composer to insert; nothing sends", async () => {
    await mount();
    // No composer mounted: nothing to insert into, so no entries.
    expect(listed.filter((c) => c.title.startsWith("Insert command"))).toEqual([]);
    act(() => useComposerInsertStore.setState({ mounted: ["1:10"], target: "1:10" }));
    await flush();
    const entries = listed.filter((c) => c.title.startsWith("Insert command"));
    expect(entries.map((c) => c.title)).toEqual(["Insert command: /review", "Insert command: /zeta:review"]);
    expect(entries[0]!.subtitle).toBe("Review the diff · from acme");
    act(() => entries[0]!.run());
    expect(useComposerInsertStore.getState().request).toMatchObject({ key: "1:10", text: "Review it." });
  });

  it("registers nothing when no plugin has commands", async () => {
    const client = new FakeWsClient();
    renderWithClientProp(
      <PaletteProvider>
        <Probe client={client} />
      </PaletteProvider>,
    );
    client.nth("plugin.commands", 0).resolve([]);
    act(() => useComposerInsertStore.setState({ mounted: ["1:10"], target: "1:10" }));
    await flush();
    expect(listed.some((c) => c.title.startsWith("Insert command"))).toBe(false);
  });
});

