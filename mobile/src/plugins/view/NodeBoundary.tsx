// NodeBoundary.tsx is the plugin view renderer's containment: one bad node
// must degrade to the UnsupportedNode placeholder instead of blanking the
// panel (the web renderer's PluginNodeBoundary twin). It imports no other
// component so the fallback can never itself depend on plugin rendering.

import { Component, ReactNode, useMemo } from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { AppTheme } from '../../theme';
import { useAppTheme } from '../../theme/ThemeProvider';

export class NodeBoundary extends Component<{ children: ReactNode }, { failed: boolean }> {
  state = { failed: false };

  static getDerivedStateFromError(): { failed: boolean } {
    return { failed: true };
  }

  render() {
    return this.state.failed ? <UnsupportedNode /> : this.props.children;
  }
}

export function UnsupportedNode({ nodeType }: { nodeType?: string }) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  // nodeType stays unrendered (the web twin carries it as a data attribute):
  // every degraded node shows the one fixed placeholder string.
  return (
    <View style={styles.box} testID="plugin-unsupported-node">
      <Text style={styles.text} accessibilityRole="text">
        Unsupported in this app version
      </Text>
    </View>
  );
}

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    box: {
      borderWidth: 1,
      borderStyle: 'dashed',
      borderColor: theme.border,
      borderRadius: theme.radius.md,
      padding: theme.spacing[2],
    },
    text: { ...theme.type.caption, color: theme.foregroundSubtle },
  });
}
