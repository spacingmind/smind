/// <reference types="vite/client" />

interface ImportMetaEnv {
  /** ADR-0013 AC1: set only by the desktop build (`build:desktop`); unset/absent in the daemon-embedded build. See `@/lib/platform`. */
  readonly VITE_SMIND_DESKTOP?: string;
}
