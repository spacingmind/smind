// types.ts is the pure-TS view model for daemon-validated ui:1 plugin view
// documents: what a later React Native step will draw. It mirrors the web
// renderer's presentation types (web/packages/ui/src/components/plugin-view)
// with every untrusted value already coerced to text or dropped -- no HTML,
// no fetched images, no plugin svg. No react-native import so vitest's plain
// node transform can load it.
import type { Block } from '../../transcript/markdown';
import type { IconName } from '../../ui/icons';

export type Tone = 'success' | 'warning' | 'danger' | 'muted';

export type ViewAction =
  | { kind: 'rpc'; rpc: string; params?: Record<string, unknown> }
  | { kind: 'prompt'; text: string }
  | { kind: 'command'; name: string }
  | { kind: 'openPanel'; panel: string; context?: Record<string, unknown> }
  | { kind: 'openUrl'; url: string; host: string };

/** `name` carries the app's IconName; svg and unknown names degrade to generic. */
export type IconRef = { kind: 'name'; name: IconName } | { kind: 'generic' } | { kind: 'none' };

export type FormField = {
  name: string;
  label: string;
  type: 'text' | 'secret' | 'select' | 'toggle';
  required: boolean;
  placeholder: string;
  options: { value: string; label: string }[];
  default: string | boolean;
};

export type ViewNode =
  | { kind: 'stack'; path: string; children: ViewNode[]; gap: 'sm' | 'md' | 'lg'; row: boolean; wrap: boolean }
  | { kind: 'heading'; path: string; text: string; level: 1 | 2 | 3 | 4 }
  | { kind: 'text'; path: string; text: string; small: boolean; tone?: Tone }
  | { kind: 'markdown'; path: string; blocks: Block[] }
  | { kind: 'badge'; path: string; text: string; tone?: Tone; icon: IconRef }
  | { kind: 'button'; path: string; label: string; tone?: Tone; disabled: boolean; icon: IconRef; action: ViewAction | null }
  | {
      kind: 'list';
      path: string;
      items: { title: string; subtitle: string; badge: string; tone?: Tone; icon: IconRef; action: ViewAction | null }[];
    }
  | { kind: 'keyValue'; path: string; items: { key: string; value: string; tone?: Tone }[] }
  | { kind: 'table'; path: string; columns: { key: string; label: string; alignEnd: boolean }[]; rows: string[][] }
  | { kind: 'code'; path: string; text: string }
  | { kind: 'diff'; path: string; lines: { text: string; kind: 'add' | 'del' | 'hunk' | 'ctx' }[] }
  | { kind: 'progress'; path: string; label: string; pct: number; tone?: Tone }
  | { kind: 'form'; path: string; id: string; fields: FormField[]; submitLabel: string; action: ViewAction | null }
  | { kind: 'empty'; path: string; title: string; text: string; icon: IconRef }
  | { kind: 'error'; path: string; title: string; text: string }
  | { kind: 'unsupported'; path: string; nodeType?: string };
