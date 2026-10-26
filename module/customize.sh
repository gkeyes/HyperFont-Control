#!/system/bin/sh
SKIPUNZIP=0

[ "$API" -ge 29 ] || abort "HyperFont requires Android 10 or newer."
case "$ARCH" in
  arm64) HF_ARCH=arm64 ;;
  x64|x86_64) HF_ARCH=x86_64 ;;
  *) abort "This release supports arm64 and x86_64 only." ;;
esac

cp "$MODPATH/bin/$HF_ARCH/hfctl" "$MODPATH/bin/hfctl" || abort "Cannot select executable."
set_perm "$MODPATH/bin/hfctl" 0 0 0755
rm -rf "$MODPATH/bin/arm64" "$MODPATH/bin/x86_64"
# webroot ownership/context is assigned by the manager, as required by KSU.
ui_print "- HyperFont 2.1.0"
ui_print "- Reading this ROM's font configuration..."
HF_RESULT=$("$MODPATH/bin/hfctl" init)
[ "$?" -eq 0 ] || abort "$HF_RESULT"
ui_print "- Preparing the latest rules for the existing submitted profile..."
HF_RESULT=$("$MODPATH/bin/hfctl" rebuild)
[ "$?" -eq 0 ] || abort "$HF_RESULT"
ui_print "- Existing submitted settings will take effect after your manual reboot."
ui_print "- Font library and settings are kept in /data/adb/hyperfont-control"
ui_print "- Open WebUI, import fonts, save a profile, then reboot manually."
ui_print "- No font is selected by default. No metamodule is required."
ui_print "- HyperOS default-weight compatibility requires enabled Zygisk."
