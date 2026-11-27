import fs from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";

import { CONFIG_CONFIRM, isWritableConfigKey, WRITABLE_CONFIG_KEYS } from "@/lib/config-keys";

const allowlist = fs.readFileSync(path.resolve(__dirname, "../../../../../internal/config/allowlist.go"), "utf8");

/** Every `{Key: "...", ... Writable: true?, ... Confirm: "..."?}` row of the Go table, in order. */
function rows(): { key: string; writable: boolean; confirm?: string }[] {
  const out: { key: string; writable: boolean; confirm?: string }[] = [];
  const re = /\{Key: "([^"]+)"([\s\S]*?)\},?\n/g;
  const table = allowlist.slice(allowlist.indexOf("var configKeys"));
  for (const m of table.matchAll(re)) {
    const confirm = /Confirm: "((?:[^"\\]|\\.)*)"/.exec(m[2]);
    out.push({
      key: m[1],
      writable: /Writable: true/.test(m[2]),
      confirm: confirm ? (JSON.parse(`"${confirm[1]}"`) as string) : undefined,
    });
  }
  return out;
}

describe("config keys", () => {
  it("config_browser_disabled_never_gets_a_switch", () => {
    const all = rows();
    expect(all.length).toBeGreaterThan(10);
    expect(all.some((r) => r.key === "browser.disabled")).toBe(true);
    expect(isWritableConfigKey("browser.disabled")).toBe(false);
    for (const r of all) expect(isWritableConfigKey(r.key), r.key).toBe(r.writable);
    expect(all.filter((r) => isWritableConfigKey(r.key)).map((r) => r.key).sort()).toEqual([...WRITABLE_CONFIG_KEYS].sort());
    expect(all.filter((r) => r.writable)).toHaveLength(5);
    expect(isWritableConfigKey("anything.else")).toBe(false);
  });

  it("config_confirm_sentences_match_the_daemon_table", () => {
    const all = rows();
    for (const key of WRITABLE_CONFIG_KEYS) {
      const row = all.find((r) => r.key === key);
      expect(row?.confirm, key).toBeTruthy();
      expect(CONFIG_CONFIRM[key], key).toBe(row?.confirm);
    }
  });
});
