import { describe, expect, it } from 'vitest';
import { base64UrlEncode } from '@smind/relay-client/src/base64';
import { consumeOfferFromLocation } from '../fragment';

function offerPayload(): string {
  const offer = {
    v: 1,
    id: 'daemon-1',
    pk: base64UrlEncode(new Uint8Array(32).fill(3)),
    relay: 'https://relay.test',
    sec: base64UrlEncode(new Uint8Array(32).fill(9)),
    ws: 'host-1',
  };
  return base64UrlEncode(new TextEncoder().encode(JSON.stringify(offer)));
}

function fakeWin(href: string) {
  const calls: Array<string | undefined> = [];
  const hash = href.includes('#') ? href.slice(href.indexOf('#')) : '';
  return {
    calls,
    win: {
      location: { href, hash },
      history: { replaceState: (_s: unknown, _t: string, url?: string) => void calls.push(url) },
    },
  };
}

describe('offer fragment', () => {
  it('fragment_is_removed_after_pairing', () => {
    const href = `https://app.test/p/q?x=1#offer=${offerPayload()}`;
    const { win, calls } = fakeWin(href);
    expect(consumeOfferFromLocation(win)).toBe(href);
    expect(calls).toEqual(['/p/q?x=1']);
    expect(calls[0]).not.toContain('#');

    const bad = fakeWin('https://app.test/#offer=not-an-offer');
    expect(consumeOfferFromLocation(bad.win)).toBeNull();
    expect(bad.calls).toEqual(['/']);

    const none = fakeWin('https://app.test/x#other=1');
    expect(consumeOfferFromLocation(none.win)).toBeNull();
    expect(none.calls).toEqual([]);
  });

  it('fragment_helper_scrubs_an_offer_in_the_query_but_does_not_accept_it', () => {
    const payload = offerPayload();
    const q = fakeWin(`https://app.test/p?offer=${payload}&keep=1`);
    expect(consumeOfferFromLocation(q.win)).toBeNull();
    expect(q.calls).toEqual(['/p?keep=1']);

    const only = fakeWin(`https://app.test/p?offer=${payload}`);
    expect(consumeOfferFromLocation(only.win)).toBeNull();
    expect(only.calls).toEqual(['/p']);

    // both: the fragment's offer is accepted, and neither stays in the address
    const href = `https://app.test/p?offer=zzz&keep=1#offer=${payload}`;
    const both = fakeWin(href);
    expect(consumeOfferFromLocation(both.win)).toBe(href);
    expect(both.calls).toEqual(['/p?keep=1']);
  });
});
