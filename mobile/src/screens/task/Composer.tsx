// Composer: the queue list + compose row + Send/Stop/Send now, moved verbatim
// out of TaskDetailScreen (M4 step 3) with explicit props; M5 redesigns it.
// No visual or behavioural change from the inline version.

import { useMemo } from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { Button, TextInput as ExpoTextInput, useNativeState } from '@expo/ui';
import { ChatQueueItem } from '../../api';
import { StopControlView } from '../../stopControl';
import { AppTheme } from '../../theme';
import { useAppTheme } from '../../theme/ThemeProvider';
import { UiHost } from '../../ui/UiHost';
import { QueuedPrompts } from '../QueuedPrompts';

export function Composer({
  draftState,
  onChangeDraft,
  liveRunId,
  stopView,
  canSend,
  sendError,
  queueItems,
  onCancelQueued,
  onStop,
  onSend,
  onSendNow,
}: {
  draftState: ReturnType<typeof useNativeState<string>>;
  onChangeDraft: (text: string) => void;
  liveRunId: string | null;
  stopView: StopControlView;
  canSend: boolean;
  sendError: string | null;
  queueItems: ChatQueueItem[];
  onCancelQueued: (itemId: number) => void;
  onStop: () => void;
  onSend: () => void;
  onSendNow: () => void;
}) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  return (
    <>
      {sendError !== null && <Text style={styles.sendError}>Couldn't send: {sendError}</Text>}
      <QueuedPrompts items={queueItems} onCancel={onCancelQueued} />
      <View style={styles.composeRow}>
        <UiHost style={styles.composeWrap}>
          <ExpoTextInput
            style={styles.composeField}
            textStyle={styles.composeText}
            value={draftState}
            onChangeText={(text) => {
              draftState.value = text;
              onChangeDraft(text);
            }}
            placeholder={liveRunId !== null ? 'Queue a follow-up — it sends when this run finishes' : 'Send a follow-up prompt…'}
            placeholderTextColor={theme.foregroundMuted}
            multiline
          />
        </UiHost>
        {stopView.visible && (
          <UiHost style={styles.sendHost}>
            <Button label={stopView.label} onPress={onStop} disabled={stopView.disabled} />
          </UiHost>
        )}
        {liveRunId !== null && canSend && (
          <UiHost style={styles.sendHost}>
            <Button label="Send now" onPress={onSendNow} />
          </UiHost>
        )}
        <UiHost style={styles.sendHost}>
          <Button label="Send" onPress={onSend} disabled={!canSend} />
        </UiHost>
      </View>
    </>
  );
}

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    sendError: {
      color: theme.status.danger,
      fontSize: theme.type.metadataLabel.fontSize,
      marginTop: theme.spacing[2],
      marginBottom: theme.spacing[1],
    },
    composeRow: {
      flexDirection: 'row',
      alignItems: 'flex-end',
      gap: theme.spacing[2],
      borderTopWidth: 1,
      borderTopColor: theme.border,
      paddingTop: theme.spacing[2],
      marginTop: theme.spacing[2],
    },
    composeWrap: {
      flex: 1,
      maxHeight: 120,
    },
    sendHost: {
      alignSelf: 'flex-end',
    },
    composeField: {
      width: '100%',
      backgroundColor: theme.surface[2],
      borderRadius: theme.radius.md,
      paddingHorizontal: theme.spacing[2.5],
      paddingVertical: theme.spacing[2],
    },
    composeText: {
      fontSize: theme.type.interface.fontSize,
      color: theme.foreground,
    },
  });
}
