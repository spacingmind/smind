import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative, sep } from "node:path";
import { describe, expect, it } from "vitest";

const SRC = join(__dirname, "..");

function sources(dir: string, out: string[] = []): string[] {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) sources(p, out);
    else if (/\.tsx?$/.test(name) && !/\.test\.tsx?$/.test(name)) out.push(p);
  }
  return out;
}

/** Files that may read the interface mode: they branch the UI on it and send nothing. */
const ALLOWED = new Set([
  "lib/interface-mode.ts",
  "lib/storage.ts",
  "stores/interface-mode-store.ts",
  "hooks/use-greeting.ts",
  "components/home/home-landing.tsx",
  "components/timeline/tool-call.tsx",
]);

describe("interface mode is client-only (ADR-0049)", () => {
  it("interface_mode_never_on_the_wire", () => {
    const users = sources(SRC)
      .filter((f) => /interface-mode|interfaceMode|InterfaceMode/.test(readFileSync(f, "utf8")))
      .map((f) => relative(SRC, f).split(sep).join("/"));
    // Any new reader must be reviewed here; the transport and call sites must never appear.
    expect(users.filter((f) => !ALLOWED.has(f))).toEqual([]);
    for (const f of users) expect(f).not.toMatch(/ws-client|relay|use-run-timeline|rpc|api/);
  });
});
