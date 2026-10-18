import { act, fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const desk = vi.hoisted(() => ({
  appSettingsGet: vi.fn(),
  appSettingsSet: vi.fn(),
  onRunSetupRequested: vi.fn(),
  daemonStatus: vi.fn(),
  daemonInstall: vi.fn(),
  onDaemonProgress: vi.fn(),
}));

vi.mock("@/lib/platform", async (orig) => ({
  ...(await orig<typeof import("@/lib/platform")>()),
  isDesktop: true,
  desktop: desk,
}));

import FirstRunGate from "@/components/first-run/first-run-gate";
import { SetupSection } from "@/components/first-run/setup-section";
import { requestFirstRun } from "@/components/first-run/first-run-events";

let runSetup: () => void = () => {};

beforeEach(() => {
  vi.resetAllMocks();
  desk.onRunSetupRequested.mockImplementation((cb: () => void) => {
    runSetup = cb;
    return () => {};
  });
  desk.onDaemonProgress.mockReturnValue(() => {});
  desk.daemonStatus.mockResolvedValue({ platform: "macos", reachable: false, managedState: "notRunning", localService: null, portOccupied: false, wslDaemonSuspected: false });
});

const mount = () =>
  render(
    <FirstRunGate>
      <div data-testid="the-app" />
    </FirstRunGate>,
  );

describe("first-run gate", () => {
  it("renders nothing while settings load, then the app when onboarding is done", async () => {
    let resolve: (v: unknown) => void = () => {};
    desk.appSettingsGet.mockReturnValue(new Promise((r) => (resolve = r)));
    const { container } = mount();
    expect(container).toBeEmptyDOMElement();
    await act(async () => resolve({ onboardingCompleted: true, manageLocalDaemon: true }));
    expect(screen.getByTestId("the-app")).toBeInTheDocument();
    expect(screen.queryByTestId("first-run")).toBeNull();
  });

  it("shows first-run when onboarding is pending, and Skip for now only dismisses it for the session", async () => {
    desk.appSettingsGet.mockResolvedValue({ onboardingCompleted: false, manageLocalDaemon: false });
    mount();
    expect(await screen.findByTestId("first-run")).toBeInTheDocument();
    expect(screen.queryByTestId("the-app")).toBeNull();
    await act(async () => {
      fireEvent.click(await screen.findByTestId("first-run-skip"));
    });
    expect(screen.getByTestId("the-app")).toBeInTheDocument();
    expect(desk.appSettingsSet).not.toHaveBeenCalled();
  });

  it("falls back to the normal app when the settings read fails", async () => {
    desk.appSettingsGet.mockRejectedValue(new Error("no such command"));
    mount();
    expect(await screen.findByTestId("the-app")).toBeInTheDocument();
  });

  it("reopens first-run from the Help menu or a Settings request", async () => {
    desk.appSettingsGet.mockResolvedValue({ onboardingCompleted: true, manageLocalDaemon: true });
    mount();
    await screen.findByTestId("the-app");
    await act(async () => runSetup());
    expect(await screen.findByTestId("first-run")).toBeInTheDocument();
    await act(async () => {
      fireEvent.click(await screen.findByTestId("first-run-skip"));
    });
    expect(screen.getByTestId("the-app")).toBeInTheDocument();
    await act(async () => requestFirstRun());
    expect(await screen.findByTestId("first-run")).toBeInTheDocument();
    expect(desk.appSettingsSet).not.toHaveBeenCalled();
  });

  it("a Skip for now session dismissal leaves the Settings Finish setup entry working", async () => {
    desk.appSettingsGet.mockResolvedValue({ onboardingCompleted: false, manageLocalDaemon: false });
    mount();
    await screen.findByTestId("first-run");
    await act(async () => {
      fireEvent.click(screen.getByTestId("first-run-skip"));
    });
    expect(screen.getByTestId("the-app")).toBeInTheDocument();
    render(<SetupSection />);
    const finish = await screen.findByText("Finish setup");
    await act(async () => {
      fireEvent.click(finish);
    });
    expect(await screen.findByTestId("first-run")).toBeInTheDocument();
    expect(desk.appSettingsSet).not.toHaveBeenCalled();
  });
});
