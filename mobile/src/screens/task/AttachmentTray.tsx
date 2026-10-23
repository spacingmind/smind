// AttachmentTray: the pending attachments above the composer field as a
// horizontal row of pills (ADR-0038). AttachmentPill is shared with the
// timeline's read-only MessageAttachments (no onRemove there). The tray
// renders nothing when empty and never animates its height (AC2).

import { memo, useMemo } from 'react';
import { Image, Pressable, ScrollView, StyleSheet, Text, View } from 'react-native';
import { TrayPill, trayPills } from '../../attachmentTray';
import { PendingAttachment } from '../../attachments';
import { AppTheme } from '../../theme';
import { useAppTheme } from '../../theme/ThemeProvider';
import { AppIcon } from '../../ui/AppIcon';

export const AttachmentPill = memo(function AttachmentPill({
  id,
  label,
  sizeLabel,
  kind,
  thumbUri,
  onRemove,
}: {
  id: string;
  label: string;
  sizeLabel: string;
  kind: string;
  thumbUri?: string;
  onRemove?: (id: string) => void;
}) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  return (
    <View style={styles.pill} accessibilityLabel={sizeLabel ? `${label}, ${sizeLabel}` : label}>
      <View style={styles.lead}>
        {thumbUri ? (
          <Image source={{ uri: thumbUri }} style={styles.thumb} resizeMode="cover" accessibilityIgnoresInvertColors />
        ) : (
          <AppIcon name={kind === 'image' ? 'image' : 'file'} size={18} color={theme.foregroundSubtle} />
        )}
      </View>
      <View style={styles.text}>
        <Text style={styles.name} numberOfLines={1}>
          {label}
        </Text>
        {sizeLabel !== '' && <Text style={styles.size}>{sizeLabel}</Text>}
      </View>
      {onRemove !== undefined && (
        <Pressable
          accessibilityRole="button"
          accessibilityLabel={`Remove ${label}`}
          hitSlop={{ top: 12, bottom: 12, left: 10, right: 10 }}
          onPress={() => onRemove(id)}
          style={styles.remove}
        >
          <AppIcon name="close" size={12} color={theme.foregroundSubtle} />
        </Pressable>
      )}
    </View>
  );
});

export function AttachmentTray({ attachments, onRemove }: { attachments: PendingAttachment[]; onRemove: (id: string) => void }) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  const pills = useMemo<TrayPill[]>(() => trayPills(attachments), [attachments]);
  if (pills.length === 0) return null;
  return (
    <ScrollView horizontal showsHorizontalScrollIndicator={false} style={styles.tray} contentContainerStyle={styles.trayContent} accessibilityLabel="Attachments">
      {pills.map((p) => (
        <AttachmentPill key={p.id} id={p.id} label={p.label} sizeLabel={p.sizeLabel} kind={p.kind} thumbUri={p.thumbUri} onRemove={onRemove} />
      ))}
    </ScrollView>
  );
}

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    tray: { flexGrow: 0, marginBottom: theme.spacing[2] },
    trayContent: { gap: theme.spacing[2], paddingHorizontal: theme.spacing[1] },
    pill: {
      flexDirection: 'row',
      alignItems: 'center',
      gap: theme.spacing[2],
      maxWidth: 220,
      padding: theme.spacing[1],
      paddingRight: theme.spacing[2],
      backgroundColor: theme.secondary,
      borderWidth: 1,
      borderColor: theme.border,
      borderRadius: theme.radius.lg,
      borderCurve: 'continuous',
    },
    lead: { width: 40, height: 40, borderRadius: theme.radius.md, overflow: 'hidden', alignItems: 'center', justifyContent: 'center' },
    thumb: { width: 40, height: 40 },
    text: { flexShrink: 1, minWidth: 0 },
    name: { ...theme.type.label, color: theme.foreground },
    size: { ...theme.type.small, color: theme.foregroundSubtle },
    remove: { width: 20, height: 20, alignItems: 'center', justifyContent: 'center' },
  });
}
