import { useEffect, useState } from "react";

function readInset(): number {
  const vv = window.visualViewport;
  if (!vv) return 0;
  return Math.max(0, Math.round(window.innerHeight - vv.height - vv.offsetTop));
}

/**
 * Pixels the on-screen keyboard covers at the bottom of the window. With the
 * default interactive-widget behaviour the layout viewport does NOT shrink
 * when the keyboard opens, only the visual viewport does, so bottom-pinned UI
 * must lift itself by this amount. Always 0 without `visualViewport`.
 */
export function useKeyboardInset(): number {
  const [inset, setInset] = useState(readInset);
  useEffect(() => {
    const vv = window.visualViewport;
    if (!vv) return;
    const update = () => setInset(readInset());
    vv.addEventListener("resize", update);
    vv.addEventListener("scroll", update);
    update();
    return () => {
      vv.removeEventListener("resize", update);
      vv.removeEventListener("scroll", update);
    };
  }, []);
  return inset;
}
