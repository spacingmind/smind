import { useEffect, useRef } from "react";

import "./demo.css";

export const DEMO_BANNER_TEXT = "Demo — not connected to a daemon. Scripted data; nothing you type leaves your browser.";
export const DEMO_INSTALL_URL = "https://spacingmind.com";

/**
 * ADR-0051 section 3: the one demo-only component. A full-width strip above the app, not dismissible. Its measured height is
 * published as `--demo-banner-h` so demo.css can shorten the shell by exactly that much (no shell component is edited).
 */
export function DemoBanner() {
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const root = document.documentElement;
    root.dataset.demo = "";
    const publish = () => root.style.setProperty("--demo-banner-h", `${el.offsetHeight}px`);
    publish();
    const ro = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(publish);
    ro?.observe(el);
    window.addEventListener("resize", publish);
    return () => {
      ro?.disconnect();
      window.removeEventListener("resize", publish);
      delete root.dataset.demo;
      root.style.removeProperty("--demo-banner-h");
    };
  }, []);

  return (
    <div
      ref={ref}
      role="status"
      data-testid="demo-banner"
      className="demo-banner fixed inset-x-0 top-0 z-[60] flex flex-wrap items-center justify-center gap-x-3 gap-y-0.5 border-b bg-secondary px-3 py-1.5 text-center text-ui-sm text-secondary-foreground"
    >
      <span>{DEMO_BANNER_TEXT}</span>
      <a href={DEMO_INSTALL_URL} target="_blank" rel="noopener noreferrer" className="font-medium underline underline-offset-2">
        Install smind
      </a>
    </div>
  );
}
