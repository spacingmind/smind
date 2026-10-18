import type { MouseEvent } from "react";

import { desktop, isDesktop } from "@/lib/platform";

/**
 * The one way the UI sends the user to an external http(s) page.
 *
 * The desktop webview swallows `window.open` and `<a target="_blank">` (a
 * new-window request never reaches the Rust navigation guard) and only
 * proxy-origin navigation is allowed, so a desktop build must go through the
 * `open_external` bridge. The browser build keeps ordinary links and
 * `window.open`.
 */
export function openExternalUrl(url: string): void {
  if (isDesktop) {
    desktop.openExternal(url).catch(() => {});
    return;
  }
  window.open(url, "_blank", "noopener,noreferrer");
}

/** Anchor props for an external link: a real `href` (copy/middle-click in the browser), opened through `openExternalUrl` on desktop. */
export function externalLinkProps(url: string): {
  href: string;
  target: "_blank";
  rel: "noreferrer";
  onClick: (event: MouseEvent) => void;
} {
  return {
    href: url,
    target: "_blank",
    rel: "noreferrer",
    onClick: (event) => {
      if (!isDesktop) return;
      event.preventDefault();
      openExternalUrl(url);
    },
  };
}
