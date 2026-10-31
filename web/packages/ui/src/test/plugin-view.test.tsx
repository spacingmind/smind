import { readdirSync, readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { PluginView } from "@/components/plugin-view/plugin-view";
import type { PluginViewDoc } from "@/lib/plugin-view-types";

const HERE = dirname(fileURLToPath(import.meta.url));
// The daemon's fixture suite (internal/plugins/uiview/testdata/fixtures): the
// same documents the Go validator is tested against, so the renderer can
// never drift from the schema.
const FIXTURES_DIR = join(HERE, "..", "..", "..", "..", "..", "internal", "plugins", "uiview", "testdata", "fixtures");

type Fixture = {
  name: string;
  env: { rpcs: string[]; panels: string[] };
  doc: unknown;
  expect: { valid: true; normalized: PluginViewDoc } | { valid: false; invalid: string };
};

function loadFixtures(): Fixture[] {
  const files = readdirSync(FIXTURES_DIR).filter((f) => f.endsWith(".json")).sort();
  expect(files.length, "fixture files in the daemon's testdata").toBeGreaterThanOrEqual(30);
  return files.map((f) => JSON.parse(readFileSync(join(FIXTURES_DIR, f), "utf-8")) as Fixture);
}

const FIXTURES = loadFixtures();

/** A valid fixture's normalised document (indexed loosely in these tests). */
function norm(name: string): any {
  const fx = FIXTURES.find((f) => f.name === name);
  if (!fx || !fx.expect.valid) throw new Error(`no valid fixture named ${name}`);
  return fx.expect.normalized;
}
function renderFixture(name: string) {
  const fx = FIXTURES.find((f) => f.name === name);
  if (!fx) throw new Error(`missing fixture ${name}`);
  const doc = fx.expect.valid ? fx.expect.normalized : fx.doc;
  return render(<PluginView doc={doc} onAction={vi.fn()} />);
}

describe("PluginView", () => {
  it("renders every shared fixture without throwing", () => {
    expect(FIXTURES.length).toBeGreaterThanOrEqual(30);
    for (const fx of FIXTURES) {
      const { container } = render(<PluginView doc={fx.expect.valid ? fx.expect.normalized : fx.doc} onAction={vi.fn()} />);
      expect(container.querySelector('[data-testid="plugin-view"]'), fx.name).not.toBeNull();
    }
  });

  const GOLDEN: Record<string, string[]> = {
    stack: ["Overview", "Two nodes stacked vertically."],
    heading: ["Release 1.2"],
    text: ["Small muted footnote."],
    badge: ["beta"],
    button: ["Insert summary"],
    list: ["Re-run suite", "pytest -q", "fast", "Open logs"],
    keyValue: ["Branch", "main", "Coverage", "82%"],
    table: ["File", "Lines", "a.go", "120", "Yes", "No"],
    code: ["fmt.Println"],
    diff: ["+new", "-old"],
    progress: ["Coverage"],
    form: ["Title", "Severity", "API token", "File report"],
    empty: ["Nothing to review", "Merge main to see changes."],
    error: ["Panel failed", "The plugin process is not running."],
    "actions-all": ["Refresh", "Ask", "Command", "Panel", "Open"],
    "tones-all": ["success", "warning", "danger", "muted"],
    row: ["beta", "Ask"],
    markdown: ["Some", "bold", "docs", "one", "two"],
  };

  it.each(Object.entries(GOLDEN))("golden text for fixture %s", (name, strings) => {
    const { container } = renderFixture(name);
    for (const s of strings) {
      expect(container.textContent, `${name}: ${s}`).toContain(s);
    }
  });

  it("unknown-node-placeholder", () => {
    const { container } = render(
      <PluginView
        doc={{ ui: 1, root: { type: "stack", children: [{ type: "text", text: "kept" }, { type: "hologram", text: "x" }] } }}
        onAction={vi.fn()}
      />,
    );
    expect(container.textContent).toContain("kept");
    const placeholders = container.querySelectorAll('[data-testid="plugin-unsupported-node"]');
    expect(placeholders).toHaveLength(1);
    expect(placeholders[0]).toHaveTextContent("Unsupported in this app version");
    expect(placeholders[0].getAttribute("data-node-type")).toBe("hologram");
    expect(container.textContent).not.toContain("hologram");

    const ui2 = render(<PluginView doc={{ ui: 2, root: { type: "text", text: "x" } }} onAction={vi.fn()} />);
    expect(ui2.container.querySelectorAll('[data-testid="plugin-unsupported-node"]')).toHaveLength(1);

    const garbage = render(<PluginView doc="garbage" onAction={vi.fn()} />);
    expect(garbage.container.querySelectorAll('[data-testid="plugin-unsupported-node"]')).toHaveLength(1);

    const nil = render(<PluginView doc={null} onAction={vi.fn()} />);
    expect(nil.container.querySelectorAll('[data-testid="plugin-unsupported-node"]')).toHaveLength(1);
  });

  it("markdown-strips-raw-html", () => {
    const { container } = renderFixture("markdown");
    expect(container.querySelector("script, img, iframe, b")).toBeNull();
    expect(container.querySelector("strong")).not.toBeNull();
    expect(container.textContent).toContain("alert(1)");
    // The only interactive element is the safe http(s) link; the
    // javascript: link renders as bare text, no <a> and no button.
    const buttons = container.querySelectorAll("button");
    expect(buttons).toHaveLength(1);
    expect(buttons[0]!.textContent).toBe("docs");
    expect(container.querySelector("a")).toBeNull();
  });

  it("remote-image-not-fetched", () => {
    const { container } = render(
      <PluginView doc={{ ui: 1, root: { type: "markdown", text: "![logo](https://evil.example/p.png) after" } }} onAction={vi.fn()} />,
    );
    expect(container.querySelector("img")).toBeNull();
    expect(container.querySelector("[src]")).toBeNull();
    expect(container.textContent).toContain("logo");
    expect(container.textContent).toContain("after");
  });

  it("button click calls onAction with the action", () => {
    const onAction = vi.fn();
    render(<PluginView doc={norm("actions-all")} onAction={onAction} />);
    const children: { label: string; action: unknown }[] = norm("actions-all").root.children;
    expect(children).toHaveLength(5);
    for (const child of children) {
      fireEvent.click(screen.getByRole("button", { name: child.label }));
      expect(onAction).toHaveBeenCalledWith(child.action);
    }
    expect(onAction).toHaveBeenCalledTimes(5);
  });

  it("list row with action is clickable", () => {
    const onAction = vi.fn();
    render(<PluginView doc={norm("list")} onAction={onAction} />);
    fireEvent.click(screen.getByRole("button", { name: /Re-run suite/ }));
    expect(onAction).toHaveBeenCalledWith(norm("list").root.items[0].action);
  });

  it("form submit passes values and is blocked until required fields are filled", () => {
    const onAction = vi.fn();
    render(<PluginView doc={norm("form")} onAction={onAction} />);
    const submit = screen.getByRole("button", { name: "File report" });
    expect(submit).toBeDisabled();
    fireEvent.change(screen.getByLabelText("Title"), { target: { value: "Hello" } });
    fireEvent.change(screen.getByLabelText("API token"), { target: { value: "s3" } });
    expect(submit).toBeEnabled();
    fireEvent.submit(screen.getByTestId("plugin-form-report"));
    expect(onAction).toHaveBeenCalledWith(
      { kind: "rpc", rpc: "refresh", params: { form: "report" } },
      { formValues: { title: "Hello", severity: "low", notify: false, token: "s3" } },
    );
  });

  it("markdown link opens through onAction as openUrl", () => {
    const onAction = vi.fn();
    render(<PluginView doc={{ ui: 1, root: { type: "markdown", text: "[docs](https://example.com/x)" } }} onAction={onAction} />);
    fireEvent.click(screen.getByRole("button", { name: "docs" }));
    expect(onAction).toHaveBeenCalledWith({ kind: "openUrl", url: "https://example.com/x", host: "example.com" });
  });

  it("svg icon is rebuilt from an allowlist", () => {
    const { container } = render(
      <PluginView
        doc={{
          ui: 1,
          root: {
            type: "badge",
            text: "x",
            icon: {
              svg: '<svg viewBox="0 0 24 24" onload="alert(1)"><script>alert(1)</script><path d="M0 0h24" stroke="red" onclick="x()"/><foreignObject><div/></foreignObject></svg>',
            },
          },
        }}
        onAction={vi.fn()}
      />,
    );
    const svgs = container.querySelectorAll("svg");
    expect(svgs).toHaveLength(1);
    expect(svgs[0]!.querySelector("path")).not.toBeNull();
    expect(container.querySelector("script")).toBeNull();
    expect(container.querySelector("foreignObject")).toBeNull();
    for (const el of Array.from(container.querySelectorAll("*"))) {
      for (const attr of Array.from(el.attributes)) {
        expect(`${el.nodeName} ${attr.name}`).not.toMatch(/\bon\w+$/);
      }
    }
    expect(svgs[0]!.getAttribute("onload")).toBeNull();
  });
});
