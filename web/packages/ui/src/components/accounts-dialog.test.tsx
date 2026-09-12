import { act } from "react";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { AccountsDialog } from "@/components/accounts-dialog";
import { FakeWsClient } from "@/test/fake-ws-client";

const ACCOUNTS = [
  {
    id: 1,
    provider: "anthropic",
    label: "main",
    credentialType: "oauth",
    createdAt: "2024-01-01T00:00:00Z",
    updatedAt: "2024-01-01T00:00:00Z",
  },
];

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

function renderDialog(client: FakeWsClient) {
  render(<AccountsDialog client={client as never} open onOpenChange={() => {}} />);
}

/** The manual-paste form is collapsed behind a disclosure by default -- open it before interacting with its fields. */
async function openManualForm(): Promise<void> {
  fireEvent.click(await screen.findByRole("button", { name: /Paste a credential instead/ }));
}

describe("AccountsDialog", () => {
  it("lists accounts from account.list", async () => {
    const client = new FakeWsClient();
    renderDialog(client);

    client.nth("account.list").resolve(ACCOUNTS);
    await flush();

    expect(await screen.findByText("main")).toBeInTheDocument();
    expect(screen.getByText(/Anthropic \(Claude\) · oauth/)).toBeInTheDocument();
  });

  it("shows the empty hint when there are no accounts", async () => {
    const client = new FakeWsClient();
    renderDialog(client);

    client.nth("account.list").resolve([]);
    await flush();

    expect(await screen.findByText(/No accounts yet/)).toBeInTheDocument();
  });

  it("manual-paste form is collapsed until its disclosure is opened", async () => {
    const client = new FakeWsClient();
    renderDialog(client);

    client.nth("account.list").resolve([]);
    await flush();

    expect(screen.queryByLabelText("Provider")).not.toBeInTheDocument();
    const toggle = await screen.findByRole("button", { name: /Paste a credential instead/ });
    expect(toggle).toHaveAttribute("aria-expanded", "false");

    fireEvent.click(toggle);

    expect(await screen.findByLabelText("Provider")).toBeInTheDocument();
    expect(toggle).toHaveAttribute("aria-expanded", "true");
  });

  it("manual-add provider dropdown offers account-credential provider IDs, not task-execution ones", async () => {
    const client = new FakeWsClient();
    renderDialog(client);

    client.nth("account.list").resolve([]);
    await flush();
    // The dialog does fetch provider.list (for the separate "managed
    // externally" row -- see below), but must never feed its result into
    // this dropdown.
    client.nth("provider.list").resolve({ providers: [{ id: "glm", label: "GLM", kind: "cli" }] });
    await flush();
    await openManualForm();

    fireEvent.click(await screen.findByLabelText("Provider"));
    const options = await screen.findAllByRole("option");
    const values = options.map((o) => o.textContent);

    // Regression guard: these are internal/taskrunner.SupportedProviders()'s
    // task-execution IDs (provider.list's vocabulary), which
    // internal/server/proxy.go never matches an account against -- an
    // account added under one of these would silently never route. Notably
    // no "GLM" here, even though provider.list above returned it.
    expect(values).toEqual([
      "Anthropic (Claude)",
      "OpenAI (Codex)",
      "Kimi",
      "xAI (Grok)",
      "Antigravity (Gemini)",
    ]);
  });

  it("renders a GLM row as managed-externally, with no credential or add-account affordance", async () => {
    const client = new FakeWsClient();
    renderDialog(client);

    client.nth("account.list").resolve([]);
    await flush();
    client.nth("provider.list").resolve({ providers: [{ id: "glm", label: "GLM", kind: "cli" }] });
    await flush();

    const row = await screen.findByTestId("accounts-external-provider-glm");
    expect(row).toHaveTextContent("GLM");
    expect(row).toHaveTextContent("Managed externally via CLI");

    // No credential/add-account affordance for it: it must never appear in
    // the manual-add dropdown or as a Connect button.
    expect(screen.queryByTestId("accounts-connect-glm")).not.toBeInTheDocument();
    await openManualForm();
    fireEvent.click(await screen.findByLabelText("Provider"));
    const options = await screen.findAllByRole("option");
    expect(options.map((o) => o.textContent)).not.toContain("GLM");
  });

  it("hides the managed-externally section when provider.list returns no cli-kind providers", async () => {
    const client = new FakeWsClient();
    renderDialog(client);

    client.nth("account.list").resolve([]);
    await flush();
    client.nth("provider.list").resolve({
      providers: [
        { id: "claude-native", label: "Claude Code" },
        { id: "kimi", label: "Kimi" },
      ],
    });
    await flush();

    expect(screen.queryByTestId("accounts-external-providers")).not.toBeInTheDocument();
  });

  it("add form requires label and credential before calling account.add", async () => {
    const client = new FakeWsClient();
    renderDialog(client);

    client.nth("account.list").resolve([]);
    await flush();
    await openManualForm();

    fireEvent.click(await screen.findByRole("button", { name: "Add account" }));
    await flush();

    expect(screen.getByText(/all required/i)).toBeInTheDocument();
    expect(client.calls.filter((c) => c.method === "account.add")).toHaveLength(0);
  });

  it("submits account.add with provider, label, credential and refreshes the list", async () => {
    const client = new FakeWsClient();
    renderDialog(client);

    client.nth("account.list", 0).resolve([]);
    await flush();
    await openManualForm();

    fireEvent.change(await screen.findByLabelText("Label", { selector: "#account-label" }), { target: { value: "main" } });
    fireEvent.change(screen.getByLabelText("Credential"), { target: { value: '{"apiKey":"sk"}' } });
    fireEvent.click(screen.getByRole("button", { name: "Add account" }));

    const add = await waitFor(() => client.nth("account.add"));
    expect(add.params).toEqual({
      provider: "anthropic",
      label: "main",
      credential: '{"apiKey":"sk"}',
    });

    await act(async () => {
      add.resolve({ id: 2, provider: "anthropic", label: "main", credentialType: "api_key", createdAt: "", updatedAt: "" });
    });
    await flush();
    client.nth("account.list", 1).resolve(ACCOUNTS);
    await flush();

    expect(await screen.findByText("main")).toBeInTheDocument();
  });

  it("surfaces a daemon error from account.add inline", async () => {
    const client = new FakeWsClient();
    renderDialog(client);

    client.nth("account.list", 0).resolve([]);
    await flush();
    await openManualForm();

    fireEvent.change(await screen.findByLabelText("Label", { selector: "#account-label" }), { target: { value: "main" } });
    fireEvent.change(screen.getByLabelText("Credential"), { target: { value: "tok" } });
    fireEvent.click(screen.getByRole("button", { name: "Add account" }));

    const add = await waitFor(() => client.nth("account.add"));
    await act(async () => {
      add.reject(new Error("account.add: provider not supported"));
    });
    await flush();

    expect(await screen.findByText(/provider not supported/)).toBeInTheDocument();
  });

  it("Connect requires a label before calling account.oauthStart", async () => {
    const client = new FakeWsClient();
    renderDialog(client);

    client.nth("account.list", 0).resolve([]);
    await flush();

    fireEvent.click(await screen.findByRole("button", { name: "Connect Anthropic (Claude)" }));
    await flush();

    expect(screen.getByText(/Label is required/)).toBeInTheDocument();
    expect(client.calls.filter((c) => c.method === "account.oauthStart")).toHaveLength(0);
  });

  it("Connect calls account.oauthStart, renders the authorize link on the event, and refreshes on success", async () => {
    const client = new FakeWsClient();
    renderDialog(client);

    client.nth("account.list", 0).resolve([]);
    await flush();

    fireEvent.change(screen.getByLabelText("Label", { selector: "#account-oauth-label" }), { target: { value: "personal" } });
    fireEvent.click(screen.getByRole("button", { name: "Connect Anthropic (Claude)" }));

    const start = await waitFor(() => client.nth("account.oauthStart"));
    expect(start.params).toEqual({ provider: "anthropic", label: "personal" });

    await act(async () => {
      client.emit("account.oauthStart", 0, "authorizeUrl", { url: "https://claude.ai/oauth/authorize?state=abc" });
    });
    await flush();

    const link = await screen.findByRole("link", { name: /open the login page/i });
    expect(link).toHaveAttribute("href", "https://claude.ai/oauth/authorize?state=abc");

    await act(async () => {
      start.resolve({ id: 3, provider: "anthropic", label: "personal", credentialType: "oauth", createdAt: "", updatedAt: "" });
    });
    await flush();

    client.nth("account.list", 1).resolve([
      { id: 3, provider: "anthropic", label: "personal", credentialType: "oauth", createdAt: "", updatedAt: "" },
    ]);
    await flush();

    expect(await screen.findByText("personal")).toBeInTheDocument();
  });

  it("surfaces a daemon error from account.oauthStart inline", async () => {
    const client = new FakeWsClient();
    renderDialog(client);

    client.nth("account.list", 0).resolve([]);
    await flush();

    fireEvent.change(screen.getByLabelText("Label", { selector: "#account-oauth-label" }), { target: { value: "personal" } });
    fireEvent.click(screen.getByRole("button", { name: "Connect OpenAI (Codex)" }));

    const start = await waitFor(() => client.nth("account.oauthStart"));
    await act(async () => {
      start.reject(new Error("account.oauthStart: a login for provider \"openai\" is already in progress"));
    });
    await flush();

    expect(await screen.findByText(/already in progress/)).toBeInTheDocument();
  });

  it("renders a neutral status dot for an untested account row, with a Test button", async () => {
    const client = new FakeWsClient();
    renderDialog(client);

    client.nth("account.list").resolve(ACCOUNTS);
    await flush();

    const row = await screen.findByTestId("accounts-row-anthropic");
    expect(row.querySelector('[data-testid="accounts-status-dot"]')).toHaveAttribute("data-status", "unknown");
    expect(screen.getByTestId("accounts-test-anthropic")).toHaveTextContent("Test");
    expect(client.calls.filter((c) => c.method === "provider.test")).toHaveLength(0);
  });

  it("Test calls provider.test and shows an ok result inline, turning the dot green", async () => {
    const client = new FakeWsClient();
    renderDialog(client);

    client.nth("account.list").resolve(ACCOUNTS);
    await flush();

    fireEvent.click(await screen.findByTestId("accounts-test-anthropic"));

    const test = await waitFor(() => client.nth("provider.test"));
    expect(test.params).toEqual({ provider: "anthropic" });

    await act(async () => {
      test.resolve({ ok: true, detail: 'using "main" (oauth, expires 2099-01-01T00:00:00Z)' });
    });
    await flush();

    const row = screen.getByTestId("accounts-row-anthropic");
    expect(row.querySelector('[data-testid="accounts-status-dot"]')).toHaveAttribute("data-status", "ok");
    expect(screen.getByTestId("accounts-test-result-anthropic")).toHaveTextContent(
      'using "main" (oauth, expires 2099-01-01T00:00:00Z)',
    );
  });

  it("Test shows a not-ok result inline and turns the dot red", async () => {
    const client = new FakeWsClient();
    renderDialog(client);

    client.nth("account.list").resolve(ACCOUNTS);
    await flush();

    fireEvent.click(await screen.findByTestId("accounts-test-anthropic"));
    const test = await waitFor(() => client.nth("provider.test"));

    await act(async () => {
      test.resolve({ ok: false, detail: "found 1 account(s) for \"anthropic\", but all credentials are expired" });
    });
    await flush();

    const row = screen.getByTestId("accounts-row-anthropic");
    expect(row.querySelector('[data-testid="accounts-status-dot"]')).toHaveAttribute("data-status", "failed");
    expect(screen.getByTestId("accounts-test-result-anthropic")).toHaveTextContent(/all credentials are expired/);
  });

  it("Test on a managed-externally (cli-kind) row calls provider.test with its provider id and shows the result", async () => {
    const client = new FakeWsClient();
    renderDialog(client);

    client.nth("account.list").resolve([]);
    await flush();
    client.nth("provider.list").resolve({ providers: [{ id: "glm", label: "GLM", kind: "cli" }] });
    await flush();

    fireEvent.click(await screen.findByTestId("accounts-test-glm"));
    const test = await waitFor(() => client.nth("provider.test"));
    expect(test.params).toEqual({ provider: "glm" });

    await act(async () => {
      test.resolve({ ok: false, detail: '"npx" not found on PATH: exec: "npx": executable file not found in $PATH' });
    });
    await flush();

    const row = screen.getByTestId("accounts-external-provider-glm");
    expect(row.querySelector('[data-testid="accounts-status-dot"]')).toHaveAttribute("data-status", "failed");
    expect(screen.getByTestId("accounts-test-result-glm")).toHaveTextContent(/not found on PATH/);
  });

  it("surfaces a rejected provider.test call as an inline not-ok result", async () => {
    const client = new FakeWsClient();
    renderDialog(client);

    client.nth("account.list").resolve(ACCOUNTS);
    await flush();

    fireEvent.click(await screen.findByTestId("accounts-test-anthropic"));
    const test = await waitFor(() => client.nth("provider.test"));

    await act(async () => {
      test.reject(new Error("provider.test: accounts registry is unavailable"));
    });
    await flush();

    expect(screen.getByTestId("accounts-test-result-anthropic")).toHaveTextContent(/accounts registry is unavailable/);
  });
});
