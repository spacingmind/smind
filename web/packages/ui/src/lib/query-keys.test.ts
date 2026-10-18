import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative } from "node:path";
import { describe, expect, it } from "vitest";

import { queryKeys } from "@/lib/query-keys";

const SRC = join(__dirname, "..");

function sourceFiles(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) return sourceFiles(path);
    return /\.(ts|tsx)$/.test(name) && !/\.test\.(ts|tsx)$/.test(name) ? [path] : [];
  });
}

describe("query keys", () => {
  it("query keys only come from query-keys.ts", () => {
    const offenders = sourceFiles(SRC)
      .filter((path) => relative(SRC, path) !== join("lib", "query-keys.ts"))
      .filter((path) => /queryKey\s*:\s*\[/.test(readFileSync(path, "utf8")))
      .map((path) => relative(SRC, path));
    expect(offenders).toEqual([]);
  });

  it("is the RPC name plus its params", () => {
    expect(queryKeys.accountList()).toEqual(["account.list"]);
    expect(queryKeys.accountUsage()).toEqual(["account.usage", { cached: true }]);
    expect(queryKeys.providerTest("anthropic")).toEqual(["provider.test", "anthropic"]);
  });
});
