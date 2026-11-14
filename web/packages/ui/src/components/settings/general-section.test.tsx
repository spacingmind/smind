import { cleanup, fireEvent, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { GeneralSection } from "@/components/settings/general-section";
import { renderWithClientProp as render } from "@/test/render-with-daemon";
import { useGetStartedDismissedStore } from "@/stores/get-started-store";

describe("GeneralSection Get started guide", () => {
  beforeEach(() => {
    window.localStorage.clear();
    useGetStartedDismissedStore.getState().reload();
  });
  afterEach(() => {
    cleanup();
    window.localStorage.clear();
    useGetStartedDismissedStore.getState().reload();
  });

  it("Show guide clears a stored dismissal", () => {
    useGetStartedDismissedStore.getState().set(true);
    render(<GeneralSection client={null} />);
    fireEvent.click(screen.getByRole("button", { name: "Show guide" }));
    expect(useGetStartedDismissedStore.getState().value).toBe(false);
    expect(window.localStorage.getItem("smind:get-started-dismissed")).toBeNull();
  });
});
