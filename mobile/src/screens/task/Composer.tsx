// Composer: the queue strip, send error and the rounded composer shell (M5):
// the field on top at full width, a right-aligned toolbar below with
// Send now (text) / Stop (outlined) / Send (filled) -- all native @expo/ui
// Buttons in their own UiHost, tinted by the Host's seedColor (expo-ui: no
// custom Pressable wrappers). Which actions show comes from composerActions.

import { useMemo, useRef, useState } from 'react';
import { Platform, Pressable, StyleSheet, Text, View } from 'react-native';
import { BottomSheet, Button, TextInput as ExpoTextInput, useNativeState } from '@expo/ui';
import { ChatQueueItem } from '../../api';
import { AttachMenuItem } from '../../attachmentTray';
import { PendingAttachment } from '../../attachments';
import { composerActions } from '../../composerActions';
import { pickSlashCommand, SlashCommand, slashSuggestions } from '../../plugins/slashCommands';
import { applySlashRpcPrompt, SlashRpcOutcome, SlashRpcStatus } from '../../plugins/slashRpc';
import { StopControlView } from '../../stopControl';
import { AppTheme } from '../../theme';
import { useAppTheme } from '../../theme/ThemeProvider';
import { AppIcon } from '../../ui/AppIcon';
import { TextButton } from '../../ui/TextButton';
import { UiHost } from '../../ui/UiHost';
import { QueuedPrompts } from '../QueuedPrompts';
import { AttachmentTray } from './AttachmentTray';
import { SlashSuggestions } from './SlashSuggestions';

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
  placeholder,
  attachments = [],
  onRemoveAttachment,
  attachMenu = [],
  onAttach,
  attachNotice = null,
  draft = '',
  slashCommands = [],
  runSlashRpc,
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
  /** Field placeholder; defaults to the follow-up wording. */
  placeholder?: string;
  /** Pending attachments shown as a tray above the field (ADR-0038). */
  attachments?: PendingAttachment[];
  onRemoveAttachment?: (id: string) => void;
  /** Attach menu entries from the daemon's kinds; empty hides the Attach button. */
  attachMenu?: AttachMenuItem[];
  onAttach?: (key: AttachMenuItem['key']) => void;
  /** Inline attach refusal, shown beside sendError. */
  attachNotice?: string | null;
  /** The current draft text (the field itself holds it natively), for the `/` list. */
  draft?: string;
  /** Plugin prompt commands (ADR-0042); empty leaves the composer exactly as it was. */
  slashCommands?: SlashCommand[];
  /** Asks a plugin for an RPC-backed command's prompt (plugin.call on the daemon); the phone runs nothing itself. */
  runSlashRpc?: (row: SlashCommand) => Promise<SlashRpcOutcome>;
}) {
  const [menuOpen, setMenuOpen] = useState(false);
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  const actions = composerActions({ liveRunId, canSend, stopping: stopView.disabled });
  const slashRows = useMemo(() => slashSuggestions(slashCommands, draft), [slashCommands, draft]);
  // Picking only puts the prompt text in the field; the user still presses Send.
  const [slashStatus, setSlashStatus] = useState<SlashRpcStatus | null>(null);
  const draftRef = useRef(draft);
  draftRef.current = draft;
  const slashStatusRef = useRef<SlashRpcStatus | null>(null);
  slashStatusRef.current = slashStatus;
  const pickSlash = (row: SlashCommand) => {
    if (!row.rpc) {
      const next = pickSlashCommand(draft, row);
      draftState.value = next;
      onChangeDraft(next);
      return;
    }
    // An RPC-backed command: ask the plugin, then put its text in place of the /token. A late answer for a draft
    // that moved on is dropped; the draft is only ever text, and sending stays the Send button's job.
    if (!runSlashRpc || slashStatusRef.current?.state === 'running') return;
    const token = draft;
    setSlashStatus({ key: row.key, state: 'running' });
    void runSlashRpc(row).then((out) => {
      if (!out.ok) {
        setSlashStatus({ key: row.key, state: 'error', message: out.message, code: out.code });
        return;
      }
      setSlashStatus(null);
      const next = applySlashRpcPrompt(draftRef.current, token, out.prompt);
      if (next !== null) {
        draftState.value = next;
        onChangeDraft(next);
      }
    });
  };

  return (
    <>
      <QueuedPrompts items={queueItems} onCancel={onCancelQueued} />
      {sendError !== null && (
        <Text style={styles.sendError} selectable>
          Couldn't send: {sendError}
        </Text>
      )}
      {attachNotice !== null && (
        <Text style={styles.sendError} selectable accessibilityRole="alert">
          {attachNotice}
        </Text>
      )}
      <SlashSuggestions rows={slashRows} onPick={pickSlash} status={slashStatus} />
      <View style={styles.shell}>
        <AttachmentTray attachments={attachments} onRemove={(id) => onRemoveAttachment?.(id)} />
        <UiHost style={styles.fieldHost}>
          <ExpoTextInput
            style={styles.field}
            textStyle={styles.fieldText}
            value={draftState}
            onChangeText={(text) => {
              draftState.value = text;
              onChangeDraft(text);
              // An error is about the token it was raised for: any edit of the draft clears it.
              setSlashStatus((s) => (s?.state === 'error' ? null : s));
            }}
            placeholder={placeholder ?? (liveRunId !== null ? 'Queue a follow-up — it sends when this run finishes' : 'Send a follow-up prompt…')}
            placeholderTextColor={theme.foregroundSubtlest}
            multiline
          />
        </UiHost>
        <View style={styles.toolbar}>
          {attachMenu.length > 0 && (
            <Pressable accessibilityRole="button" accessibilityLabel="Attach" onPress={() => setMenuOpen(true)} style={styles.attach}>
              <AppIcon name="attach" size={20} color={theme.foregroundSubtle} />
            </Pressable>
          )}
          <View style={styles.spacer} />
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
      {attachMenu.length > 0 && (
        <UiHost>
          <BottomSheet isPresented={menuOpen} onDismiss={() => setMenuOpen(false)} containerColor={theme.overlay}>
            <View style={styles.menu}>
              {attachMenu.map((item) => (
                <TextButton
                  key={item.key}
                  label={item.label}
                  style={styles.menuItem}
                  onPress={() => {
                    setMenuOpen(false);
                    // iOS cannot present a picker while the sheet is still dismissing.
                    if (Platform.OS === 'ios') setTimeout(() => onAttach?.(item.key), 400);
                    else onAttach?.(item.key);
                  }}
                />
              ))}
            </View>
          </BottomSheet>
        </UiHost>
      )}
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
    // boxShadow: the web TextInput carries a base drop-shadow hairline that borderWidth/background overrides do not clear
    // (the shell is the field's surface); native ignores the key.
    field: { width: '100%', borderWidth: 0, backgroundColor: 'transparent', boxShadow: 'none', paddingHorizontal: theme.spacing[1], paddingVertical: theme.spacing[1] } as never,
    fieldText: {
      fontSize: process.env.EXPO_OS === 'web' ? 16 : theme.type.body.fontSize, // web: 16px avoids browser zoom-on-focus
      lineHeight: theme.type.body.lineHeight,
      color: theme.foreground,
    },
    toolbar: {
      flexDirection: 'row',
      alignItems: 'center',
      gap: theme.spacing[2],
      marginTop: theme.spacing[2],
    },
    action: { height: 44 },
    spacer: { flex: 1 },
    attach: { width: 44, height: 44, alignItems: 'center', justifyContent: 'center' },
    menu: { paddingBottom: theme.spacing[4] },
    menuItem: { alignSelf: 'flex-start' },
  });
}
