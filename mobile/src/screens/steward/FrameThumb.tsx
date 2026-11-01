// FrameThumb.tsx is one evidence frame of a steward review: the png the
// daemon captured (steward.frame, base64) as a tappable thumbnail. The
// Pressable toggles between 140 and 420 high so one frame can be read
// closely without a second screen; anything that is not a drawable png
// (frameSource) becomes a quiet bordered box, never a broken image. Frame
// metadata (name/viewport/theme) is daemon text and is drawn as <Text> only.

import { useEffect, useMemo, useState } from 'react';
import { Image, Pressable, StyleSheet, Text, View } from 'react-native';
import { RelayConnection } from '../../relay/RelayConnection';
import { AppTheme } from '../../theme';
import { useAppTheme } from '../../theme/ThemeProvider';
import { fetchFrame } from '../../steward/api';
import { StewardFrame } from '../../steward/types';
import { frameSource } from '../../steward/view';
import { Skeleton } from '../../ui/Skeleton';

type FrameState = { kind: 'loading' } | { kind: 'ready'; uri: string } | { kind: 'failed' };

export function FrameThumb({ conn, reviewId, frame }: { conn: RelayConnection; reviewId: number; frame: StewardFrame }) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  const [state, setState] = useState<FrameState>({ kind: 'loading' });
  const [expanded, setExpanded] = useState(false);

  useEffect(() => {
    let alive = true;
    fetchFrame(conn, reviewId, frame.name)
      .then((data) => {
        if (!alive) return;
        const uri = frameSource(data);
        setState(uri === null ? { kind: 'failed' } : { kind: 'ready', uri });
      })
      .catch(() => {
        if (alive) setState({ kind: 'failed' });
      });
    return () => {
      alive = false;
    };
  }, [conn, reviewId, frame.name]);

  if (state.kind === 'loading') return <Skeleton width="100%" height={140} />;
  if (state.kind === 'failed') {
    return (
      <View style={styles.unavailable}>
        <Text style={styles.caption}>Frame unavailable</Text>
      </View>
    );
  }
  return (
    <View>
      <Pressable
        accessibilityRole="button"
        accessibilityLabel={`Frame ${frame.name}, ${expanded ? 'tap to collapse' : 'tap to expand'}`}
        style={styles.press}
        onPress={() => setExpanded((e) => !e)}
      >
        <Image source={{ uri: state.uri }} resizeMode="contain" style={expanded ? styles.imageExpanded : styles.image} />
      </Pressable>
      <Text style={styles.caption} selectable>{`${frame.name} · ${frame.viewport}px, ${frame.theme}`}</Text>
    </View>
  );
}

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    unavailable: {
      height: 140,
      borderWidth: 1,
      borderColor: theme.border,
      borderRadius: theme.radius.xl,
      borderCurve: 'continuous',
      alignItems: 'center',
      justifyContent: 'center',
    },
    press: { minHeight: 44 },
    image: { width: '100%', height: 140 },
    imageExpanded: { width: '100%', height: 420 },
    caption: { ...theme.type.small, color: theme.foregroundSubtle },
  });
}
