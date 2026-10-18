import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { RunConfigOptions } from "@/components/run-config-options";
import { PILL_TRIGGER_CLASS } from "@/components/composer/pill-class";
import type { ConfigOptionParams } from "@/lib/types";

const MODE: ConfigOptionParams = {
  configId: "mode",
  name: "Mode",
  category: "mode",
  type: "select",
  currentValue: "default",
  options: [{ value: "default", name: "Ask for permission" }],
};
const MODEL: ConfigOptionParams = {
  configId: "model",
  name: "Model",
  category: "model",
  type: "select",
  currentValue: "glm-5.3",
  options: [{ value: "glm-5.3", name: "GLM-5.3" }],
};
const THINKING: ConfigOptionParams = {
  configId: "thinking",
  name: "Thinking",
  type: "select",
  currentValue: "max",
  options: [{ value: "max", name: "Max" }],
};
const TOGGLE: ConfigOptionParams = { configId: "fast", name: "Fast", type: "boolean", currentValue: false };

describe("RunConfigOptions", () => {
  it("live config options use the pill trigger and omit the mode option while the mode pill is live", () => {
    render(<RunConfigOptions options={[MODE, THINKING, TOGGLE]} error={null} onSetOption={vi.fn()} omitMode />);
    expect(screen.queryByLabelText("Mode")).not.toBeInTheDocument();
    const thinking = screen.getByLabelText("Thinking");
    expect(thinking.className).toContain(PILL_TRIGGER_CLASS.split(" ")[0]);
    expect(thinking.className).toContain("md:h-7");
    expect(screen.getByTestId("run-config-option-toggle").className).toContain("md:h-7");
  });

  it("live thinking options are the toolbar's shared pill select: named by the option, choices from the agent", async () => {
    const onSetOption = vi.fn().mockResolvedValue(undefined);
    const option: ConfigOptionParams = {
      ...THINKING,
      description: "How hard the agent thinks",
      options: [
        { value: "max", name: "Max", description: "Think the most" },
        { value: "low", name: "Low" },
      ],
    };
    render(<RunConfigOptions options={[option]} error={null} onSetOption={onSetOption} />);
    const trigger = screen.getByLabelText("Thinking");
    // Same component as the composer's Thinking pill (ThinkingSelect): its accessible name is the option's own,
    // its tooltip the option's description.
    expect(trigger).toHaveAttribute("title", "How hard the agent thinks");
    fireEvent.click(trigger);
    fireEvent.click(await screen.findByRole("option", { name: "Low" }));
    expect(onSetOption).toHaveBeenCalledWith("thinking", "low");
  });

  it("keeps the mode option when the mode pill is not live", () => {
    render(<RunConfigOptions options={[MODE]} error={null} onSetOption={vi.fn()} />);
    expect(screen.getByLabelText("Mode")).toBeInTheDocument();
  });

  it("the live model option is labelled This run and keeps the ADR-0025 aria-label", () => {
    render(<RunConfigOptions options={[MODEL]} error={null} onSetOption={vi.fn()} omitMode />);
    const model = screen.getByTestId("run-config-option-model");
    expect(model).toHaveAccessibleName("Model (this session)");
    expect(model).toHaveTextContent("This run: GLM-5.3");
  });

  it("a boolean option toggles through onSetOption", () => {
    const onSetOption = vi.fn().mockResolvedValue(undefined);
    render(<RunConfigOptions options={[TOGGLE]} error={null} onSetOption={onSetOption} />);
    fireEvent.click(screen.getByTestId("run-config-option-toggle"));
    expect(onSetOption).toHaveBeenCalledWith("fast", "true");
  });
});
