//go:build linux

package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// ProbeMount reproduces mirror preparation under a unique /dev directory.
// It never exposes a mount on /system, /product or a system XML file, and
// never changes the active/pending generation or boot result.
func (s *Store) ProbeMount() (any, error) {
	if os.Getuid() != 0 {
		return nil, fmt.Errorf("probe requires root")
	}
	g, e := s.generation("pending")
	if e != nil {
		return nil, e
	}
	if g == nil {
		return nil, fmt.Errorf("no pending profile")
	}
	root := fmt.Sprintf("/dev/hyperfont-probe-%d", os.Getpid())
	if e = os.Mkdir(root, 0700); e != nil {
		return nil, e
	}
	mounted := false
	defer func() {
		if mounted {
			_ = syscall.Unmount(root, syscall.MNT_DETACH)
		}
		_ = os.Remove(root)
	}()
	steps := []string{}
	ctx, _ := os.ReadFile("/proc/self/attr/current")
	fail := func(stage string, err error) (any, error) {
		return map[string]any{"steps": steps, "failedStage": stage, "error": err.Error(), "context": strings.TrimSpace(string(ctx)), "systemPathsReplaced": false}, nil
	}
	if e = mountFS("hyperfont-runtime", root, "tmpfs", syscall.MS_NOSUID|syscall.MS_NODEV, "mode=0700"); e != nil {
		return fail("runtime-tmpfs", e)
	}
	mounted = true
	steps = append(steps, "runtime-tmpfs")
	if e = mountFS("", root, "", syscall.MS_PRIVATE|syscall.MS_REC, ""); e != nil {
		return fail("runtime-private", e)
	}
	steps = append(steps, "runtime-private")
	copyStore := *s
	copyStore.Runtime = root
	for _, dir := range []string{"/system/fonts", "/product/fonts"} {
		if _, e = os.Stat(dir); os.IsNotExist(e) {
			continue
		}
		if _, e = copyStore.buildMirror(dir, *g); e != nil {
			return fail("mirror "+dir, e)
		}
		steps = append(steps, "mirror "+dir)
	}
	var payloadBytes int64
	for name, want := range g.Fonts {
		path := filepath.Join(root, "merged/system/fonts", name)
		var systemInfo os.FileInfo
		if systemInfo, e = os.Stat(path); e != nil {
			return fail("font-visible", e)
		}
		got, err := hashFile(path)
		if err != nil || got != want || systemInfo.Size() == 0 {
			return fail("font-content", fmt.Errorf("generated font data is missing or changed"))
		}
		payloadBytes += systemInfo.Size()
		if productInfo, err := os.Stat(filepath.Join(root, "merged/product/fonts", name)); err == nil && !os.SameFile(systemInfo, productInfo) {
			return fail("shared-font-inode", fmt.Errorf("partitions do not share font payload"))
		}
		if f, err := os.OpenFile(path, os.O_WRONLY, 0); err == nil {
			f.Close()
			return fail("read-only-mirror", fmt.Errorf("generated font view is writable"))
		}
	}
	info, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return fail("mount-inventory", err)
	}
	for _, line := range strings.Split(string(info), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 5 && strings.HasPrefix(fields[4], root+"/merged/") && strings.HasPrefix(filepath.Base(fields[4]), "HFC-") {
			return fail("removable-font-bind", fmt.Errorf("generated font still has an individual mount"))
		}
	}
	return map[string]any{"steps": steps, "passed": true, "context": strings.TrimSpace(string(ctx)), "systemPathsReplaced": false, "generatedPayloadBytes": payloadBytes, "sharedFontInodes": true, "generatedFileBindMounts": 0}, nil
}
