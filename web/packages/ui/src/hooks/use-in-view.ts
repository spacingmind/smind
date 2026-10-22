import { useCallback, useState } from "react";

/**
 * A one-way "has been on screen" latch for lazy fetches (ADR-0038 §7): attach the returned ref to an element and
 * `inView` turns true the first time it intersects the viewport, then stays true (a thumbnail that scrolled away is
 * already cached; there is nothing to un-fetch). Without IntersectionObserver (jsdom, very old webviews) it is
 * true at once, so the thumbnail simply loads.
 */
export function useInView<T extends Element>(): { ref: (node: T | null) => void; inView: boolean } {
  const [inView, setInView] = useState(() => typeof IntersectionObserver === "undefined");
  const ref = useCallback((node: T | null) => {
    if (!node || typeof IntersectionObserver === "undefined") return;
    const observer = new IntersectionObserver((entries) => {
      if (entries.some((e) => e.isIntersecting)) {
        setInView(true);
        observer.disconnect();
      }
    });
    observer.observe(node);
    // React 19 ref cleanup: runs when the node unmounts or the ref changes.
    return () => observer.disconnect();
  }, []);
  return { ref, inView };
}
