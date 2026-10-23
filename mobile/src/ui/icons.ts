// icons.ts is the single icon registry (expo-design-system: tokens live in
// one place; so do icon names): every AppIcon consumer names an icon from
// ICONS and nothing else. PURE -- no react-native import -- so vitest's
// plain transform can load it (same constraint as theme.ts). `ios` is the
// SF Symbol name, `android` the @expo/material-symbols XML asset name
// (existence checked by icons.test.ts), `glyph` the web fallback character
// (universal Icon renders nothing on web).

export const ICONS = {
  chevronRight: { ios: 'chevron.right', android: 'chevron_right', glyph: '›' },
  more: { ios: 'ellipsis', android: 'more_horiz', glyph: '⋯' },
  close: { ios: 'xmark', android: 'close', glyph: '✕' },
  check: { ios: 'checkmark', android: 'check', glyph: '✓' },
  warning: { ios: 'exclamationmark.triangle', android: 'warning', glyph: '!' },
  info: { ios: 'info.circle', android: 'info', glyph: 'i' },
  refresh: { ios: 'arrow.clockwise', android: 'refresh', glyph: '↻' },
  lock: { ios: 'lock', android: 'lock', glyph: '•' },
  stop: { ios: 'stop.fill', android: 'stop', glyph: '■' },
  send: { ios: 'arrow.up', android: 'arrow_upward', glyph: '↑' },
  link: { ios: 'link', android: 'link', glyph: '↗' },
  attach: { ios: 'paperclip', android: 'attach_file', glyph: '+' },
  file: { ios: 'doc', android: 'description', glyph: '▤' },
  image: { ios: 'photo', android: 'image', glyph: '▣' },
  devices: { ios: 'laptopcomputer.and.iphone', android: 'devices', glyph: '▭' },
} as const;

export type IconName = keyof typeof ICONS;
