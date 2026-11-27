import { describe, expect, it } from "vitest";

import { daemonSentence } from "@/lib/daemon-sentence";

describe("daemonSentence", () => {
  it("daemon_sentence_drops_the_method_and_package_labels", () => {
    const table: [string, string][] = [
      ["profile.rolePack.apply: backup directory /root/x: permission denied", "Backup directory /root/x: permission denied."],
      ["profile.rolePack.check: database is locked", "Database is locked."],
      ['browser.settings.set: browser: allowlist entry "*.x" must be one exact host.', 'Allowlist entry "*.x" must be one exact host.'],
      ['browser: allowlist entry "x" is not valid', 'Allowlist entry "x" is not valid.'],
      ["database is locked", "Database is locked."],
      ["Already a sentence.", "Already a sentence."],
      ["", ""],
      ["error: keep this label", "Error: keep this label."],
    ];
    for (const [raw, want] of table) expect(daemonSentence(raw), raw).toBe(want);
  });
});
