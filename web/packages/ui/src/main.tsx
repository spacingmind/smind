import { lazy, StrictMode, Suspense } from "react";
import { createRoot } from "react-dom/client";

import { App } from "@/App";
import { DesktopPaintedSignal } from "@/components/desktop-painted-signal";
import { TooltipProvider } from "@/components/ui/tooltip";
import { Toaster } from "@/components/ui/toast";
import { ThemeProvider } from "@/hooks/use-theme";
import { desktopOS, isDesktop } from "@/lib/platform";
import { applyStoredPluginTheme } from "@/lib/plugin-theme";
import { installDesktopContextMenu } from "@/lib/desktop-context-menu";
import { installDesktopWebviewDefaults } from "@/lib/desktop-webview";
import "@/index.css";
import "@/desktop-chrome.css";

// Desktop build only (ADR-0031 D7): the first-run gate decides between first-run and the app.
const FirstRunGate = isDesktop ? lazy(() => import("@/components/first-run/first-run-gate")) : null;

// Desktop build only: scopes desktop-chrome.css's per-OS rules.
if (desktopOS) document.documentElement.dataset.desktopOs = desktopOS;

// D2 (desktop-native-feel plan): the webview zoom guard and the
// context-menu policy -- each a no-op in the browser build.
installDesktopWebviewDefaults();
// A remembered plugin theme paints with the first frame (the daemon has not answered yet).
applyStoredPluginTheme();
installDesktopContextMenu();

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <ThemeProvider>
      <TooltipProvider>
        {FirstRunGate ? (
          <Suspense fallback={null}>
            <FirstRunGate>
              <App />
            </FirstRunGate>
          </Suspense>
        ) : (
          <App />
        )}
        <DesktopPaintedSignal />
        <Toaster />
      </TooltipProvider>
    </ThemeProvider>
  </StrictMode>,
);
