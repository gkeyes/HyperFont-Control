#!/system/bin/sh
MODDIR=${0%/*}
# Late-start fallback for managers without boot-completed.sh. This never mounts
# fonts or reboots the device. Bounded retries tolerate a briefly busy WebUI.
tries=0
while [ "$tries" -lt 120 ]; do
  [ "$(getprop sys.boot_completed)" = "1" ] && break
  tries=$((tries + 1))
  sleep 5
done
[ "$(getprop sys.boot_completed)" = "1" ] || exit 0
tries=0
while [ "$tries" -lt 12 ]; do
  "$MODDIR/bin/hfctl" boot-complete > /dev/hyperfont-boot-complete.json 2>&1 && exit 0
  tries=$((tries + 1))
  sleep 5
done
