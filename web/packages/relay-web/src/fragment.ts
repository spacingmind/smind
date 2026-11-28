// fragment.ts takes a pairing offer out of the page address (#offer=...) and
// scrubs it from the address bar and history, valid or not: the fragment
// carries the admission secret and must not linger.

import { parsePairingURL } from '@smind/relay-client/src/pairing';

export interface OfferWindow {
  location: { href: string; hash: string };
  history: { replaceState(s: unknown, t: string, url?: string): void };
  document?: { title: string };
}

export function consumeOfferFromLocation(win: OfferWindow): string | null {
  const hash = win.location.hash.replace(/^#/, '');
  if (!new URLSearchParams(hash).has('offer')) return null;

  const href = win.location.href;
  let url: string | null = null;
  try {
    parsePairingURL(href);
    url = href;
  } catch {
    url = null;
  }

  const cut = href.indexOf('#');
  const noHash = cut === -1 ? href : href.slice(0, cut);
  let clean = noHash;
  try {
    const u = new URL(noHash);
    clean = u.pathname + u.search;
  } catch {
    // a relative href is already path-only
  }
  win.history.replaceState(null, '', clean);
  return url;
}
