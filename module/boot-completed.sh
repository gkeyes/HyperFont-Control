#!/system/bin/sh
MODDIR=${0%/*}
"$MODDIR/bin/hfctl" boot-complete > /dev/hyperfont-boot-complete.json 2>&1
