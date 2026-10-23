// UserBubble: the right-aligned user message (ZCode look): `secondary` fill,
// radius.lg, reading type. Memoised with primitive props (vercel-react-native-
// skills: list items are memo'd and take primitives); selectable text (expo-native-ui).

import { memo, useMemo } from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { LineAttachment } from '../../runTimeline';
import { AppTheme } from '../../theme';
import { useAppTheme } from '../../theme/ThemeProvider';
import { MessageAttachments } from './MessageAttachments';

export const UserBubble = memo(function UserBubble({
  text,
  attachments,
  loadThumb,
}: {
  text: string;
  /** The prompt's attachments, read-only under the bubble; the array is stable per line. */
  attachments?: LineAttachment[];
  loadThumb?: (runId: string, id: string) => Promise<string | undefined>;
}) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  return (
    <>
      <View style={styles.bubble}>
        <Text style={styles.text} selectable>
          {text}
        </Text>
      </View>
      {attachments !== undefined && loadThumb !== undefined && <MessageAttachments attachments={attachments} loadThumb={loadThumb} />}
    </>
  );
});

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    bubble: {
      alignSelf: 'flex-end',
      maxWidth: '85%',
      backgroundColor: theme.secondary,
      borderRadius: theme.radius.lg,
      borderCurve: 'continuous',
      paddingHorizontal: theme.spacing[4],
      paddingVertical: theme.spacing[3],
    },
    text: { ...theme.type.reading, color: theme.foreground },
  });
}
