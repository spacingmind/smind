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
  const href = win.location.href;
  const hash = win.location.hash.replace(/^#/, '');
  const inHash = new URLSearchParams(hash).has('offer');

  const cut = href.indexOf('#');
  const noHash = cut === -1 ? href : href.slice(0, cut);
  let path = noHash;
  let queryHadOffer = false;
  try {
    const u = new URL(noHash);
    if (u.searchParams.has('offer')) {
      queryHadOffer = true;
      u.searchParams.delete('offer');
    }
    const q = u.searchParams.toString();
    path = u.pathname + (queryHadOffer ? (q ? '?' + q : '') : u.search);
  } catch {
    // a relative href is already path-only
  }
  if (!inHash && !queryHadOffer) return null;

  // An offer is only accepted from the fragment; one in the query string is scrubbed and ignored.
  let url: string | null = null;
  if (inHash) {
    try {
      parsePairingURL(href);
      url = href;
    } catch {
      url = null;
    }
  }
  win.history.replaceState(null, '', path);
  return url;
}
