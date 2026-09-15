import { useCallback, useEffect, useState } from "react";

import {
  applyFontSizes,
  readStoredFontSizes,
  writeStoredFontSizes,
  type FontSizes,
  type FontSizeStep,
} from "@/lib/settings-preferences";

/**
 * Owns the three Appearance font-size steps: reads the persisted value
 * once, applies it to the document on every change (including the very
 * first render, so a value that drifted from what's already on the
 * document -- there's no pre-paint bootstrap script for this the way
 * lib/theme.ts's flash-prevention one exists for dark mode, since a wrong
 * font size for one frame is not the same class of problem as a
 * flash-of-wrong-theme) -- gets reconciled immediately.
 */
export function useFontSizes(): {
  fontSizes: FontSizes;
  setFontSize: (axis: keyof FontSizes, step: FontSizeStep) => void;
} {
  const [fontSizes, setFontSizes] = useState<FontSizes>(readStoredFontSizes);

  useEffect(() => {
    applyFontSizes(fontSizes);
  }, [fontSizes]);

  const setFontSize = useCallback((axis: keyof FontSizes, step: FontSizeStep) => {
    setFontSizes((prev) => {
      const next = { ...prev, [axis]: step };
      writeStoredFontSizes(next);
      return next;
    });
  }, []);

  return { fontSizes, setFontSize };
}
