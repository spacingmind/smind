// Composer: the queue strip, send error and the rounded composer shell (M5):
// the field on top at full width, a right-aligned toolbar below with
// Send now (text) / Stop (outlined) / Send (filled) -- all native @expo/ui
// Buttons in their own UiHost, tinted by the Host's seedColor (expo-ui: no
// custom Pressable wrappers). Which actions show comes from composerActions.

import { useMemo } from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { Button, TextInput as ExpoTextInput, useNativeState } from '@expo/ui';
import { ChatQueueItem } from '../../api';
import { composerActions } from '../../composerActions';
import { StopControlView } from '../../stopControl';
import { AppTheme } from '../../theme';
import { useAppTheme } from '../../theme/ThemeProvider';
import { TextButton } from '../../ui/TextButton';
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
  const actions = composerActions({ liveRunId, canSend, stopping: stopView.disabled });
  return (
    <>
      <QueuedPrompts items={queueItems} onCancel={onCancelQueued} />
      {sendError !== null && (
        <Text style={styles.sendError} selectable>
          Couldn't send: {sendError}
        </Text>
      )}
      <View style={styles.shell}>
        <UiHost style={styles.fieldHost}>
          <ExpoTextInput
            style={styles.field}
            textStyle={styles.fieldText}
            value={draftState}
            onChangeText={(text) => {
              draftState.value = text;
              onChangeDraft(text);
            }}
            placeholder={liveRunId !== null ? 'Queue a follow-up — it sends when this run finishes' : 'Send a follow-up prompt…'}
            placeholderTextColor={theme.foregroundSubtlest}
            multiline
          />
        </UiHost>
        <View style={styles.toolbar}>
          {actions.sendNow.visible && <TextButton label="Send now" onPress={onSendNow} />}
          {actions.stop.visible && (
            <UiHost>
              <Button label={actions.stop.label} variant="outlined" onPress={onStop} disabled={actions.stop.disabled} style={styles.action} />
            </UiHost>
          )}
          <UiHost>
            <Button label="Send" variant="filled" onPress={onSend} disabled={actions.send.disabled} style={styles.action} />
          </UiHost>
        </View>
      </View>
    </>
  );
}

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    sendError: {
      color: theme.status.danger,
      fontSize: theme.type.small.fontSize,
      lineHeight: theme.type.small.lineHeight,
      marginBottom: theme.spacing[2],
    },
    shell: {
      backgroundColor: theme.card,
      borderWidth: 1,
      borderColor: theme.border,
      borderRadius: theme.radius['2xl'],
      borderCurve: 'continuous',
      padding: theme.spacing[2],
    },
    fieldHost: { alignSelf: 'stretch', maxHeight: 120 },
    field: { width: '100%', borderWidth: 0, backgroundColor: 'transparent', paddingHorizontal: theme.spacing[1], paddingVertical: theme.spacing[1] },
    fieldText: {
      fontSize: process.env.EXPO_OS === 'web' ? 16 : theme.type.body.fontSize, // web: 16px avoids browser zoom-on-focus
      lineHeight: theme.type.body.lineHeight,
      color: theme.foreground,
    },
    toolbar: {
      flexDirection: 'row',
      justifyContent: 'flex-end',
      alignItems: 'center',
      gap: theme.spacing[2],
      marginTop: theme.spacing[2],
    },
    action: { height: 44 },
  });
}
