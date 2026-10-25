//go:build linux

package engine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

func bind(source, target string, recursive bool) error {
	flags := uintptr(syscall.MS_BIND)
	if recursive {
		flags |= syscall.MS_REC
	}
	return mountFS(source, target, "", flags, "")
}

func mountFS(source, target, fs string, flags uintptr, options string) error {
	if err := syscall.Mount(source, target, fs, flags, options); err != nil {
		return fmt.Errorf("mount %s -> %s (fs=%s flags=0x%x): %w", source, target, fs, flags, err)
	}
	return nil
}
func placeholder(path string, mode os.FileMode) error {
	f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if e != nil {
		return e
	}
	return f.Close()
}
func (s *Store) buildMirror(dir string, g Generation) (string, error) {
	original := filepath.Join(s.Runtime, "original", strings.TrimPrefix(dir, "/"))
	merged := filepath.Join(s.Runtime, "merged", strings.TrimPrefix(dir, "/"))
	for _, p := range []string{original, merged} {
		if e := os.MkdirAll(p, 0755); e != nil {
			return "", e
		}
	}
	if e := bind(dir, original, true); e != nil {
		return "", e
	}
	if e := mountFS("", original, "", syscall.MS_PRIVATE|syscall.MS_REC, ""); e != nil {
		return "", e
	}
	// Keep both mirrors on the runtime tmpfs so generated fonts can be real
	// shared hard links instead of removable /data/adb mounts over empty files.
	if out, e := exec.Command("/system/bin/chcon", "u:object_r:system_file:s0", merged).CombinedOutput(); e != nil {
		return "", fmt.Errorf("字体目录标签设置失败: %s", strings.TrimSpace(string(out)))
	}
	entries, e := os.ReadDir(original)
	if e != nil {
		return "", e
	}
	for _, entry := range entries {
		src := filepath.Join(original, entry.Name())
		dst := filepath.Join(merged, entry.Name())
		st, e := os.Stat(src)
		if os.IsNotExist(e) && entry.Type()&os.ModeSymlink != 0 {
			target, e := os.Readlink(src)
			if e != nil {
				return "", e
			}
			if e = os.Symlink(target, dst); e != nil {
				return "", e
			}
			continue
		}
		if e != nil {
			return "", e
		}
		if strings.HasPrefix(entry.Name(), "HFC-") {
			return "", fmt.Errorf("原始目录含有其他 HyperFont 覆盖，请先重启")
		}
		if st.IsDir() {
			if e = os.Mkdir(dst, 0755); e != nil {
				return "", e
			}
			if e = bind(src, dst, true); e != nil {
				return "", e
			}
		} else if st.Mode().IsRegular() {
			if e = placeholder(dst, 0644); e != nil {
				return "", e
			}
			if e = bind(src, dst, false); e != nil {
				return "", e
			}
		} else {
			return "", fmt.Errorf("字体目录中含有非普通文件: %s", entry.Name())
		}
	}
	for name := range g.Fonts {
		dst := filepath.Join(merged, name)
		src, err := s.runtimeFont(g, name)
		if err != nil {
			return "", err
		}
		if out, err := exec.Command("/system/bin/chcon", "u:object_r:system_file:s0", src).CombinedOutput(); err != nil {
			return "", fmt.Errorf("运行字体标签设置失败: %s", strings.TrimSpace(string(out)))
		}
		if e = os.Link(src, dst); e != nil {
			return "", e
		}
	}
	if needsFontCompat(g) {
		marker := filepath.Join(merged, fontCompatMarker)
		if e = os.WriteFile(marker, []byte(fontCompatSignature), 0644); e != nil {
			return "", e
		}
		if out, err := exec.Command("/system/bin/chcon", "u:object_r:system_file:s0", marker).CombinedOutput(); err != nil {
			return "", fmt.Errorf("字重兼容标记设置失败: %s", strings.TrimSpace(string(out)))
		}
	}
	// A read-only self-bind affects this view, not the whole tmpfs needed to
	// prepare the other partition. Stock child mounts remain read-only.
	if e = bind(merged, merged, true); e != nil {
		return "", e
	}
	if e = mountFS("", merged, "", syscall.MS_BIND|syscall.MS_REMOUNT|syscall.MS_RDONLY|syscall.MS_NOSUID|syscall.MS_NODEV, ""); e != nil {
		return "", e
	}
	return merged, nil
}
func (s *Store) Boot() (result any, err error) {
	if os.Getuid() != 0 {
		return nil, fmt.Errorf("开机挂载需要 root")
	}
	if s.Runtime != "/dev/hyperfont-control" || s.Root != "/data/adb/hyperfont-control" {
		return nil, fmt.Errorf("拒绝非标准设备挂载路径")
	}
	var prior BootRecord
	_ = readJSON(s.data("boot.json"), &prior)
	bid := bootID()
	if prior.BootID == bid && bid != "" {
		return prior, nil
	}
	if property("sys.boot_completed") == "1" {
		return nil, fmt.Errorf("仅允许开机阶段挂载；请手动重启")
	}
	record := BootRecord{BootID: bid, State: "failed", Verified: "none"}
	var attempted *Generation
	defer func() {
		if err != nil {
			record.Error = err.Error()
			record.ConfigChecks = s.configObservations(attempted)
			_ = writeJSON(s.data("boot.json"), record)
		}
	}()
	pending, e := s.generation("pending")
	if e != nil {
		return nil, e
	}
	g := pending
	if g == nil {
		g, e = s.generation("active")
		if e != nil {
			return nil, e
		}
	}
	if g == nil {
		record.State = "system"
		return record, writeJSON(s.data("boot.json"), record)
	}
	record.Generation = g.ID
	attempted = g
	if !g.Profile.IsSystem() {
		blocked, e := s.bootAttemptBlocked(g.ID, bid)
		if e != nil {
			return nil, e
		}
		if blocked {
			record.State = "guarded"
			record.Error = "上次使用这份配置时未完成开机，本次已跳过覆盖并保留系统字体。请修改或重新提交配置后再手动重启。"
			return record, writeJSON(s.data("boot.json"), record)
		}
	}
	if e = s.conflicts(); e != nil {
		return nil, e
	}
	if e = s.verifyGeneration(*g); e != nil {
		return nil, e
	}
	configMounts, e := s.configMountTargets(*g)
	if e != nil {
		return nil, e
	}
	mounted := []string{}
	runtimeMounted := false
	committed := false
	defer func() {
		if !committed {
			for i := len(mounted) - 1; i >= 0; i-- {
				_ = syscall.Unmount(mounted[i], syscall.MNT_DETACH)
			}
			if runtimeMounted {
				_ = syscall.Unmount(s.Runtime, syscall.MNT_DETACH)
			}
		}
	}()
	if !g.Profile.IsSystem() {
		files := []string{}
		for _, t := range g.Targets {
			files = append(files, s.data("generations", g.ID, t.File))
		}
		for name := range g.Fonts {
			files = append(files, s.data("generations", g.ID, "fonts", name))
		}
		for _, p := range files {
			if e = os.Chmod(p, 0644); e != nil {
				return nil, e
			}
			if out, e := exec.Command("/system/bin/chcon", "u:object_r:system_file:s0", p).CombinedOutput(); e != nil {
				return nil, fmt.Errorf("字体安全标签设置失败: %s", strings.TrimSpace(string(out)))
			}
		}
		if e = os.MkdirAll(s.Runtime, 0700); e != nil {
			return nil, e
		}
		if e = mountFS("hyperfont-runtime", s.Runtime, "tmpfs", syscall.MS_NOSUID|syscall.MS_NODEV, "mode=0700"); e != nil {
			return nil, e
		}
		runtimeMounted = true
		if e = mountFS("", s.Runtime, "", syscall.MS_PRIVATE|syscall.MS_REC, ""); e != nil {
			return nil, e
		}
		mirrors := map[string]string{}
		for _, dir := range []string{"/system/fonts", "/product/fonts"} {
			if _, e = os.Stat(dir); os.IsNotExist(e) {
				continue
			}
			merged, e := s.buildMirror(dir, *g)
			if e != nil {
				return nil, e
			}
			mirrors[dir] = merged
		}
		// Build all mirrors before exposing any of them. Roll back every visible
		// bind if a later mount fails. No running-app hot replacement is supported.
		if e = s.startBootAttempt(g.ID, bid); e != nil {
			return nil, e
		}
		for _, dir := range []string{"/system/fonts", "/product/fonts"} {
			if src, ok := mirrors[dir]; ok {
				if e = bind(src, dir, true); e != nil {
					return nil, e
				}
				mounted = append(mounted, dir)
			}
		}
		for _, t := range configMounts {
			if e = bind(s.data("generations", g.ID, t.File), t.Path, false); e != nil {
				return nil, e
			}
			mounted = append(mounted, t.Path)
		}
		if e = s.verifyVisibleAtBoot(*g, true); e != nil {
			return nil, e
		}
		record.State = "mounted"
		record.Verified = "physical-config-and-font-content-hashes"
	} else {
		record.State = "system"
		record.Verified = "no-overrides-requested"
		if e = removeIfExists(s.data("boot-attempt.json")); e != nil {
			return nil, e
		}
	}
	if e = writeAtomic(s.data("active"), []byte(g.ID), 0600); e != nil {
		return nil, e
	}
	if e = writeJSON(s.data("boot.json"), record); e != nil {
		return nil, e
	}
	if pending != nil {
		if e = removeIfExists(s.data("pending")); e != nil {
			return nil, e
		}
	}
	committed = true
	return record, nil
}
