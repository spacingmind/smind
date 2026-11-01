/**
 * True only in `vite build --mode demo` (ADR-0051). A build-time constant: every other bundle drops the branches it guards,
 * so the daemon-embedded and desktop builds contain none of src/demo/**.
 */
export const IS_DEMO: boolean = __SMIND_DEMO__;
