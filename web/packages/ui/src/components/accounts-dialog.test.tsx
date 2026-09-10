import { act } from "react";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { AccountsDialog } from "@/components/accounts-dialog";
import { FakeWsClient } from "@/test/fake-ws-client";

const ACCOUNTS = [
  {
    id: 1,
    provider: "glm",
    label: "main",
    credentialType: "api_key",
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

describe("AccountsDialog", () => {
  it("lists accounts from account.list", async () => {
    const client = new FakeWsClient();
    renderDialog(client);

    client.nth("account.list").resolve(ACCOUNTS);
    client.nth("provider.list").resolve({ providers: [] });
    await flush();

    expect(await screen.findByText("main")).toBeInTheDocument();
    expect(screen.getByText(/glm · api_key/)).toBeInTheDocument();
  });

  it("shows the empty hint when there are no accounts", async () => {
    const client = new FakeWsClient();
    renderDialog(client);

    client.nth("account.list").resolve([]);
    client.nth("provider.list").resolve({ providers: [] });
    await flush();

    expect(await screen.findByText(/No accounts yet/)).toBeInTheDocument();
  });

  it("add form requires label and credential before calling account.add", async () => {
    const client = new FakeWsClient();
    renderDialog(client);

    client.nth("account.list").resolve([]);
    client.nth("provider.list").resolve({ providers: [{ id: "glm" }] });
    await flush();

    fireEvent.click(await screen.findByRole("button", { name: "Add account" }));
    await flush();

    expect(screen.getByText(/all required/i)).toBeInTheDocument();
    expect(client.calls.filter((c) => c.method === "account.add")).toHaveLength(0);
  });

  it("submits account.add with provider, label, credential and refreshes the list", async () => {
    const client = new FakeWsClient();
    renderDialog(client);

    client.nth("account.list", 0).resolve([]);
    client.nth("provider.list", 0).resolve({ providers: [{ id: "glm" }] });
    await flush();

    fireEvent.change(await screen.findByLabelText("Label"), { target: { value: "main" } });
    fireEvent.change(screen.getByLabelText("Credential"), { target: { value: '{"apiKey":"sk"}' } });
    fireEvent.click(screen.getByRole("button", { name: "Add account" }));

    const add = await waitFor(() => client.nth("account.add"));
    expect(add.params).toEqual({
      provider: "glm",
      label: "main",
      credential: '{"apiKey":"sk"}',
    });

    await act(async () => {
      add.resolve({ id: 2, provider: "glm", label: "main", credentialType: "api_key", createdAt: "", updatedAt: "" });
    });
    await flush();
    client.nth("account.list", 1).resolve(ACCOUNTS);
    client.nth("provider.list", 1).resolve({ providers: [{ id: "glm" }] });
    await flush();

    expect(await screen.findByText("main")).toBeInTheDocument();
  });

  it("surfaces a daemon error from account.add inline", async () => {
    const client = new FakeWsClient();
    renderDialog(client);

    client.nth("account.list", 0).resolve([]);
    client.nth("provider.list", 0).resolve({ providers: [{ id: "glm" }] });
    await flush();

    fireEvent.change(await screen.findByLabelText("Label"), { target: { value: "main" } });
    fireEvent.change(screen.getByLabelText("Credential"), { target: { value: "tok" } });
    fireEvent.click(screen.getByRole("button", { name: "Add account" }));

    const add = await waitFor(() => client.nth("account.add"));
    await act(async () => {
      add.reject(new Error("account.add: provider not supported"));
    });
    await flush();

    expect(await screen.findByText(/provider not supported/)).toBeInTheDocument();
  });
});
