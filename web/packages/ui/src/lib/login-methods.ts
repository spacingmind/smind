import type { ProviderInfo } from "@/lib/types";

/** The ways a user can connect an account for a provider (ADR-0033's login contract, plus the manual API-key fallback). */
export type ConnectMethod = "browser" | "device" | "api-key";

export interface ConnectMethodOption {
  value: ConnectMethod;
  label: string;
}

/** The connect methods a provider offers, in display order, keyed off provider.list's accountProvider/credentialKind. */
export function connectMethodsFor(
  provider: Pick<ProviderInfo, "credentialKind" | "accountProvider">,
): ConnectMethodOption[] {
  if (provider.accountProvider === "anthropic") {
    return [
      { value: "browser", label: "Claude.ai subscription" },
      { value: "api-key", label: "API key" },
    ];
  }
  if (provider.accountProvider === "openai") {
    return [
      { value: "browser", label: "Sign in with ChatGPT" },
      { value: "device", label: "Device code" },
      { value: "api-key", label: "API key" },
    ];
  }
  if (provider.credentialKind === "oauth") {
    return [
      { value: "browser", label: "Sign in with your browser" },
      { value: "api-key", label: "API key" },
    ];
  }
  return [{ value: "api-key", label: "API key" }];
}

/** The initially-selected connect method: device code on a remote daemon when offered, else browser when offered, else API key. */
export function defaultConnectMethod(
  provider: Pick<ProviderInfo, "credentialKind" | "accountProvider">,
  remote: boolean,
): ConnectMethod {
  const methods = connectMethodsFor(provider);
  if (remote && methods.some((m) => m.value === "device")) return "device";
  if (methods.some((m) => m.value === "browser")) return "browser";
  return "api-key";
}
