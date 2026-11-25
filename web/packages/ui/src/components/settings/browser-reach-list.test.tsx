import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { BrowserReachList } from "@/components/settings/browser-reach-list";
import type { BrowserProviderReach } from "@/lib/browser-settings-types";

const WHY_CONDITIONAL = "The harness may be too old for the browser tool.";
const WHY_NO = "This agent only reads tool output as text.";

// The daemon's reach table, as `browser.settings.get` reports it: one entry
// per reach and one of each screenshots capability, so the counts and the
// statements below are unique and assertable.
const providers: BrowserProviderReach[] = [
  { id: "opencode", label: "OpenCode", reach: "yes", images: "yes" },
  { id: "aider", label: "Aider", reach: "conditional", why: WHY_CONDITIONAL, images: "unknown" },
  { id: "gemini-cli", label: "Gemini CLI", reach: "conditional", why: "Another harness may be too old.", images: "no" },
  { id: "cursor", label: "Cursor", reach: "no", why: WHY_NO, images: "no" },
];

describe("browser reach list", () => {
  it("renders the three groups in order with counts", () => {
    render(<BrowserReachList providers={providers} />);

    expect(screen.getAllByRole("heading").map((heading) => heading.textContent)).toEqual([
      "Browser tools offered (1)",
      "Offered, not guaranteed (2)",
      "No browser for these agents (1)",
    ]);
    expect(screen.getByTestId("browser-reach-yes")).toBeInTheDocument();
    expect(screen.getByTestId("browser-reach-conditional")).toBeInTheDocument();
    expect(screen.getByTestId("browser-reach-no")).toBeInTheDocument();
  });

  it("shows the daemon's why for no and conditional entries verbatim", () => {
    render(<BrowserReachList providers={providers} />);

    expect(screen.getByText(WHY_CONDITIONAL)).toBeInTheDocument();
    expect(screen.getByText("Another harness may be too old.")).toBeInTheDocument();
    expect(screen.getByText(WHY_NO)).toBeInTheDocument();
  });

  it("states screenshots from images for every entry that gets the browser, and not for one that does not", () => {
    render(<BrowserReachList providers={providers} />);

    expect(screen.getAllByText("Screenshots: yes")).toHaveLength(1);
    expect(screen.getAllByText("Screenshots: unknown, text tools only")).toHaveLength(1);
    expect(screen.getAllByText("Screenshots: no, text tools only")).toHaveLength(1);
    expect(screen.getAllByText(/^Screenshots:/)).toHaveLength(providers.length - 1);
    expect(within(screen.getByTestId("browser-reach-no")).queryByText(/Screenshots:/)).toBeNull();
  });

  it("does not render an empty group", () => {
    render(<BrowserReachList providers={[providers[0], providers[3]]} />);

    expect(screen.queryByTestId("browser-reach-conditional")).toBeNull();
    expect(screen.queryByText(/Offered, not guaranteed/)).toBeNull();
    expect(screen.getByText("Browser tools offered (1)")).toBeInTheDocument();
    expect(screen.getByText("No browser for these agents (1)")).toBeInTheDocument();
  });

  it("renders a neutral line when the list is empty", () => {
    render(<BrowserReachList providers={[]} />);

    expect(screen.getByTestId("browser-reach-empty")).toHaveTextContent(
      "The daemon did not report which agents get the browser.",
    );
    expect(screen.queryByTestId("browser-reach-yes")).toBeNull();
  });

  it("renders whatever providers it is given (no hard-coded names)", () => {
    const custom: BrowserProviderReach[] = [
      { id: "zz-one", label: "ZZ One", reach: "yes", images: "unknown" },
      { id: "zz-two", label: "ZZ Two", reach: "no", why: "The daemon has no browser for this one.", images: "no" },
    ];
    render(<BrowserReachList providers={custom} />);

    expect(screen.getByText("ZZ One")).toBeInTheDocument();
    expect(screen.getByText("ZZ Two")).toBeInTheDocument();
    // Exactly the two given entries, and no provider name the component
    // might have baked in (Codex, Claude, ...).
    expect(screen.getAllByRole("listitem")).toHaveLength(2);
    expect(document.body.textContent).not.toMatch(/codex|claude/i);
  });
});
