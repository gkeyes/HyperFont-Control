//go:build linux

package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestDeviceLogicalWeightMarker(t *testing.T) {
	if os.Getenv("HFC_COMPAT_MOUNT_AUDIT") != "1" {
		t.Skip("opt-in isolated device check")
	}
	self, _ := os.Readlink("/proc/self/ns/mnt")
	init, _ := os.Readlink("/proc/1/ns/mnt")
	info, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil || os.Getuid() != 0 || self == init || strings.Contains(string(info), " shared:") || strings.Contains(string(info), " master:") {
		t.Fatal("requires private root mount namespace")
	}
	s := Store{Root: "/data/adb/hyperfont-control", RomRoot: "/", Runtime: "/dev/hyperfont-control"}
	g, err := s.generation("active")
	if err != nil || g == nil || !needsFontCompat(*g) {
		t.Fatal("expected active Xiaomi custom profile", err)
	}
	original := s.Runtime
	s.Runtime, err = os.MkdirTemp("/dev", "hfc-compat-audit-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(s.Runtime)
	if err = mountFS("hfc-compat-audit", s.Runtime, "tmpfs", syscall.MS_NOSUID|syscall.MS_NODEV, "mode=0700"); err != nil {
		t.Fatal(err)
	}
	defer syscall.Unmount(s.Runtime, syscall.MNT_DETACH)
	for _, partition := range []string{"system", "product"} {
		dir := "/" + partition + "/fonts"
		if err = bind(filepath.Join(original, "original", partition, "fonts"), dir, true); err != nil {
			t.Fatal(err)
		}
		mirror, err := s.buildMirror(dir, *g)
		if err != nil {
			t.Fatal(err)
		}
		marker := filepath.Join(mirror, fontCompatMarker)
		b, err := os.ReadFile(marker)
		if err != nil || string(b) != fontCompatSignature {
			t.Fatal("invalid marker", err)
		}
		st, err := os.Stat(marker)
		if err != nil || st.Mode().Perm() != 0644 {
			t.Fatal("unreadable marker", err)
		}
		label, err := exec.Command("/system/bin/ls", "-Z", marker).Output()
		if err != nil || !strings.Contains(string(label), "u:object_r:system_file:s0") {
			t.Fatal("incorrect SELinux label", err)
		}
		if f, err := os.OpenFile(marker, os.O_WRONLY, 0); err == nil {
			f.Close()
			t.Fatal("marker view must be read-only")
		}
	}
	t.Log("COMPAT_MARKER_PASS partitions=2 content=verified mode=0644 context=system_file read_only=true")
}
