import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

import { App } from "@/App";
import { DesktopPaintedSignal } from "@/components/desktop-painted-signal";
import { TooltipProvider } from "@/components/ui/tooltip";
import { Toaster } from "@/components/ui/toast";
import { ThemeProvider } from "@/hooks/use-theme";
import { desktopOS } from "@/lib/platform";
import { installDesktopContextMenu } from "@/lib/desktop-context-menu";
import { installDesktopWebviewDefaults } from "@/lib/desktop-webview";
import "@/index.css";
import "@/desktop-chrome.css";

// Desktop build only: scopes desktop-chrome.css's per-OS rules.
if (desktopOS) document.documentElement.dataset.desktopOs = desktopOS;

// D2 (desktop-native-feel plan): the webview zoom guard and the
// context-menu policy -- each a no-op in the browser build.
installDesktopWebviewDefaults();
installDesktopContextMenu();

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <ThemeProvider>
      <TooltipProvider>
        <App />
        <DesktopPaintedSignal />
        <Toaster />
      </TooltipProvider>
    </ThemeProvider>
  </StrictMode>,
);
