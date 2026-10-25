import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { MiddleText } from "@/components/ui/middle-text";

describe("MiddleText", () => {
  it("splits a long name into a shrinking head and a fixed tail, full name as the title", () => {
    const { container } = render(<MiddleText text="smind/task-payments-rework-dmdo2typy7js" />);
    expect(container.querySelector('[data-part="head"]')).toHaveClass("truncate");
    expect(container.querySelector('[data-part="tail"]')).toHaveTextContent("-dmdo2typy7js".slice(-14));
    expect(container.firstElementChild).toHaveAttribute("title", "smind/task-payments-rework-dmdo2typy7js");
  });

  it("renders a short name whole", () => {
    const { container } = render(<MiddleText text="develop" />);
    expect(container.querySelector('[data-part="tail"]')).toBeNull();
    expect(container).toHaveTextContent("develop");
  });
});
