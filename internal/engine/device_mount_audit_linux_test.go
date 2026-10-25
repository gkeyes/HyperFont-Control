//go:build linux

package engine

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// Opt-in diagnostic only; absent from the module executable. The caller must
// already be in a separate private mount namespace. No app is instrumented.
func TestDeviceFontPayloadAfterUnmount(t *testing.T) {
	if os.Getenv("HFC_DEVICE_MOUNT_AUDIT") != "1" {
		t.Skip("opt-in device diagnostic")
	}
	self, e := os.Readlink("/proc/self/ns/mnt")
	if e != nil {
		t.Fatal(e)
	}
	init, e := os.Readlink("/proc/1/ns/mnt")
	if e != nil || self == init || os.Getuid() != 0 {
		t.Fatal("requires isolated root namespace")
	}
	info, e := os.ReadFile("/proc/self/mountinfo")
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(info), " shared:") || strings.Contains(string(info), " master:") {
		t.Fatal("mount propagation is not private")
	}
	s := Store{Root: "/data/adb/hyperfont-control", RomRoot: "/", Runtime: "/dev/hyperfont-control"}
	g, e := s.generation("pending")
	if e != nil || g == nil {
		t.Fatal("no candidate generation", e)
	}
	originalRuntime := s.Runtime
	s.Runtime = "/dev/hyperfont-unmount-audit"
	if e = os.Mkdir(s.Runtime, 0700); e != nil {
		t.Fatal(e)
	}
	defer os.Remove(s.Runtime)
	if e = mountFS("hyperfont-audit", s.Runtime, "tmpfs", syscall.MS_NOSUID|syscall.MS_NODEV, "mode=0700"); e != nil {
		t.Fatal(e)
	}
	defer syscall.Unmount(s.Runtime, syscall.MNT_DETACH)
	for _, partition := range []string{"system", "product"} {
		dir := "/" + partition + "/fonts"
		if e = bind(filepath.Join(originalRuntime, "original", partition, "fonts"), dir, true); e != nil {
			t.Fatal(e)
		}
		mirror, e := s.buildMirror(dir, *g)
		if e != nil {
			t.Fatal(e)
		}
		if e = bind(mirror, dir, true); e != nil {
			t.Fatal(e)
		}
	}
	var mounted []string
	for _, target := range g.Targets {
		if e = bind(s.data("generations", g.ID, target.File), target.Path, false); e != nil {
			t.Fatal(e)
		}
		mounted = append(mounted, target.Path)
	}
	// Reproduce the relevant FORCE_DENYLIST_UNMOUNT result: all font-related
	// /data/adb file binds are detached, while the tmpfs mirror remains.
	for i := len(mounted) - 1; i >= 0; i-- {
		if e = syscall.Unmount(mounted[i], syscall.MNT_DETACH); e != nil {
			t.Fatal(e)
		}
	}
	for name, want := range g.Fonts {
		var first os.FileInfo
		for _, dir := range []string{"/system/fonts", "/product/fonts"} {
			path := filepath.Join(dir, name)
			got, e := hashFile(path)
			if e != nil || got != want {
				t.Fatalf("font unavailable after detach: %s %v", path, e)
			}
			st, e := os.Stat(path)
			if e != nil || st.Size() == 0 {
				t.Fatal("empty font", e)
			}
			if first != nil && !os.SameFile(first, st) {
				t.Fatal("font copied twice")
			}
			first = st
		}
	}
	command := exec.Command("/system/bin/app_process", "/system/bin", "local.hyperfont.FontRouteProbe", "--serialized", "/data/local/tmp/hyperfont-coverage-20261025/system-fontmap.bin", "--package", "com.android.vending", "--hyper")
	command.Env = append(os.Environ(), "CLASSPATH=/data/local/tmp/hyperfont-coverage-20261025/probe.jar")
	output, e := command.CombinedOutput()
	if e != nil {
		t.Fatalf("font engine failed after unmount: %v %s", e, output)
	}
	var result struct {
		Error string `json:"error"`
		Runs  []struct {
			Error string `json:"error"`
		} `json:"runs"`
	}
	if e = json.Unmarshal(output, &result); e != nil || result.Error != "" || len(result.Runs) != 16 {
		t.Fatalf("invalid font probe: %v %s", e, output)
	}
	for _, run := range result.Runs {
		if run.Error != "" {
			t.Fatal(run.Error)
		}
	}
	t.Log("FONT_PAYLOAD_AFTER_UNMOUNT_PASS generated_fonts=", len(g.Fonts), " shared_inodes=true app_font_engine_runs=16")
}
