import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { FileIcon, fileIconKey } from "@/lib/file-icons";

describe("fileIconKey", () => {
  it("resolves a known extension, case-insensitively and off the last dot", () => {
    expect(fileIconKey("internal/workspace/git.go")).toBe("go");
    expect(fileIconKey("src/App.TSX")).toBe("code");
    expect(fileIconKey("docs/plans/active/ui.min.md")).toBe("markdown");
  });

  it("falls back to the generic file icon for an unknown extension", () => {
    expect(fileIconKey("weird/thing.qqq")).toBe("file");
    expect(fileIconKey("Makefile.custom")).toBe("file");
  });

  it("matches whole filenames that have no extension, or whose extension lies", () => {
    expect(fileIconKey("Dockerfile")).toBe("config");
    expect(fileIconKey("web/bun.lock")).toBe("lock");
    // A leading dot is part of the name, not an extension.
    expect(fileIconKey(".gitignore")).toBe("config");
    expect(fileIconKey("some/dir/.gitignore")).toBe("config");
  });

  it("has no extension to read at all", () => {
    expect(fileIconKey("LICENSE")).toBe("text");
    expect(fileIconKey("bin/smind")).toBe("file");
  });
});

describe("FileIcon", () => {
  it("renders the resolved key as data-icon, and hides itself from the accessibility tree", () => {
    render(<FileIcon path="cmd/smind/main.go" />);
    const icon = screen.getByTestId("file-icon");
    expect(icon).toHaveAttribute("data-icon", "go");
    expect(icon).toHaveAttribute("aria-hidden", "true");
  });

  it("renders the generic icon for an unknown extension", () => {
    render(<FileIcon path="notes.qqq" />);
    expect(screen.getByTestId("file-icon")).toHaveAttribute("data-icon", "file");
  });
});
