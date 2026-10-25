import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { ProbeContrastTable } from "@/components/learning/probe-contrast-table";
import { metric } from "@/test/probe-fixtures";

describe("ProbeContrastTable", () => {
  it("shows the replicate and noise columns when there is a noise floor", () => {
    render(<ProbeContrastTable noiseFloor metrics={[metric()]} />);
    expect(screen.getByTestId("probe-col-replicate")).toBeTruthy();
    expect(screen.getByTestId("probe-col-noise")).toBeTruthy();
    const row = screen.getByTestId("probe-metric");
    expect(row.textContent).toContain("Tool calls");
    expect(row.textContent).toContain("±1");
    expect(row.textContent).toContain("−6");
  });

  it("hides both columns without a noise floor", () => {
    render(<ProbeContrastTable noiseFloor={false} metrics={[metric({ a2: null, noise: null })]} />);
    expect(screen.queryByTestId("probe-col-replicate")).toBeNull();
    expect(screen.queryByTestId("probe-col-noise")).toBeNull();
    expect(screen.getByTestId("probe-metric").querySelectorAll("td")).toHaveLength(3);
  });

  it("marks a worse row with a word, not only a tint", () => {
    render(<ProbeContrastTable noiseFloor metrics={[metric(), metric({ key: "failedToolCalls", worse: true, deltaAB: 4, a: 1, b: 5 })]} />);
    const rows = screen.getAllByTestId("probe-metric");
    expect(rows[0]?.getAttribute("data-worse")).toBe("false");
    expect(rows[1]?.getAttribute("data-worse")).toBe("true");
    expect(rows[1]?.textContent).toContain("worse");
    expect(rows[0]?.textContent).not.toContain("worse");
  });

  it("formats each metric in its own unit and shows a missing replicate as a dash", () => {
    render(
      <ProbeContrastTable
        noiseFloor
        metrics={[
          metric({ key: "costUSD", a: 0.41, a2: null, b: 0.4, deltaAB: -0.01, noise: null }),
          metric({ key: "durationMs", a: 72_000, a2: 75_000, b: 64_000, deltaAB: -8000, noise: 3000 }),
        ]}
      />,
    );
    const [cost, duration] = screen.getAllByTestId("probe-metric");
    expect(cost?.textContent).toContain("$0.4100");
    expect(cost?.textContent).toContain("–");
    expect(duration?.textContent).toContain("1m 12s");
    expect(duration?.textContent).toContain("−8s");
  });
});

describe("ProbeContrastTable narrow layout", () => {
  it("never wraps a value inside a cell, so a narrow screen scrolls the table instead of breaking numbers", () => {
    render(<ProbeContrastTable noiseFloor metrics={[{ key: "costUSD", a: 0.09, a2: 0.08, b: 0.05, deltaAB: -0.04, noise: 0.01, worse: false }]} />);
    const cells = screen.getByTestId("probe-contrast").querySelectorAll("th, td");
    expect(cells.length).toBeGreaterThan(0);
    for (const c of cells) expect(c.className).toContain("whitespace-nowrap");
  });
});

describe("ProbeContrastTable stacked layout (below sm)", () => {
  const worse = metric({ key: "failedToolCalls", worse: true, deltaAB: 4, a: 1, a2: null, b: 5, noise: null });

  it("renders one block per metric, each with every column as a label/value pair, so nothing scrolls off screen", () => {
    render(<ProbeContrastTable noiseFloor metrics={[metric(), worse]} />);
    const blocks = screen.getAllByTestId("probe-metric-stacked");
    expect(blocks).toHaveLength(2);
    const labels = [...blocks[0]!.querySelectorAll("dt")].map((d) => d.textContent);
    expect(labels).toEqual(["Control", "Replicate", "With this skill", "Δ", "Noise"]);
    const values = [...blocks[0]!.querySelectorAll("dd")].map((d) => d.textContent);
    expect(values).toEqual(["20", "21", "14", "−6", "±1"]);
    expect(screen.getByTestId("probe-contrast-stacked").className).toContain("sm:hidden");
    expect(screen.getByTestId("probe-contrast").parentElement?.className).toContain("hidden");
  });

  it("drops Replicate and Noise for a quick probe and keeps the dash for a missing replicate", () => {
    render(<ProbeContrastTable noiseFloor={false} metrics={[metric({ a2: null, noise: null })]} />);
    const block = screen.getByTestId("probe-metric-stacked");
    expect([...block.querySelectorAll("dt")].map((d) => d.textContent)).toEqual(["Control", "With this skill", "Δ"]);
    render(<ProbeContrastTable noiseFloor metrics={[worse]} />);
    expect(screen.getAllByTestId("probe-metric-stacked")[1]?.textContent).toContain("–");
  });

  it("marks a worse metric with the word and the data attribute", () => {
    render(<ProbeContrastTable noiseFloor metrics={[metric(), worse]} />);
    const [ok, bad] = screen.getAllByTestId("probe-metric-stacked");
    expect(ok?.getAttribute("data-worse")).toBe("false");
    expect(ok?.textContent).not.toContain("worse");
    expect(bad?.getAttribute("data-worse")).toBe("true");
    expect(bad?.textContent).toContain("worse");
  });
});
