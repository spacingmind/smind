; Tauri 2 NSIS installer hooks -- wired via bundle.windows.nsis.installerHooks
; in tauri.windows.conf.json.
;
; Tauri's own NSIS template always creates the Start Menu shortcut, but the
; Desktop shortcut is created unconditionally only for silent/passive
; installs -- an interactive install only gets one if the user notices and
; checks the finish page's "show readme" checkbox, which the template
; repurposes as "create a Desktop shortcut" ($(createDesktop), unchecked by
; default). That's what made a normal interactive install end up with a
; Start Menu entry and no Desktop shortcut. These hooks make the Desktop
; shortcut unconditional, matching the Start Menu one.
;
; ${PRODUCTNAME} and ${MAINBINARYNAME} are the same defines the template's
; own shortcut-creation code uses (installer.nsi), so this always points at
; the actual installed exe even if productName or the Cargo binary name
; changes; the shortcut's icon comes from the exe's own embedded icon
; (icons/icon.ico, set in tauri.conf.json's bundle.icon), same as the Start
; Menu shortcut.
!macro NSIS_HOOK_POSTINSTALL
  CreateShortCut "$DESKTOP\${PRODUCTNAME}.lnk" "$INSTDIR\${MAINBINARYNAME}.exe"
!macroend

!macro NSIS_HOOK_POSTUNINSTALL
  Delete "$DESKTOP\${PRODUCTNAME}.lnk"
!macroend
