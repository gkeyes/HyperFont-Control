#!/system/bin/sh
MODDIR=${0%/*}
# Current KernelSU provides post-mount, after its metamodule. Magisk uses
# post-fs-data; other font modules are rejected by the engine preflight.
case "${KSU:-false}" in true|1) exit 0 ;; esac
"$MODDIR/bin/hfctl" boot > /dev/hyperfont-boot-result.json 2>&1
