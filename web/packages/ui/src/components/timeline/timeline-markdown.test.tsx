import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { TimelineMarkdown } from "@/components/timeline/timeline-markdown";

// The element styling lives in child-selector classes on the wrapper (no
// `prose` plugin in this project), so these assert the wrapper's own
// className rather than inspecting parsed markdown output.
describe("TimelineMarkdown styling", () => {
  it("inline code is a chip", () => {
    render(<TimelineMarkdown content="use `x` here" />);
    const wrapper = screen.getByTestId("timeline-markdown");
    expect(wrapper.className).toContain("[&_code]:rounded-md");
    expect(wrapper.className).toContain("[&_code]:bg-surface");
    expect(wrapper.className).toContain("[&_code]:px-1.5");
    expect(wrapper.className).toContain("[&_code]:py-0.5");
  });

  it("headings keep the type scale", () => {
    render(<TimelineMarkdown content="# Title" />);
    const wrapper = screen.getByTestId("timeline-markdown");
    expect(wrapper.className).toContain("[&_h1]:text-ui-xl");
    expect(wrapper.className).toContain("[&_h2]:text-ui-lg");
    expect(wrapper.className).toContain("[&_h3]:text-ui-base");
  });
});
