import { describe, expect, it } from "vitest";

import { connectMethodsFor, defaultConnectMethod } from "@/lib/login-methods";

describe("login methods", () => {
  it("connect methods per provider", () => {
    expect(connectMethodsFor({ credentialKind: "oauth", accountProvider: "anthropic" })).toEqual([
      { value: "browser", label: "Claude.ai subscription" },
      { value: "api-key", label: "API key" },
    ]);
    expect(connectMethodsFor({ credentialKind: "oauth", accountProvider: "openai" })).toEqual([
      { value: "browser", label: "Sign in with ChatGPT" },
      { value: "device", label: "Device code" },
      { value: "api-key", label: "API key" },
    ]);
    expect(connectMethodsFor({ credentialKind: "oauth", accountProvider: "kimi" })).toEqual([
      { value: "browser", label: "Sign in with your browser" },
      { value: "api-key", label: "API key" },
    ]);
    expect(connectMethodsFor({ credentialKind: "api-key", accountProvider: "xai" })).toEqual([
      { value: "api-key", label: "API key" },
    ]);
  });

  it("default connect method follows remote and availability", () => {
    const openai = { credentialKind: "oauth", accountProvider: "openai" } as const;
    const anthropic = { credentialKind: "oauth", accountProvider: "anthropic" } as const;
    const xai = { credentialKind: "api-key", accountProvider: "xai" } as const;
    expect(defaultConnectMethod(openai, true)).toBe("device");
    expect(defaultConnectMethod(openai, false)).toBe("browser");
    expect(defaultConnectMethod(anthropic, true)).toBe("browser");
    expect(defaultConnectMethod(anthropic, false)).toBe("browser");
    expect(defaultConnectMethod(xai, true)).toBe("api-key");
    expect(defaultConnectMethod(xai, false)).toBe("api-key");
  });
});
