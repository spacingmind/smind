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

// Mirrors internal/taskrunner.SupportedProviders()'s real shape (Item 7d):
// this dialog derives every section (managed-externally, Connect buttons,
// manual-add dropdown) from provider.list rather than a frontend constant,
// so these tests drive it the same way the daemon would.
const PROVIDERS = {
  providers: [
    { id: "claude-native", label: "Claude Code", credentialKind: "oauth", accountProvider: "anthropic" },
    { id: "glm", label: "GLM", kind: "cli" },
    { id: "kimi", label: "Kimi", credentialKind: "api-key", accountProvider: "kimi" },
    { id: "codex-native", label: "Codex", credentialKind: "oauth", accountProvider: "openai" },
  ],
};

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
  it("lists accounts from account.list, labeling each with provider.list's label for its account-provider id", async () => {
    const client = new FakeWsClient();
    renderDialog(client);

    client.nth("account.list").resolve(ACCOUNTS);
    client.nth("provider.list").resolve(PROVIDERS);
    await flush();

    expect(await screen.findByText("main")).toBeInTheDocument();
    // ACCOUNTS' entry has provider: "anthropic" (internal/accounts' vocabulary) --
    // its label comes from whichever provider.list entry has
    // accountProvider: "anthropic" (claude-native's "Claude Code"), not the
    // taskrunner id itself.
    expect(screen.getByText(/Claude Code · oauth/)).toBeInTheDocument();
  });

  it("falls back to the raw provider id when provider.list hasn't described it", async () => {
    const client = new FakeWsClient();
    renderDialog(client);

    client.nth("account.list").resolve(ACCOUNTS);
    await flush();
    // provider.list left unresolved (or could resolve empty) -- no label to
    // derive from yet.
    expect(screen.getByText(/anthropic · oauth/)).toBeInTheDocument();
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
    client.nth("provider.list").resolve(PROVIDERS);
    await flush();

    expect(screen.queryByLabelText("Provider")).not.toBeInTheDocument();
    const toggle = await screen.findByRole("button", { name: /Paste a credential instead/ });
    expect(toggle).toHaveAttribute("aria-expanded", "false");

    fireEvent.click(toggle);

    expect(await screen.findByLabelText("Provider")).toBeInTheDocument();
    expect(toggle).toHaveAttribute("aria-expanded", "true");
  });

  it("manual-add dropdown is derived from provider.list's credential-bearing providers, keyed by accountProvider not id", async () => {
    const client = new FakeWsClient();
    renderDialog(client);

    client.nth("account.list").resolve([]);
    await flush();
    client.nth("provider.list").resolve(PROVIDERS);
    await flush();
    await openManualForm();

    fireEvent.click(await screen.findByLabelText("Provider"));
    const options = await screen.findAllByRole("option");
    const values = options.map((o) => o.textContent);

    // GLM (kind: "cli", no credentialKind) never appears -- it's rendered
    // separately as "managed externally" (see below), with no credential
    // form at all. The remaining three are ordered and labeled exactly as
    // provider.list returned them (Claude Code/Kimi/Codex, not the
    // internal/accounts labels this dropdown used to hardcode). Its
    // default selection (its first entry) is exercised end-to-end by
    // "submits account.add..." below, which asserts the accountProvider
    // id ("anthropic") is what actually gets sent, not provider.list's own
    // "claude-native" -- the whole point of Item 7d's indirection.
    expect(values).toEqual(["Claude Code", "Kimi", "Codex"]);
  });

  it("renders a GLM row as managed-externally, with no credential or add-account affordance", async () => {
    const client = new FakeWsClient();
    renderDialog(client);

    client.nth("account.list").resolve([]);
    await flush();
    // A credential-bearing provider rides along so the manual-add dropdown
    // has at least one option to open -- the assertion is that GLM is
    // absent from it, not that the dropdown is empty.
    client.nth("provider.list").resolve({
      providers: [
        { id: "kimi", label: "Kimi", credentialKind: "api-key", accountProvider: "kimi" },
        { id: "glm", label: "GLM", kind: "cli" },
      ],
    });
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
    client.nth("provider.list").resolve(PROVIDERS);
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
    client.nth("provider.list").resolve(PROVIDERS);
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
    client.nth("provider.list").resolve(PROVIDERS);
    await flush();

    fireEvent.click(await screen.findByRole("button", { name: "Connect Claude Code" }));
    await flush();

    expect(screen.getByText(/Label is required/)).toBeInTheDocument();
    expect(client.calls.filter((c) => c.method === "account.oauthStart")).toHaveLength(0);
  });

  it("Connect calls account.oauthStart with the accountProvider id, renders the authorize link on the event, and refreshes on success", async () => {
    const client = new FakeWsClient();
    renderDialog(client);

    client.nth("account.list", 0).resolve([]);
    client.nth("provider.list").resolve(PROVIDERS);
    await flush();

    fireEvent.change(screen.getByLabelText("Label", { selector: "#account-oauth-label" }), { target: { value: "personal" } });
    fireEvent.click(screen.getByRole("button", { name: "Connect Claude Code" }));

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
    client.nth("provider.list").resolve(PROVIDERS);
    await flush();

    fireEvent.change(screen.getByLabelText("Label", { selector: "#account-oauth-label" }), { target: { value: "personal" } });
    fireEvent.click(screen.getByRole("button", { name: "Connect Codex" }));

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
