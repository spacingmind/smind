// inboxView.test.ts: grouping, actions per kind, the snooze cap, relative time, the empty state and the a11y strings.

import { describe, expect, it } from 'vitest';
import { ICONS as icons } from '../ui/icons';
import { inboxActions, inboxAnnouncement, inboxBadgeLabel, inboxView, relativeTime, SNOOZE_CAP_MS, snoozeOptions } from '../inbox/view';
import { inboxItem } from './inboxFixtures';

const NOW = Date.parse('2026-10-27T10:30:00Z');
const perm = inboxItem({ key: 'perm:r1:q1', kind: 'permission', attention: 'high', title: 'Allow write?', links: { taskId: 7, permissionRequestId: 'q1', runId: 'r1' } });
const verdict = inboxItem();
const stall = inboxItem({ key: 'stall:9', kind: 'stall', state: 'seen', title: 'Stuck' });
const hostPair = inboxItem({ key: 'host_pair:mac', kind: 'host_pair', taskId: undefined, title: 'Approve mac?', links: { hostId: 'h1' } });
const hostRefused = inboxItem({ key: 'host_refused:mac', kind: 'host_refused', taskId: undefined, title: 'mac refused the link', links: {} });
const hostDown = inboxItem({ key: 'host_down:mac', kind: 'host_down', taskId: undefined, state: 'seen', title: 'mac unreachable', links: { hostId: 'h1' } });

describe('inbox grouping', () => {
  it('groups in the fixed order and names the sections', () => {
    const v = inboxView([stall, verdict, perm], NOW);
    expect(v.kind).toBe('list');
    if (v.kind !== 'list') return;
    expect(v.sections.map((s) => s.title)).toEqual(['Needs you now', 'Validator verdicts', 'Stalled']);
    expect(v.sections[0].rows[0]).toMatchObject({ key: 'perm:r1:q1', tone: 'waiting', unseen: true, taskId: 7 });
    expect(v.sections[1].rows[0].time).toBe('30m ago');
    expect(v.sections[1].rows[0].label).toBe('Passed');
    expect(inboxView([inboxItem({ attention: 'high' })], NOW)).toMatchObject({ sections: [{ rows: [{ label: 'Needs a decision' }] }] });
  });

  it('omits empty groups and shows the empty state for no items', () => {
    const v = inboxView([verdict], NOW);
    expect(v.kind === 'list' && v.sections.map((s) => s.kind)).toEqual(['verdict']);
    expect(inboxView([], NOW)).toMatchObject({ kind: 'empty', title: 'Nothing needs you' });
  });

  it('groups host kinds in order', () => {
    const v = inboxView([perm, hostPair, hostRefused, hostDown, verdict], NOW);
    expect(v.kind).toBe('list');
    if (v.kind !== 'list') return;
    expect(v.sections.map((s) => s.title)).toEqual(['Needs you now', 'Host pairing', 'Hosts refused', 'Hosts unreachable', 'Validator verdicts']);
    expect(v.sections[1].rows[0]).toMatchObject({ hostId: 'h1', icon: 'devices', label: 'Pairing', tone: 'waiting' });
    expect(v.sections[2].rows[0]).toMatchObject({ hostId: null, label: 'Refused', tone: 'warning' });
    expect(v.sections[3].rows[0]).toMatchObject({ hostId: 'h1', label: 'Unreachable', tone: 'neutral' });
  });

  it('uses registry icons', () => {
    const v = inboxView([perm, verdict, stall], NOW);
    const names = v.kind === 'list' ? v.sections.map((s) => s.rows[0].icon) : [];
    expect(names.every((n) => n in icons)).toBe(true);
  });
});

describe('inbox actions', () => {
  it('never offers snooze or resolve for a permission', () => {
    expect(inboxActions(perm)).toEqual(['seen', 'open']);
    expect(inboxActions({ ...perm, kind: 'verdict' })).not.toContain('snooze'); // a perm: key is enough on its own
    expect(inboxActions({ ...perm, kind: 'verdict' })).not.toContain('resolve');
  });
  it('offers snooze and resolve for verdict and stall; seen only while new', () => {
    expect(inboxActions(verdict)).toEqual(['seen', 'snooze', 'resolve', 'open']);
    expect(inboxActions(stall)).toEqual(['snooze', 'resolve', 'open']);
  });
  it('host rows have the hosts action and snooze/resolve but never open (they have no task)', () => {
    expect(inboxActions(hostPair)).toEqual(['seen', 'snooze', 'resolve', 'hosts']);
    expect(inboxActions(hostRefused)).toEqual(['seen', 'snooze', 'resolve', 'hosts']);
    expect(inboxActions(hostDown)).toEqual(['snooze', 'resolve', 'hosts']);
    for (const item of [hostPair, hostRefused, hostDown]) expect(inboxActions(item)).not.toContain('open');
  });
});

describe('snooze options', () => {
  it('are 1h, 4h and tomorrow, all within 24h', () => {
    for (const hour of [0, 3, 9, 10, 23]) {
      const now = new Date(2026, 9, 27, hour, 15);
      const opts = snoozeOptions(now);
      expect(opts.map((o) => o.id)).toEqual(['1h', '4h', 'tomorrow']);
      for (const o of opts) {
        const d = Date.parse(o.until) - now.getTime();
        expect(d).toBeGreaterThan(0);
        expect(d).toBeLessThanOrEqual(SNOOZE_CAP_MS);
      }
    }
    const now = new Date(2026, 9, 27, 10, 0);
    const [h1, h4] = snoozeOptions(now);
    expect(Date.parse(h1.until) - now.getTime()).toBe(3600_000);
    expect(Date.parse(h4.until) - now.getTime()).toBe(4 * 3600_000);
  });
});

describe('time and accessibility text', () => {
  it('relative time', () => {
    expect(relativeTime('2026-10-27T10:30:00Z', NOW)).toBe('just now');
    expect(relativeTime('2026-10-27T07:30:00Z', NOW)).toBe('3h ago');
    expect(relativeTime('2026-10-25T10:30:00Z', NOW)).toBe('2d ago');
    expect(relativeTime('nope', NOW)).toBe('nope');
  });
  it('badge label and polite announcement', () => {
    expect(inboxBadgeLabel(0)).toBe('Inbox, nothing new');
    expect(inboxBadgeLabel(1)).toBe('Inbox, 1 new item');
    expect(inboxBadgeLabel(3)).toBe('Inbox, 3 new items');
    expect(inboxAnnouncement(null, 3)).toBeNull();
    expect(inboxAnnouncement(3, 3)).toBeNull();
    expect(inboxAnnouncement(0, 2)).toBe('2 new inbox items');
    expect(inboxAnnouncement(2, 0)).toBe('Inbox is clear');
  });

  it('a verdict that needs a decision does not use the pass check icon', () => {
    const v = inboxView([inboxItem({ key: 'verdict:1', kind: 'verdict', attention: 'high' }), inboxItem({ key: 'verdict:2', kind: 'verdict', attention: 'normal' })], NOW);
    const rows = v.kind === 'list' ? v.sections[0].rows : [];
    expect(rows.map((r) => r.icon)).toEqual(['warning', 'check']);
  });

  it('tones: a permission is waiting, a verdict that needs a decision is warning (never a success tint), the rest neutral', () => {
    const v = inboxView(
      [
        inboxItem({ key: 'perm:1', kind: 'permission', attention: 'high' }),
        inboxItem({ key: 'verdict:1', kind: 'verdict', attention: 'high' }),
        inboxItem({ key: 'verdict:2', kind: 'verdict', attention: 'normal' }),
        inboxItem({ key: 'stall:1:1', kind: 'stall', attention: 'normal' }),
      ],
      NOW,
    );
    const tones = v.kind === 'list' ? v.sections.flatMap((s) => s.rows.map((r) => r.tone)) : [];
    expect(tones).toEqual(['waiting', 'warning', 'neutral', 'neutral']);
  });
});
