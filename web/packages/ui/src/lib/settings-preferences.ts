/**
 * Pure persistence for the settings screen's General/Appearance
 * preferences (ui-redesign-parity plan, Item 13) -- same shape as
 * lib/theme.ts: no React here, one localStorage key per preference,
 * every read defends against absent/corrupt/thrown storage the same way.
 *
 * "Preferences persist client-side (localStorage) unless and until a
 * daemon-side settings API exists" is Item 13's explicit decision -- see
 * the plan's Decisions section. Nothing here talks to the daemon.
 */

import type { ApprovalPolicy, Provider } from "@/lib/types";

export type FontSizeStep = "small" | "medium" | "large";

/** The three font-size axes Item 13 ships (audit-paseo.md §4's Appearance section): interface chrome, chat/content prose, and code. */
export interface FontSizes {
  interface: FontSizeStep;
  content: FontSizeStep;
  code: FontSizeStep;
}

export const DEFAULT_FONT_SIZES: FontSizes = { interface: "medium", content: "medium", code: "medium" };

const FONT_SIZE_STORAGE_KEY = "smind:settings:fontSizes";
const DEFAULT_PROVIDER_STORAGE_KEY = "smind:settings:defaultProvider";
const DEFAULT_APPROVAL_POLICY_STORAGE_KEY = "smind:settings:defaultApprovalPolicy";

function isFontSizeStep(v: unknown): v is FontSizeStep {
  return v === "small" || v === "medium" || v === "large";
}

/** Reads the persisted font sizes, filling in DEFAULT_FONT_SIZES for any axis that's absent, corrupt, or thrown -- a half-written or half-upgraded value never blanks the other two axes. */
export function readStoredFontSizes(): FontSizes {
  if (typeof window === "undefined") return DEFAULT_FONT_SIZES;
  try {
    const raw = window.localStorage.getItem(FONT_SIZE_STORAGE_KEY);
    if (!raw) return DEFAULT_FONT_SIZES;
    const parsed = JSON.parse(raw) as Partial<Record<keyof FontSizes, unknown>>;
    return {
      interface: isFontSizeStep(parsed.interface) ? parsed.interface : DEFAULT_FONT_SIZES.interface,
      content: isFontSizeStep(parsed.content) ? parsed.content : DEFAULT_FONT_SIZES.content,
      code: isFontSizeStep(parsed.code) ? parsed.code : DEFAULT_FONT_SIZES.code,
    };
  } catch {
    return DEFAULT_FONT_SIZES;
  }
}

/** Best-effort persistence, matching lib/theme.ts's writeStoredThemePreference: a write failure never stops the preference applying for the rest of the session. */
export function writeStoredFontSizes(sizes: FontSizes): void {
  if (typeof window === "undefined") return;
  try {
    window.localStorage.setItem(FONT_SIZE_STORAGE_KEY, JSON.stringify(sizes));
  } catch {
    // Best-effort only.
  }
}

/** The multiplier each step applies to its axis's CSS custom property -- deliberately narrow (0.9x/1x/1.15x) so "large" reads as a deliberate bump, not a different app. */
const FONT_SCALE: Record<FontSizeStep, number> = { small: 0.9, medium: 1, large: 1.15 };

/** Applies font sizes to the document as the `--font-scale-*` CSS custom properties index.css's font-size scale reads (see that file's comment) -- the single place besides this module's own default that names the three axes. */
export function applyFontSizes(sizes: FontSizes): void {
  if (typeof document === "undefined") return;
  const root = document.documentElement;
  root.style.setProperty("--font-scale-interface", String(FONT_SCALE[sizes.interface]));
  root.style.setProperty("--font-scale-content", String(FONT_SCALE[sizes.content]));
  root.style.setProperty("--font-scale-code", String(FONT_SCALE[sizes.code]));
}

/** Reads the persisted default-provider preference, or null if unset/corrupt/thrown. null means "no preference" -- callers fall back to whatever they'd otherwise choose (e.g. the daemon's own default), not to a hardcoded provider. */
export function readStoredDefaultProvider(): Provider | null {
  if (typeof window === "undefined") return null;
  try {
    const raw = window.localStorage.getItem(DEFAULT_PROVIDER_STORAGE_KEY);
    if (raw === "claude-native" || raw === "glm" || raw === "kimi" || raw === "codex-native") return raw;
    return null;
  } catch {
    return null;
  }
}

export function writeStoredDefaultProvider(provider: Provider | null): void {
  if (typeof window === "undefined") return;
  try {
    if (provider === null) window.localStorage.removeItem(DEFAULT_PROVIDER_STORAGE_KEY);
    else window.localStorage.setItem(DEFAULT_PROVIDER_STORAGE_KEY, provider);
  } catch {
    // Best-effort only.
  }
}

/** Reads the persisted default-approval-policy preference, or null if unset/corrupt/thrown ("no preference" -- same contract as readStoredDefaultProvider). */
export function readStoredDefaultApprovalPolicy(): ApprovalPolicy | null {
  if (typeof window === "undefined") return null;
  try {
    const raw = window.localStorage.getItem(DEFAULT_APPROVAL_POLICY_STORAGE_KEY);
    if (raw === "manual" || raw === "auto-safe") return raw;
    return null;
  } catch {
    return null;
  }
}

export function writeStoredDefaultApprovalPolicy(policy: ApprovalPolicy | null): void {
  if (typeof window === "undefined") return;
  try {
    if (policy === null) window.localStorage.removeItem(DEFAULT_APPROVAL_POLICY_STORAGE_KEY);
    else window.localStorage.setItem(DEFAULT_APPROVAL_POLICY_STORAGE_KEY, policy);
  } catch {
    // Best-effort only.
  }
}
