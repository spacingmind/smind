import { act } from "react";
import { fireEvent, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { LearnedSkillDetail } from "@/components/learning/learned-skill-detail";
import { RunLinkProvider } from "@/components/learning/run-link";
import type { ExportResult, LearnedSkillDetail as Detail } from "@/lib/learning-types";
import { RpcError } from "@/lib/ws-client";
import { skill } from "@/test/learning-fixtures";
import { learningDaemon } from "@/test/learning-test-client";
import { renderWithDaemon } from "@/test/render-with-daemon";

async function flush(): Promise<void> {
  await act(async () => {
    for (let i = 0; i < 8; i++) await Promise.resolve();
  });
}

function detail(status: "active" | "disabled" | "archived" = "active"): Detail {
  return {
    skill: skill({ id: 5, name: "run-focused-tests", version: 1, status }),
    versions: [{ version: 1, description: "Run tests.", body: "# Tests\n", actor: "reflector", createdAt: "2026-10-10T10:00:00Z" }],
    ledger: [],
  };
}

const INSTALLED: ExportResult = {
  dir: "/home/u/.claude/skills/run-focused-tests",
  files: ["/home/u/.claude/skills/run-focused-tests/SKILL.md"],
  status: "installed",
  skipped: [],
};
const MODIFIED: ExportResult = {
  dir: "/home/u/.claude/skills/run-focused-tests",
  files: [],
  status: "modified",
  skipped: ["/home/u/.claude/skills/run-focused-tests/SKILL.md"],
};

function setup(exportReply: (params: unknown, n: number) => ExportResult | Promise<ExportResult>, status: "active" | "disabled" | "archived" = "active") {
  const exports: unknown[] = [];
  const d = learningDaemon((method, params) => {
    if (method === "learning.skills.get") return detail(status);
    if (method === "learning.skills.export") {
      exports.push(params);
      return exportReply(params, exports.length);
    }
    return null;
  });
  renderWithDaemon(
    <RunLinkProvider closeSettings={() => {}}>
      <LearnedSkillDetail id={5} onBack={() => {}} />
    </RunLinkProvider>,
    d,
  );
  return { d, exports };
}

async function open() {
  await flush();
  fireEvent.click(screen.getByTestId("skill-export"));
  await flush();
}

const dialog = () => screen.getByTestId("skill-export-dialog");
const submit = () => within(dialog()).getByTestId("skill-export-submit") as HTMLButtonElement;

describe("export a learned skill", () => {
  it("opens a dialog with the four targets, the repo one chosen, and the ownership sentence", async () => {
    setup(() => INSTALLED);
    await open();
    const radios = within(dialog()).getAllByRole("radio") as HTMLInputElement[];
    expect(radios.map((r) => r.value)).toEqual(["repo", "claude", "agents", "codex"]);
    expect(radios[0]!.checked).toBe(true);
    expect(dialog().textContent).toContain("This repository (its skills folder)");
    expect(dialog().textContent).toContain("~/.claude/skills");
    expect(dialog().textContent).toContain("smind stops managing it");
    expect(within(dialog()).queryByTestId("skill-export-force")).toBeNull();
  });

  it("sends the chosen target and shows the directory and written files", async () => {
    const { exports } = setup(() => INSTALLED);
    await open();
    fireEvent.click(within(dialog()).getByRole("radio", { name: /~\/\.claude\/skills/ }));
    fireEvent.click(submit());
    await flush();
    expect(exports).toEqual([{ id: 5, target: "claude" }]);
    expect(within(dialog()).getByTestId("skill-export-dir").textContent).toBe(INSTALLED.dir);
    expect(within(dialog()).getByTestId("skill-export-dir").className).toContain("font-mono");
    expect(within(dialog()).getAllByTestId("skill-export-file").map((f) => f.textContent)).toEqual(INSTALLED.files);
  });

  it("a modified result warns, lists what was skipped and offers Replace, which re-sends with force", async () => {
    const { exports } = setup((_p, n) => (n === 1 ? MODIFIED : INSTALLED));
    await open();
    fireEvent.click(submit());
    await flush();
    const warning = within(dialog()).getByTestId("skill-export-modified");
    expect(warning.textContent).toContain("A different SKILL.md is already there and was left alone");
    expect(warning.textContent).toContain(MODIFIED.skipped[0]);
    const force = within(dialog()).getByTestId("skill-export-force");
    expect(force).toHaveAttribute("aria-checked", "false");
    fireEvent.click(force);
    fireEvent.click(submit());
    await flush();
    expect(exports).toEqual([
      { id: 5, target: "repo" },
      { id: 5, target: "repo", force: true },
    ]);
    expect(within(dialog()).queryByTestId("skill-export-modified")).toBeNull();
    expect(within(dialog()).getAllByTestId("skill-export-file")).toHaveLength(1);
  });

  it("changing the target clears the previous result and the Replace option", async () => {
    setup(() => MODIFIED);
    await open();
    fireEvent.click(submit());
    await flush();
    fireEvent.click(within(dialog()).getByTestId("skill-export-force"));
    fireEvent.click(within(dialog()).getByRole("radio", { name: /~\/\.codex\/skills/ }));
    expect(within(dialog()).queryByTestId("skill-export-modified")).toBeNull();
    expect(within(dialog()).queryByTestId("skill-export-force")).toBeNull();
  });

  it("an idempotent re-export says it is already up to date", async () => {
    setup(() => ({ dir: "/r/.agents/skills/run-focused-tests", files: [], status: "installed", skipped: [] }));
    await open();
    fireEvent.click(submit());
    await flush();
    expect(within(dialog()).getByTestId("skill-export-uptodate").textContent).toBe("Already up to date");
  });

  it("drops the daemon's method prefix from an error message", async () => {
    setup(() => {
      throw new RpcError("learning.skills.export: repo skills dir is a symlink", "invalid_params");
    });
    await open();
    fireEvent.click(submit());
    await flush();
    const text = within(dialog()).getByTestId("skill-export-error").textContent ?? "";
    expect(text).toContain("repo skills dir is a symlink");
    expect(text).not.toContain("learning.skills.export:");
  });

  it("shows an invalid_params error verbatim", async () => {
    const message = "repo skills dir /r/.agents/skills is a symlink; refusing to write through it";
    setup(() => {
      throw new RpcError(message, "invalid_params");
    });
    await open();
    fireEvent.click(submit());
    await flush();
    const alert = within(dialog()).getByTestId("skill-export-error");
    expect(alert.textContent).toContain(message);
    expect(alert.getAttribute("role")).toBe("alert");
    expect(submit().disabled).toBe(false);
  });

  it("disables every control while the export is pending", async () => {
    let release: (r: ExportResult) => void = () => {};
    setup(() => new Promise<ExportResult>((resolve) => (release = resolve)));
    await open();
    fireEvent.click(submit());
    await flush();
    expect(submit().disabled).toBe(true);
    expect(submit().textContent).toBe("Exporting…");
    for (const r of within(dialog()).getAllByRole("radio")) expect((r as HTMLInputElement).disabled).toBe(true);
    expect((within(dialog()).getByTestId("skill-export-cancel") as HTMLButtonElement).disabled).toBe(true);
    await act(async () => release(INSTALLED));
    await flush();
    expect(submit().disabled).toBe(false);
  });

  it("closes on Escape and starts fresh when reopened", async () => {
    setup(() => INSTALLED);
    await open();
    fireEvent.click(submit());
    await flush();
    fireEvent.keyDown(dialog(), { key: "Escape" });
    await flush();
    expect(screen.queryByTestId("skill-export-dialog")).toBeNull();
    fireEvent.click(screen.getByTestId("skill-export"));
    await flush();
    expect(within(dialog()).queryByTestId("skill-export-file")).toBeNull();
  });

  it.each(["archived", "disabled"] as const)("a %s skill can be exported too", async (status) => {
    const { exports } = setup(() => INSTALLED, status);
    await open();
    fireEvent.click(submit());
    await flush();
    expect(exports).toEqual([{ id: 5, target: "repo" }]);
  });
});
