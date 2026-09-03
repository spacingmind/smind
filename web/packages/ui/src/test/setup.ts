// Vitest setup file for component tests.
//
// - @testing-library/jest-dom/vitest extends Vitest's `expect` with DOM
//   matchers (toBeInTheDocument, toHaveTextContent, etc.), registering
//   itself as a side effect of the import.
// - React 19's `act` requires `IS_REACT_ACT_ENVIRONMENT` to be explicitly
//   set in non-Jest test runners (Vitest doesn't set it itself), or every
//   state update triggers an "not configured to support act(...)" warning.
// - cleanup() unmounts anything rendered by a previous test. Vitest's own
//   globals (describe/it/expect/afterEach) aren't injected as ambient
//   globals in this project's config (see vitest.config.ts -- no
//   `globals: true`), so testing-library's usual auto-cleanup-via-ambient-
//   afterEach doesn't kick in on its own; wiring it explicitly here is the
//   standard fix.
import "@testing-library/jest-dom/vitest";
import { afterEach } from "vitest";
import { cleanup } from "@testing-library/react";

declare global {
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

// jsdom doesn't implement matchMedia; hooks/use-mobile.ts (used by the
// shadcn Sidebar primitives, so anything rendering AppSidebar/SidebarProvider
// hits it) calls it unconditionally. A minimal stub is enough -- no test
// here exercises actual media-query change behavior.
if (!window.matchMedia) {
  window.matchMedia = (query: string) =>
    ({
      matches: false,
      media: query,
      onchange: null,
      addListener: () => {},
      removeListener: () => {},
      addEventListener: () => {},
      removeEventListener: () => {},
      dispatchEvent: () => false,
    }) as MediaQueryList;
}

// jsdom doesn't implement ResizeObserver either; react-resizable-panels'
// <Group> (used by components/ui/resizable.tsx, which App.tsx renders
// unconditionally as its main layout) constructs one unconditionally on
// mount. TerminalPane's own ResizeObserver usage already guards against it
// being undefined (see that component's doc comment on what jsdom can't
// exercise), but this one isn't this project's code to guard -- a no-op
// stub is enough, since no test here exercises real panel-resize behavior.
if (!window.ResizeObserver) {
  window.ResizeObserver = class {
    observe(): void {}
    unobserve(): void {}
    disconnect(): void {}
  } as unknown as typeof ResizeObserver;
}

afterEach(() => {
  cleanup();
});
