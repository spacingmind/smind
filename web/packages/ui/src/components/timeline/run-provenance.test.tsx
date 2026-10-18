import { describe, expect, it, vi, afterEach } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { fireEvent } from "@testing-library/react";

import { RunProvenance } from "@/components/timeline/run-provenance";

const BASE = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";
const HEAD = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb";

function mount(props: Partial<Parameters<typeof RunProvenance>[0]> = {}) {
  return render(
    <RunProvenance baseSha={BASE} headSha={HEAD} status="done" dirty={false} {...props} />,
  );
}

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

function mockClipboard() {
  const writeText = vi.fn().mockResolvedValue(undefined);
  Object.assign(navigator, { clipboard: { writeText } });
  return writeText;
}

describe("RunProvenance", () => {
  it("shows the 7-char head sha when the run is terminal", () => {
    mount();
    expect(screen.getByTestId("run-provenance").textContent).toContain("bbbbbbb");
  });

  it("prefers base while the run is still running", () => {
    mount({ status: "running" });
    expect(screen.getByTestId("run-provenance").textContent).toContain("aaaaaaa");
  });

  it("falls back to base when terminal with no head sha", () => {
    mount({ headSha: undefined });
    expect(screen.getByTestId("run-provenance").textContent).toContain("aaaaaaa");
  });

  it("marks dirty runs and stays quiet for clean ones", () => {
    const { rerender } = mount({ dirty: true });
    expect(screen.getByTestId("run-provenance").textContent).toContain("uncommitted changes");
    rerender(
      <RunProvenance baseSha={BASE} headSha={HEAD} status="done" dirty={false} />,
    );
    expect(screen.getByTestId("run-provenance").textContent).not.toContain("uncommitted changes");
  });

  it("lists both full shas and the dirty state in the tooltip", () => {
    mount({ dirty: true });
    expect(screen.getByTestId("run-provenance")).toHaveAttribute(
      "title",
      `Base ${BASE} / Head ${HEAD} / Uncommitted changes`,
    );
  });

  it("copies the full head sha on click", async () => {
    const writeText = mockClipboard();
    mount();
    fireEvent.click(screen.getByTestId("run-provenance"));
    expect(writeText).toHaveBeenCalledWith(HEAD);
  });

  it("copies the full base sha while running", async () => {
    const writeText = mockClipboard();
    mount({ status: "running" });
    fireEvent.click(screen.getByTestId("run-provenance"));
    expect(writeText).toHaveBeenCalledWith(BASE);
  });

  it("rerenders the same instance from no sha to a sha without a hooks error", () => {
    const { rerender } = mount({ baseSha: undefined, headSha: undefined });
    expect(screen.queryByTestId("run-provenance")).toBeNull();
    rerender(<RunProvenance baseSha={BASE} status="done" />);
    expect(screen.getByTestId("run-provenance").textContent).toContain("aaaaaaa");
  });

  it("renders nothing when neither sha is known", () => {
    mount({ baseSha: undefined, headSha: undefined });
    expect(screen.queryByTestId("run-provenance")).toBeNull();
  });
});
