#!/system/bin/sh
# All persistent state is owned by this module. Runtime mounts live only in
# /dev and disappear on reboot. Never traverse runtime mounts during removal.
HF_DATA=/data/adb/hyperfont-control
[ -d "$HF_DATA" ] || exit 0
[ ! -L "$HF_DATA" ] || exit 1
[ "$(cat "$HF_DATA/owner" 2>/dev/null)" = hyperfont-control-v1 ] || exit 1
awk -v root="$HF_DATA" '$5 == root || index($5, root "/") == 1 { bad=1 } END { exit bad ? 1 : 0 }' /proc/self/mountinfo || exit 1
rm -rf "$HF_DATA"
