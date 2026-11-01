// icons.ts maps the plugin document's lucide icon names onto the app's own
// registry: a curated name becomes a native IconName, every other shape
// (unknown lucide name, inline svg) degrades to the generic glyph -- this app
// version renders no plugin-supplied svg. Never throws. Pure TS, no
// react-native import.
import type { IconName } from '../../ui/icons';
import type { IconRef } from './types';

const LUCIDE_TO_APP: Record<string, IconName> = {
  Check: 'check',
  X: 'close',
  ChevronRight: 'chevronRight',
  Info: 'info',
  TriangleAlert: 'warning',
  RefreshCw: 'refresh',
  Lock: 'lock',
  Square: 'stop',
  Send: 'send',
  Link: 'link',
  Paperclip: 'attach',
  File: 'file',
  Image: 'image',
};

const LUCIDE_NAME = /^[A-Z][A-Za-z0-9]{0,39}$/;

function isRecord(x: unknown): x is Record<string, unknown> {
  return typeof x === 'object' && x !== null && !Array.isArray(x);
}

export function pluginIcon(icon: unknown): IconRef {
  if (typeof icon === 'string' && LUCIDE_NAME.test(icon)) {
    const name = LUCIDE_TO_APP[icon];
    return name === undefined ? { kind: 'generic' } : { kind: 'name', name };
  }
  if (isRecord(icon) && typeof icon.svg === 'string') return { kind: 'generic' };
  return { kind: 'none' };
}
