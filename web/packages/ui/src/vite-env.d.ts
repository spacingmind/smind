/// <reference types="vite/client" />

interface ImportMetaEnv {
  /** ADR-0013 AC1: set only by the desktop build (`build:desktop`); unset/absent in the daemon-embedded build. See `@/lib/platform`. */
  readonly VITE_SMIND_DESKTOP?: string;
}

/**
 * ADR-0051: true only in `vite build --mode demo` (replaced by `define` at build time, so the branches it guards are
 * removed from every other bundle). In vitest it is a plain global, set to false in src/test/setup.ts.
 */
declare const __SMIND_DEMO__: boolean;
