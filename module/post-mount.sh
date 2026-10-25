#!/system/bin/sh
MODDIR=${0%/*}
"$MODDIR/bin/hfctl" boot > /dev/hyperfont-boot-result.json 2>&1
