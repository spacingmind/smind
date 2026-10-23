// MessageAttachments: a sent prompt's attachments, read-only, under its user
// bubble (ADR-0038). Pills always; an image that run.attachment can serve
// within the frame shows as a thumbnail once its bytes arrive, fetched lazily
// and one at a time (thumbLoader). A failed fetch leaves the pill.

import { memo, useEffect, useMemo, useState } from 'react';
import { Image, StyleSheet, Text, View } from 'react-native';
import { LineAttachment } from '../../runTimeline';
import { AppTheme } from '../../theme';
import { useAppTheme } from '../../theme/ThemeProvider';
import { shouldFetchThumb } from '../../thumbLoader';
import { AttachmentPill } from './AttachmentTray';

const THUMB_MAX = 120;

const Item = memo(function Item({ a, loadThumb }: { a: LineAttachment; loadThumb: (runId: string, id: string) => Promise<string | undefined> }) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  const [thumb, setThumb] = useState<string | undefined>(undefined);
  const fetchable = shouldFetchThumb(a);
  useEffect(() => {
    if (!fetchable || a.runId === undefined) return;
    let alive = true;
    void loadThumb(a.runId, a.id).then((uri) => alive && setThumb(uri));
    return () => {
      alive = false;
    };
  }, [a.id, a.runId, fetchable, loadThumb]);
  if (thumb !== undefined) {
    return (
      <View accessibilityLabel={`${a.label}, ${a.sizeLabel}`}>
        <Image source={{ uri: thumb }} style={styles.thumb} resizeMode="cover" accessibilityIgnoresInvertColors />
        <Text style={styles.caption} numberOfLines={1}>
          {a.label}
        </Text>
      </View>
    );
  }
  return <AttachmentPill id={a.id} label={a.label} sizeLabel={a.sizeLabel} kind={a.kind} thumbUri={a.uri} />;
});

export const MessageAttachments = memo(function MessageAttachments({
  attachments,
  loadThumb,
}: {
  attachments: LineAttachment[];
  loadThumb: (runId: string, id: string) => Promise<string | undefined>;
}) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  return (
    <View style={styles.row} accessibilityLabel="Attachments">
      {attachments.map((a) => (
        <Item key={a.id} a={a} loadThumb={loadThumb} />
      ))}
    </View>
  );
});

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    row: { alignSelf: 'flex-end', maxWidth: '85%', flexDirection: 'row', flexWrap: 'wrap', justifyContent: 'flex-end', alignItems: 'flex-start', gap: theme.spacing[2], marginTop: theme.spacing[1] },
    thumb: { width: THUMB_MAX, height: THUMB_MAX, borderRadius: theme.radius.lg, borderCurve: 'continuous', backgroundColor: theme.secondary },
    caption: { ...theme.type.small, color: theme.foregroundSubtle, maxWidth: THUMB_MAX },
  });
}
