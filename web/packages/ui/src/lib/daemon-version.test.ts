import { afterEach, describe, expect, it } from "vitest";

import {
  compareVersions,
  dismissNotice,
  isNoticeDismissed,
  managedByLabel,
  noticeToken,
  updateCommands,
} from "@/lib/daemon-version";

afterEach(() => window.localStorage.clear());

describe("compareVersions (mirrors the Rust daemon_manager::version::compare)", () => {
  it("orders clean releases from the daemon's point of view", () => {
    expect(compareVersions("0.12.0", "0.11.1")).toBe("older");
    expect(compareVersions("0.12.0", "0.12.0")).toBe("same");
    expect(compareVersions("0.12.0", "0.12.1")).toBe("newer");
    expect(compareVersions("1.0.0", "0.99.99")).toBe("older");
    expect(compareVersions("0.9.0", "0.10.0")).toBe("newer");
  });

  it("tolerates a v prefix on either side", () => {
    expect(compareVersions("v0.12.0", "0.11.0")).toBe("older");
    expect(compareVersions("0.12.0", "v0.12.0")).toBe("same");
  });

  it("is unknown for a dev build or anything unparseable, so it never nags", () => {
    for (const bad of ["dev", "0.12.0-dev+abc1234", "0.12.0-dev+abc.dirty", "", "foo", "1.2", "1.2.3.4", "1.2.x", "1.2.3-rc1"]) {
      expect(compareVersions("0.12.0", bad)).toBe("unknown");
      expect(compareVersions(bad, "0.12.0")).toBe("unknown");
    }
  });
});

describe("updateCommands", () => {
  it("offers smind update once the daemon has the command", () => {
    for (const v of ["0.12.0", "0.12.1", "1.0.0", "v0.13.0"]) {
      expect(updateCommands(v)).toEqual([{ label: null, command: "smind update" }]);
    }
  });

  it("offers the installer one-liners for a daemon older than smind update", () => {
    for (const v of ["0.11.1", "0.10.0", "0.9.9"]) {
      expect(updateCommands(v).map((c) => c.command)).toEqual([
        "curl -fsSL https://spacingmind.com/install.sh | sh",
        "irm https://spacingmind.com/install.ps1 | iex",
      ]);
    }
  });

  it("offers the installers when the version is unknown or unparseable", () => {
    expect(updateCommands(null)).toHaveLength(2);
    expect(updateCommands("dev")).toHaveLength(2);
  });
});

describe("managedByLabel", () => {
  it("names the service managers", () => {
    expect(managedByLabel("systemd")).toBe("systemd");
    expect(managedByLabel("launchd")).toBe("launchd");
    expect(managedByLabel("taskscheduler")).toBe("Task Scheduler");
    expect(managedByLabel("task")).toBe("Task Scheduler");
  });
});

describe("dismissal", () => {
  it("is per connection and per daemon/app version pair", () => {
    const token = noticeToken("0.11.1", "0.12.0");
    expect(isNoticeDismissed("relay:a", token)).toBe(false);
    dismissNotice("relay:a", token);
    expect(isNoticeDismissed("relay:a", token)).toBe(true);
    // another connection, another daemon version, another app version: shown again
    expect(isNoticeDismissed("relay:b", token)).toBe(false);
    expect(isNoticeDismissed("relay:a", noticeToken("0.11.2", "0.12.0"))).toBe(false);
    expect(isNoticeDismissed("relay:a", noticeToken("0.11.1", "0.13.0"))).toBe(false);
  });

  it("keeps other connections' dismissals", () => {
    dismissNotice("a", "1->2");
    dismissNotice("b", "3->4");
    expect(isNoticeDismissed("a", "1->2")).toBe(true);
    expect(isNoticeDismissed("b", "3->4")).toBe(true);
  });

  it("survives corrupt storage", () => {
    window.localStorage.setItem("smind:dismissed-daemon-notices", "{nope");
    expect(isNoticeDismissed("a", "1->2")).toBe(false);
    dismissNotice("a", "1->2");
    expect(isNoticeDismissed("a", "1->2")).toBe(true);
  });

  it("names an unknown version as such", () => {
    expect(noticeToken(null, "0.12.0")).toBe("unknown->0.12.0");
  });
});
