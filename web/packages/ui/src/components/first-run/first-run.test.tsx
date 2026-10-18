import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { Connection, DaemonProgress, DaemonStatus, ProbeResult } from "@/lib/platform";

const desk = vi.hoisted(() => ({
  daemonStatus: vi.fn(),
  daemonInstall: vi.fn(),
  onDaemonProgress: vi.fn(),
  appSettingsSet: vi.fn(),
  selectConnection: vi.fn(),
  probeConnection: vi.fn(),
  addConnection: vi.fn(),
  addSshConnection: vi.fn(),
  previewRelayLink: vi.fn(),
  addRelayConnection: vi.fn(),
}));

vi.mock("@/lib/platform", async (orig) => ({
  ...(await orig<typeof import("@/lib/platform")>()),
  isDesktop: true,
  desktop: desk,
}));

import { FirstRun } from "@/components/first-run/first-run";

const FREE: DaemonStatus = {
  platform: "macos",
  reachable: false,
  daemonVersion: null,
  appVersion: "0.12.0",
  comparison: "unknown",
  managedState: "notRunning",
  pid: null,
  installedPath: null,
  logPath: null,
  binaryInstalled: false,
  localService: null,
  portOccupied: false,
  wslDaemonSuspected: false,
  gitFound: null,
  prerequisiteMessage: null,
};
const RUNNING_SMIND: DaemonStatus = { ...FREE, reachable: true, daemonVersion: "0.11.1", managedState: "unmanaged", localService: "smind" };
const NEW_CONN: Connection = { id: "url:h", kind: "url", label: "h", baseUrl: "http://h:4648" };
const PROBE_OK: ProbeResult = { reachable: true, version: "0.12.0", service: "smind", authRequired: false, loopback: false, error: null };

async function renderFirstRun(status: DaemonStatus | Error = FREE, props: Partial<Parameters<typeof FirstRun>[0]> = {}) {
  desk.daemonStatus.mockImplementation(() => (status instanceof Error ? Promise.reject(status) : Promise.resolve(status)));
  const onDone = vi.fn();
  const onSkip = vi.fn();
  render(<FirstRun onDone={onDone} onSkip={onSkip} pollIntervalMs={1} pollTimeoutMs={200} {...props} />);
  await screen.findByTestId("first-run-choices");
  return { onDone, onSkip };
}

const radio = (id: string) => screen.getByTestId(`first-run-radio-${id}`);
const checked = (id: string) => radio(id).getAttribute("aria-checked") === "true";

async function pick(id: string) {
  await act(async () => {
    fireEvent.click(radio(id));
  });
}

async function click(el: HTMLElement) {
  await act(async () => {
    fireEvent.click(el);
  });
}

async function type(el: HTMLElement, value: string) {
  await act(async () => {
    fireEvent.change(el, { target: { value } });
  });
}

beforeEach(() => {
  vi.resetAllMocks();
  desk.onDaemonProgress.mockReturnValue(() => {});
  desk.appSettingsSet.mockResolvedValue({ onboardingCompleted: true, manageLocalDaemon: false });
  desk.selectConnection.mockResolvedValue(NEW_CONN);
  desk.daemonInstall.mockResolvedValue({ ...FREE, reachable: true, managedState: "managed", localService: "smind" });
});

describe("first-run branding and copy", () => {
  it("leads with the sMind mark and title", async () => {
    await renderFirstRun();
    expect(screen.getByTestId("first-run-logo").querySelector("img")?.getAttribute("src")).toBe("/logo.png");
    expect(screen.getByTestId("first-run-title")).toHaveTextContent("Welcome to sMind");
    expect(screen.getByTestId("first-run-choice-run")).toHaveTextContent("Run sMind on this computer");
  });

  it("names the service manager when a service owns the running daemon", async () => {
    await renderFirstRun({ ...RUNNING_SMIND, managedBy: "systemd" });
    expect(screen.getByTestId("first-run-choice-existing")).toHaveTextContent("managed by systemd");
    expect(screen.getByTestId("first-run-choice-existing")).not.toHaveTextContent("started outside the app");
  });

  it("says managed by Docker for a containerised daemon", async () => {
    await renderFirstRun({ ...RUNNING_SMIND, managedBy: "docker" });
    expect(screen.getByTestId("first-run-choice-existing")).toHaveTextContent("managed by Docker");
  });

  it("labels the daemon password field", async () => {
    desk.probeConnection.mockResolvedValue({ ...PROBE_OK, authRequired: true });
    await renderFirstRun();
    await pick("url");
    await type(screen.getByTestId("connection-add-url"), "http://h:4648");
    await click(screen.getByTestId("connection-add-submit"));
    expect(await screen.findByLabelText("Daemon password")).toBeInTheDocument();
    expect(screen.getByText("Daemon password", { selector: "label" })).toBeVisible();
    expect(screen.getByTestId("connection-add-submit")).toBeEnabled();
  });
});

describe("first-run local situation", () => {
  it("preselects Run here when the port is free", async () => {
    await renderFirstRun();
    expect(checked("run")).toBe(true);
    expect(screen.getByTestId("first-run-run")).toBeEnabled();
    expect(screen.queryByTestId("first-run-radio-existing")).toBeNull();
  });

  it("preselects Use the running daemon and disables Run here when a sMind daemon answers", async () => {
    await renderFirstRun(RUNNING_SMIND);
    expect(checked("existing")).toBe(true);
    expect(radio("run")).toBeDisabled();
    expect(screen.getByTestId("first-run-reason-run")).toHaveTextContent(/already running/i);
    expect(screen.getByTestId("first-run-choice-existing")).toHaveTextContent("sMind v0.11.1");
    expect(screen.getByTestId("first-run-choice-existing")).toHaveTextContent("started outside the app");
    // (d) is listed first.
    const choices = screen.getByTestId("first-run-choices");
    expect((choices.firstElementChild as HTMLElement).dataset.testid).toBe("first-run-choice-existing");
  });

  it("disables Run here with a port-conflict message when something else owns the port", async () => {
    await renderFirstRun({ ...FREE, reachable: true, localService: null, managedState: "unmanaged" });
    expect(["run", "existing", "url", "ssh", "relay"].some((id) => screen.queryByTestId(`first-run-radio-${id}`) && checked(id))).toBe(false);
    expect(radio("run")).toBeDisabled();
    expect(screen.getByTestId("first-run-reason-run")).toHaveTextContent(/not smind/i);
    expect(screen.queryByTestId("first-run-radio-existing")).toBeNull();
  });

  it("shows guidance instead of Run here on Linux", async () => {
    await renderFirstRun({ ...FREE, platform: "unsupported" });
    expect(screen.queryByTestId("first-run-radio-run")).toBeNull();
    expect(screen.getByTestId("first-run-linux-guidance")).toHaveTextContent("smind service install");
  });

  it("treats a daemon_status failure as unknown and still offers Run here", async () => {
    await renderFirstRun(new Error("boom"));
    expect(radio("run")).toBeEnabled();
    expect(checked("run")).toBe(false);
  });
});

describe("first-run actions", () => {
  it("Skip for now installs nothing and leaves first-run pending", async () => {
    const { onSkip, onDone } = await renderFirstRun();
    await click(screen.getByTestId("first-run-skip"));
    expect(onSkip).toHaveBeenCalledTimes(1);
    expect(onDone).not.toHaveBeenCalled();
    expect(desk.daemonInstall).not.toHaveBeenCalled();
    expect(desk.appSettingsSet).not.toHaveBeenCalled();
    expect(desk.selectConnection).not.toHaveBeenCalled();
  });

  it("Run here calls daemonInstall once, waits for healthz, then enters the app", async () => {
    let progress: (p: DaemonProgress) => void = () => {};
    desk.onDaemonProgress.mockImplementation((cb: (p: DaemonProgress) => void) => {
      progress = cb;
      return () => {};
    });
    const { onDone } = await renderFirstRun();
    desk.daemonInstall.mockResolvedValue({ ...FREE, reachable: false, managedState: "managed" });
    // Not answering on the first two polls, then healthy.
    desk.daemonStatus.mockResolvedValueOnce({ ...FREE }).mockResolvedValueOnce({ ...FREE }).mockResolvedValue({ ...FREE, reachable: true });

    await click(screen.getByTestId("first-run-run"));
    await act(async () => {
      progress({ stage: "downloading", message: "Downloading smind…" });
    });
    await vi.waitFor(() => expect(onDone).toHaveBeenCalledTimes(1));

    expect(desk.daemonInstall).toHaveBeenCalledTimes(1);
    expect(desk.appSettingsSet.mock.calls).toEqual([[{ manageLocalDaemon: true }], [{ onboardingCompleted: true }]]);
    expect(desk.selectConnection).toHaveBeenCalledWith("local");
    // Entered only after the health poll succeeded.
    expect(desk.daemonStatus.mock.calls.length).toBeGreaterThanOrEqual(3);
    expect(desk.appSettingsSet.mock.invocationCallOrder[1]).toBeGreaterThan(desk.daemonStatus.mock.invocationCallOrder[desk.daemonStatus.mock.calls.length - 1]);
  });

  it("an install failure keeps the screen and the other choices usable", async () => {
    const { onDone } = await renderFirstRun();
    desk.daemonInstall.mockRejectedValueOnce(new Error("download failed: 503"));
    await click(screen.getByTestId("first-run-run"));

    expect(await screen.findByTestId("first-run-error")).toHaveTextContent("download failed: 503");
    expect(onDone).not.toHaveBeenCalled();
    expect(screen.getByTestId("first-run-run")).toHaveTextContent("Retry");
    expect(screen.getByTestId("first-run-run")).toBeEnabled();
    expect(radio("url")).toBeEnabled();
    expect(radio("ssh")).toBeEnabled();
    await pick("url");
    expect(screen.getByTestId("url-form")).toBeInTheDocument();
    // onboarding was never marked complete
    expect(desk.appSettingsSet).not.toHaveBeenCalledWith(expect.objectContaining({ onboardingCompleted: true }));
  });

  it("Use running daemon sets manageLocalDaemon=false and never calls daemonInstall", async () => {
    const { onDone } = await renderFirstRun(RUNNING_SMIND);
    await click(screen.getByTestId("first-run-existing-connect"));
    await vi.waitFor(() => expect(onDone).toHaveBeenCalledTimes(1));
    expect(desk.appSettingsSet).toHaveBeenCalledWith({ manageLocalDaemon: false, onboardingCompleted: true });
    expect(desk.selectConnection).toHaveBeenCalledWith("local");
    expect(desk.daemonInstall).not.toHaveBeenCalled();
  });
});

describe("first-run connect by URL", () => {
  async function openUrl() {
    const out = await renderFirstRun();
    await pick("url");
    return out;
  }

  it("Connect by URL probes before saving and shows the version", async () => {
    desk.probeConnection.mockResolvedValue(PROBE_OK);
    desk.addConnection.mockResolvedValue(NEW_CONN);
    const { onDone } = await openUrl();

    await type(screen.getByTestId("connection-add-url"), "http://h:4648");
    await click(screen.getByTestId("connection-add-submit"));
    expect(desk.probeConnection).toHaveBeenCalledWith("http://h:4648");
    expect(await screen.findByTestId("url-probe-ok")).toHaveTextContent("Reachable, v0.12.0");
    expect(desk.addConnection).not.toHaveBeenCalled();

    await click(screen.getByTestId("connection-add-submit"));
    await vi.waitFor(() => expect(onDone).toHaveBeenCalledTimes(1));
    expect(desk.addConnection).toHaveBeenCalledWith("", "http://h:4648", undefined);
    expect(desk.selectConnection).toHaveBeenCalledWith("url:h");
    expect(desk.appSettingsSet).toHaveBeenCalledWith({ manageLocalDaemon: false, onboardingCompleted: true });
    expect(desk.daemonInstall).not.toHaveBeenCalled();
  });

  it("asks for a password when the probe says authRequired and never stores it", async () => {
    desk.probeConnection.mockResolvedValue({ ...PROBE_OK, authRequired: true });
    desk.addConnection.mockRejectedValueOnce(new Error("wrong password")).mockResolvedValue(NEW_CONN);
    const { onDone } = await openUrl();

    await type(screen.getByTestId("connection-add-url"), "http://h:4648");
    await click(screen.getByTestId("connection-add-submit"));
    const password = (await screen.findByTestId("url-password")) as HTMLInputElement;
    expect(password.type).toBe("password");
    expect(password.getAttribute("autocomplete")).toBe("off");

    password.value = "hunter2";
    await click(screen.getByTestId("connection-add-submit"));
    expect(desk.addConnection).toHaveBeenLastCalledWith("", "http://h:4648", "hunter2");
    expect(await screen.findByText("wrong password")).toBeInTheDocument();
    // Cleared after the submit, and nowhere in the document.
    expect(password.value).toBe("");
    expect(document.body.innerHTML).not.toContain("hunter2");
    expect(onDone).not.toHaveBeenCalled();

    password.value = "hunter2";
    await click(screen.getByTestId("connection-add-submit"));
    await vi.waitFor(() => expect(onDone).toHaveBeenCalledTimes(1));
    expect(password.value).toBe("");
  });

  it("refuses a URL that is not a sMind daemon", async () => {
    desk.probeConnection.mockResolvedValue({ ...PROBE_OK, service: "nginx", version: null });
    await openUrl();
    await type(screen.getByTestId("connection-add-url"), "http://h:8080");
    await click(screen.getByTestId("connection-add-submit"));
    expect(await screen.findByTestId("url-not-smind")).toHaveTextContent(/not a sMind daemon/i);
    expect(screen.getByTestId("connection-add-submit")).toHaveTextContent("Check");
    await click(screen.getByTestId("connection-add-submit"));
    expect(desk.addConnection).not.toHaveBeenCalled();
  });

  it("explains loopback-only daemons and points to SSH and relay", async () => {
    desk.probeConnection.mockResolvedValue({ reachable: false, version: null, service: null, authRequired: false, loopback: false, error: "connection refused" });
    await openUrl();
    await type(screen.getByTestId("connection-add-url"), "http://10.0.0.5:4648");
    await click(screen.getByTestId("connection-add-submit"));
    expect(await screen.findByTestId("url-loopback-note")).toHaveTextContent(/loopback/i);
    expect(screen.getByTestId("url-unreachable")).toHaveTextContent("connection refused");
    await click(screen.getByTestId("url-pick-ssh"));
    expect(await screen.findByTestId("ssh-form")).toBeInTheDocument();
    expect(desk.addConnection).not.toHaveBeenCalled();
  });

  it("hands a pasted relay pairing link to the relay flow instead of probing it", async () => {
    desk.previewRelayLink.mockResolvedValue({ relayAddress: "r:7400", relayHostId: "ws-1", keyFingerprint: "aaaa bbbb", pinned: true, kind: "newHost" });
    await openUrl();
    await type(screen.getByTestId("connection-add-url"), "https://x.example/pair#offer=abc");
    await click(screen.getByTestId("connection-add-submit"));
    expect(desk.probeConnection).not.toHaveBeenCalled();
    expect(await screen.findByTestId("relay-link-confirm")).toBeInTheDocument();
    expect(desk.previewRelayLink).toHaveBeenCalledWith("https://x.example/pair#offer=abc");
  });
});

describe("first-run SSH and relay", () => {
  it("Connect over SSH validates the host before spawning anything", async () => {
    desk.addSshConnection.mockRejectedValueOnce(new Error("ssh: Could not resolve hostname nope"));
    desk.addSshConnection.mockResolvedValue({ id: "ssh:homelab", kind: "ssh", label: "homelab", baseUrl: "http://127.0.0.1:50123" });
    const { onDone } = await renderFirstRun();
    await pick("ssh");

    await click(screen.getByTestId("ssh-submit"));
    expect(screen.getByTestId("ssh-error")).toHaveTextContent(/enter the host/i);
    await type(screen.getByTestId("ssh-host"), "-oProxyCommand=evil");
    await click(screen.getByTestId("ssh-submit"));
    expect(screen.getByTestId("ssh-error")).toHaveTextContent(/dash/i);
    await type(screen.getByTestId("ssh-host"), "my host");
    await click(screen.getByTestId("ssh-submit"));
    expect(screen.getByTestId("ssh-error")).toHaveTextContent(/spaces/i);
    await type(screen.getByTestId("ssh-host"), "homelab");
    await type(screen.getByTestId("ssh-port"), "70000");
    await click(screen.getByTestId("ssh-submit"));
    expect(screen.getByTestId("ssh-error")).toHaveTextContent(/ssh port/i);
    await type(screen.getByTestId("ssh-port"), "22");
    await type(screen.getByTestId("ssh-daemon-port"), "0");
    await click(screen.getByTestId("ssh-submit"));
    expect(screen.getByTestId("ssh-error")).toHaveTextContent(/daemon port/i);
    expect(desk.addSshConnection).not.toHaveBeenCalled();

    // Rust's rejection (ssh stderr) is shown inline.
    await type(screen.getByTestId("ssh-daemon-port"), "4648");
    await click(screen.getByTestId("ssh-submit"));
    expect(await screen.findByText("ssh: Could not resolve hostname nope")).toBeInTheDocument();
    expect(onDone).not.toHaveBeenCalled();

    await click(screen.getByTestId("ssh-submit"));
    await vi.waitFor(() => expect(onDone).toHaveBeenCalledTimes(1));
    expect(desk.addSshConnection).toHaveBeenLastCalledWith("homelab", { host: "homelab", sshPort: 22, daemonPort: 4648 });
    expect(desk.selectConnection).toHaveBeenCalledWith("ssh:homelab");
    expect(desk.appSettingsSet).toHaveBeenCalledWith({ manageLocalDaemon: false, onboardingCompleted: true });
  });

  it("Pair via relay link uses the shared AddRelayLink component", async () => {
    desk.previewRelayLink.mockResolvedValue({ relayAddress: "r:7400", relayHostId: "ws-1", keyFingerprint: "aaaa bbbb", pinned: true, kind: "newHost" });
    desk.addRelayConnection.mockResolvedValue({ id: "relay:ws-1", kind: "relay", label: "Relay (ws-1)", baseUrl: "relay://ws-1@r:7400" });
    const { onDone } = await renderFirstRun();
    await pick("relay");

    const form = screen.getByTestId("relay-link-form");
    expect(within(form).getByTestId("relay-link-input")).toHaveAttribute("type", "password");
    await type(screen.getByTestId("relay-link-input"), "https://x.example/pair#offer=abc");
    await click(screen.getByTestId("relay-link-review"));
    await click(await screen.findByTestId("relay-link-add"));
    await vi.waitFor(() => expect(onDone).toHaveBeenCalledTimes(1));
    expect(desk.addRelayConnection).toHaveBeenCalledWith("", "https://x.example/pair#offer=abc");
    expect(desk.selectConnection).toHaveBeenCalledWith("relay:ws-1");
    expect(desk.appSettingsSet).toHaveBeenCalledWith({ manageLocalDaemon: false, onboardingCompleted: true });
    expect(document.body.innerHTML).not.toContain("offer=abc");
  });
});
