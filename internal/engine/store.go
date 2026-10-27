package engine

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

var ConfigPaths = []string{"/system/etc/fonts.xml", "/system/etc/font_fallback.xml", "/system_ext/etc/hyper_fonts.xml", "/system_ext/etc/hyper_font_fallback.xml", "/system_ext/etc/miui_fonts.xml", "/system_ext/etc/miui_font_fallback.xml", "/product/etc/fonts_customization.xml", "/product/etc/misans_variable_fonts.xml", "/product/etc/mi_fonts_customization.xml"}

type Store struct {
	Root    string
	RomRoot string
	Runtime string
	// Downloads is the phone's Download folder; empty means /sdcard/Download.
	Downloads string
}

type DownloadFont struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	Modified int64  `json:"modified"`
	Imported bool   `json:"imported"`
	// Folder is the subfolder of Download holding the file, empty at the top level.
	Folder string `json:"folder,omitempty"`
}

func (s *Store) downloads() (string, error) {
	dir := s.Downloads
	if dir == "" {
		dir = "/sdcard/Download"
	}
	return filepath.EvalSymlinks(dir)
}

// DownloadFonts lists font files in the Download folder and one level of subfolders,
// newest first, so the panel can import them by path without the WebView picker.
func (s *Store) DownloadFonts() ([]DownloadFont, error) {
	dir, e := s.downloads()
	if e != nil {
		return nil, fmt.Errorf("无法读取 Download 文件夹: %v", e)
	}
	library, e := s.Library()
	if e != nil {
		return nil, e
	}
	// Name and size pick candidates cheaply; only those get hashed to confirm the content.
	known := map[string]map[string]bool{}
	for _, a := range library {
		key := fmt.Sprint(a.Label, "\x00", a.Size)
		if known[key] == nil {
			known[key] = map[string]bool{}
		}
		known[key][a.ID] = true
	}
	imported := func(path, name string, size int64) bool {
		ids := known[fmt.Sprint(name, "\x00", size)]
		if ids == nil {
			return false
		}
		b, err := os.ReadFile(path)
		return err == nil && ids[digest(b)]
	}
	out := []DownloadFont{}
	var walk func(string, int)
	walk = func(d string, depth int) {
		entries, err := os.ReadDir(d)
		if err != nil {
			return
		}
		for _, entry := range entries {
			path := filepath.Join(d, entry.Name())
			if entry.IsDir() {
				if depth < 1 && !strings.HasPrefix(entry.Name(), ".") {
					walk(path, depth+1)
				}
				continue
			}
			switch strings.ToLower(filepath.Ext(entry.Name())) {
			case ".ttf", ".otf", ".ttc", ".otc":
			default:
				continue
			}
			info, err := entry.Info()
			if err != nil || !info.Mode().IsRegular() || info.Size() < 12 || info.Size() > MaxFontSize {
				continue
			}
			folder := ""
			if depth > 0 {
				folder = filepath.Base(d)
			}
			out = append(out, DownloadFont{path, entry.Name(), info.Size(), info.ModTime().Unix(), imported(path, entry.Name(), info.Size()), folder})
		}
	}
	walk(dir, 0)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Modified != out[j].Modified {
			return out[i].Modified > out[j].Modified
		}
		return out[i].Path < out[j].Path
	})
	return out, nil
}

type Baseline struct {
	Fingerprint string            `json:"fingerprint"`
	Configs     map[string]string `json:"configs"`
	SDK         int               `json:"sdk"`
}
type Target struct {
	Path string `json:"path"`
	File string `json:"file"`
	SHA  string `json:"sha256"`
}
type Generation struct {
	ID           string            `json:"id"`
	Profile      Profile           `json:"profile"`
	Targets      []Target          `json:"targets"`
	Fonts        map[string]string `json:"fonts"`
	Dependencies map[string]string `json:"dependencies"`
	Warnings     []string          `json:"warnings"`
	Fingerprint  string            `json:"fingerprint"`
	Created      string            `json:"created"`
}
type BootRecord struct {
	BootID       string              `json:"bootId"`
	Generation   string              `json:"generation"`
	State        string              `json:"state"`
	Error        string              `json:"error,omitempty"`
	Verified     string              `json:"verification"`
	ConfigChecks []ConfigObservation `json:"configChecks,omitempty"`
}
type Request struct {
	Op      string   `json:"op"`
	Profile *Profile `json:"profile,omitempty"`
	Path    string   `json:"path,omitempty"`
	Label   string   `json:"label,omitempty"`
	ID      string   `json:"id,omitempty"`
	Token   string   `json:"token,omitempty"`
	Chunk   string   `json:"chunk,omitempty"`
	Offset  int64    `json:"offset,omitempty"`
	Size    int64    `json:"size,omitempty"`
}

func (s *Store) data(parts ...string) string {
	return filepath.Join(append([]string{s.Root}, parts...)...)
}
func (s *Store) rom(path string) string {
	if s.RomRoot == "" || s.RomRoot == "/" {
		return path
	}
	return filepath.Join(s.RomRoot, path)
}
func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func token() string {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func readJSON(path string, v any) error {
	b, e := os.ReadFile(path)
	if e != nil {
		return e
	}
	return strictJSON(b, v)
}
func writeAtomic(path string, b []byte, mode os.FileMode) error {
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".write-")
	if e != nil {
		return e
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if e = f.Chmod(mode); e != nil {
		f.Close()
		return e
	}
	if _, e = f.Write(b); e != nil {
		f.Close()
		return e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	if e = os.Rename(tmp, path); e != nil {
		return e
	}
	if d, err := os.Open(filepath.Dir(path)); err == nil {
		err = d.Sync()
		d.Close()
		return err
	}
	return nil
}
func writeJSON(path string, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	return writeAtomic(path, b, 0600)
}
func hashFile(path string) (string, error) {
	f, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil {
		return "", e
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func (s *Store) Locked(fn func() (any, error)) (any, error) {
	if info, e := os.Lstat(s.Root); e == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("数据目录不能是符号链接")
	}
	if e := os.MkdirAll(s.Root, 0700); e != nil {
		return nil, e
	}
	f, e := os.OpenFile(s.data(".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	if e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		return nil, fmt.Errorf("另一个操作正在运行，请稍后重试")
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	marker := s.data("owner")
	if b, e := os.ReadFile(marker); e == nil {
		if string(b) != "hyperfont-control-v1\n" {
			return nil, fmt.Errorf("数据目录归属不匹配")
		}
	} else {
		entries, _ := os.ReadDir(s.Root)
		if len(entries) > 1 {
			return nil, fmt.Errorf("拒绝接管已有数据目录")
		}
		if e = writeAtomic(marker, []byte("hyperfont-control-v1\n"), 0600); e != nil {
			return nil, e
		}
	}
	return fn()
}
func (s *Store) originalFont(path string) (string, error) {
	var dir string
	for _, d := range []string{"/system/fonts", "/product/fonts", "/system_ext/fonts"} {
		if filepath.Dir(path) == d {
			dir = d
			break
		}
	}
	if dir == "" || filepath.Base(path) == "." {
		return "", fmt.Errorf("不允许的字体路径")
	}
	if s.Runtime != "" {
		p := filepath.Join(s.Runtime, "original", strings.TrimPrefix(dir, "/"), filepath.Base(path))
		if info, e := os.Stat(p); e == nil && info.Mode().IsRegular() {
			return p, nil
		}
	}
	return s.rom(path), nil
}
func property(name string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	b, _ := exec.CommandContext(ctx, "/system/bin/getprop", name).Output()
	return strings.TrimSpace(string(b))
}
func (s *Store) fingerprint() string {
	if s.RomRoot != "" && s.RomRoot != "/" {
		return "host-fixture"
	}
	return property("ro.build.fingerprint")
}
func (s *Store) conflicts() error {
	if s.RomRoot != "" && s.RomRoot != "/" {
		return nil
	}
	for _, p := range []string{"/data/adb/modules/MFGA", "/data/adb/modules_update/MFGA"} {
		if _, e := os.Stat(p); e == nil {
			return fmt.Errorf("请先卸载 MFGA 并手动重启，避免叠加字体配置")
		}
	}
	entries, _ := os.ReadDir("/data/adb/modules")
	for _, d := range entries {
		if !d.IsDir() || d.Name() == "hyperfont_control" {
			continue
		}
		base := filepath.Join("/data/adb/modules", d.Name())
		if _, e := os.Stat(filepath.Join(base, "disable")); e == nil {
			continue
		}
		for _, p := range []string{"system/fonts", "system/etc/fonts.xml", "system/etc/font_fallback.xml", "system/product/fonts", "system/product/etc/misans_variable_fonts.xml"} {
			if _, e := os.Stat(filepath.Join(base, p)); e == nil {
				return fmt.Errorf("检测到另一个字体覆盖模块: %s", d.Name())
			}
		}
	}
	return nil
}
func (s *Store) Init() (Baseline, error) {
	var b Baseline
	if err := readJSON(s.data("baseline.json"), &b); err == nil {
		// An upgrade can discover another independent ROM entry point. Extend
		// only missing, untouched files; never resnapshot already-mounted XML.
		if b.Fingerprint != s.fingerprint() {
			return b, nil
		}
		changed := false
		for _, path := range ConfigPaths {
			if _, ok := b.Configs[path]; ok {
				continue
			}
			raw, e := os.ReadFile(s.rom(path))
			if os.IsNotExist(e) {
				continue
			}
			if e != nil {
				return b, e
			}
			if bytesContainHFC(raw) {
				return b, fmt.Errorf("新发现的字体配置已被覆盖，不能捕获为原生基线: %s", path)
			}
			if _, e = parseXML(raw); e != nil {
				return b, e
			}
			if e = writeAtomic(s.data("baseline", strings.TrimPrefix(path, "/")), raw, 0600); e != nil {
				return b, e
			}
			b.Configs[path] = digest(raw)
			changed = true
		}
		if changed {
			return b, writeJSON(s.data("baseline.json"), b)
		}
		return b, nil
	}
	if e := s.conflicts(); e != nil {
		return b, e
	}
	b = Baseline{Fingerprint: s.fingerprint(), Configs: map[string]string{}, SDK: 0}
	if b.Fingerprint == "" {
		return b, fmt.Errorf("无法读取系统版本")
	}
	if s.RomRoot == "" || s.RomRoot == "/" {
		fmt.Sscanf(property("ro.build.version.sdk"), "%d", &b.SDK)
	} else {
		b.SDK = 37
	}
	for _, p := range ConfigPaths {
		raw, e := os.ReadFile(s.rom(p))
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return b, e
		}
		if bytesContainHFC(raw) {
			return b, fmt.Errorf("当前仍有本模块挂载，请先重启后重新识别系统")
		}
		if _, e = parseXML(raw); e != nil {
			return b, fmt.Errorf("%s: %w", p, e)
		}
		if e = writeAtomic(s.data("baseline", strings.TrimPrefix(p, "/")), raw, 0600); e != nil {
			return b, e
		}
		b.Configs[p] = digest(raw)
	}
	if b.Configs["/system/etc/fonts.xml"] == "" && b.Configs["/system/etc/font_fallback.xml"] == "" {
		return b, fmt.Errorf("未发现受支持的 Android 字体表")
	}
	return b, writeJSON(s.data("baseline.json"), b)
}
func bytesContainHFC(b []byte) bool { return strings.Contains(string(b), "HFC-") }
func (s *Store) ImportFile(path, label string) (Asset, error) {
	var a Asset
	st, e := os.Stat(path)
	if e != nil {
		return a, e
	}
	if !st.Mode().IsRegular() || st.Size() < 12 || st.Size() > MaxFontSize {
		return a, fmt.Errorf("请选择不超过 256 MiB 的 TTF/OTF/TTC/OTC")
	}
	f, e := os.Open(path)
	if e != nil {
		return a, e
	}
	b, e := io.ReadAll(io.LimitReader(f, MaxFontSize+1))
	_ = f.Close()
	if e != nil {
		return a, e
	}
	faces, e := ParseFont(b)
	if e != nil {
		return a, e
	}
	if label == "" {
		label = filepath.Base(path)
	}
	if len([]rune(label)) > 200 {
		return a, fmt.Errorf("字体名称过长")
	}
	a = Asset{digest(b), label, int64(len(b)), faces}
	if e = writeAtomic(s.data("library", a.ID+".font"), b, 0600); e != nil {
		return a, e
	}
	return a, writeJSON(s.data("library", a.ID+".json"), a)
}
func (s *Store) Library() ([]Asset, error) {
	out := []Asset{}
	entries, e := os.ReadDir(s.data("library"))
	if os.IsNotExist(e) {
		return out, nil
	}
	if e != nil {
		return nil, e
	}
	for _, f := range entries {
		if !strings.HasSuffix(f.Name(), ".json") {
			continue
		}
		var a Asset
		if e = readJSON(s.data("library", f.Name()), &a); e != nil {
			return nil, e
		}
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	return out, nil
}
func (s *Store) Stage(p Profile) (Generation, error) {
	return s.stage(p, true)
}

// RebuildCommitted applies compiler fixes during module upgrades. A submitted
// pending profile takes precedence; an unsubmitted draft is never activated or
// overwritten. Stage publishes the new pending pointer only after validation.
func (s *Store) RebuildCommitted() (any, error) {
	for _, pointer := range []string{"pending", "active"} {
		prior, err := s.generation(pointer)
		if err != nil {
			return nil, err
		}
		if prior == nil {
			continue
		}
		next, err := s.stage(prior.Profile, false)
		if err != nil {
			return nil, err
		}
		return map[string]any{"rebuilt": true, "source": pointer, "generation": next.ID, "activation": "manual-reboot"}, nil
	}
	return map[string]any{"rebuilt": false, "reason": "no-committed-profile"}, nil
}

func (s *Store) stage(p Profile, saveDraft bool) (Generation, error) {
	var g Generation
	warnings, e := s.Validate(p)
	if e != nil {
		return g, e
	}
	b, e := s.Init()
	if e != nil {
		return g, e
	}
	if !p.IsSystem() && b.Fingerprint != s.fingerprint() {
		return g, fmt.Errorf("系统已升级，请重启后重新识别系统基线")
	}
	if e = s.conflicts(); e != nil {
		return g, e
	}
	id := token()
	dir := s.data("generations", ".building-"+id)
	if e = os.MkdirAll(dir, 0700); e != nil {
		return g, e
	}
	defer os.RemoveAll(dir)
	c := compiler{s, dir, map[string]string{}, map[string]string{}, map[string]bool{}}
	g = Generation{ID: id, Profile: p, Targets: []Target{}, Fonts: map[string]string{}, Dependencies: c.deps, Warnings: warnings, Fingerprint: b.Fingerprint, Created: time.Now().UTC().Format(time.RFC3339)}
	if !p.IsSystem() {
		paths := []string{}
		for path := range b.Configs {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		for _, path := range paths {
			raw, e := os.ReadFile(s.data("baseline", strings.TrimPrefix(path, "/")))
			if e != nil {
				return g, e
			}
			if digest(raw) != b.Configs[path] {
				return g, fmt.Errorf("基线文件已被修改")
			}
			out, count, e := c.compileXML(path, raw, p)
			if e != nil {
				return g, fmt.Errorf("%s: %w", path, e)
			}
			if count == 0 {
				continue
			}
			file := filepath.Join("config", strings.TrimPrefix(path, "/"))
			if e = writeAtomic(filepath.Join(dir, file), out, 0644); e != nil {
				return g, e
			}
			g.Targets = append(g.Targets, Target{path, file, digest(out)})
		}
		if len(g.Targets) == 0 {
			return g, fmt.Errorf("未找到可修改的字体入口")
		}
		for name := range c.references {
			h, e := hashFile(filepath.Join(dir, "fonts", name))
			if e != nil {
				return g, e
			}
			g.Fonts[name] = h
		}
	}
	if e = writeJSON(filepath.Join(dir, "manifest.json"), g); e != nil {
		return g, e
	}
	if !p.IsSystem() && (s.RomRoot == "" || s.RomRoot == "/") {
		exe, err := os.Executable()
		if err != nil {
			return g, err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		command := exec.CommandContext(ctx, "/system/bin/app_process", "/system/bin", "local.hyperfont.VerifyFonts", dir)
		command.Env = append(os.Environ(), "CLASSPATH="+filepath.Join(filepath.Dir(exe), "verify-fonts.jar"))
		output, err := command.CombinedOutput()
		cancel()
		if err != nil || !strings.Contains(string(output), fmt.Sprintf("HFC_ANDROID_FONT_OK %d", len(g.Fonts))) || !strings.Contains(string(output), fmt.Sprintf("HFC_ANDROID_CONFIG_OK %d", len(g.Targets))) {
			return g, fmt.Errorf("Android 字体或配置校验失败，原配置保持不变: %s", strings.TrimSpace(string(output)))
		}
	}
	final := s.data("generations", id)
	if e = os.Rename(dir, final); e != nil {
		return g, e
	}
	if saveDraft {
		if e = writeJSON(s.data("draft.json"), p); e != nil {
			return g, e
		}
	}
	if e = writeAtomic(s.data("pending"), []byte(id), 0600); e != nil {
		return g, e
	}
	s.pruneGenerations()
	return g, nil
}

func (s *Store) pruneGenerations() {
	keep := map[string]bool{}
	for _, pointer := range []string{"active", "pending"} {
		b, e := os.ReadFile(s.data(pointer))
		if e == nil && tokenPattern.MatchString(string(b)) {
			keep[string(b)] = true
		}
	}
	entries, _ := os.ReadDir(s.data("generations"))
	type old struct {
		name     string
		modified time.Time
	}
	all := []old{}
	for _, entry := range entries {
		if !entry.IsDir() || !tokenPattern.MatchString(entry.Name()) {
			continue
		}
		info, e := entry.Info()
		if e == nil {
			all = append(all, old{entry.Name(), info.ModTime()})
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].modified.After(all[j].modified) })
	extra := false
	for _, g := range all {
		if keep[g.name] {
			continue
		}
		if !extra {
			extra = true
			continue
		}
		_ = os.RemoveAll(s.data("generations", g.name))
	}
}
func (s *Store) generation(pointer string) (*Generation, error) {
	b, e := os.ReadFile(s.data(pointer))
	if os.IsNotExist(e) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	id := string(b)
	if !tokenPattern.MatchString(id) {
		return nil, fmt.Errorf("生成记录损坏")
	}
	var g Generation
	e = readJSON(s.data("generations", id, "manifest.json"), &g)
	if e == nil && g.ID != id {
		e = fmt.Errorf("生成 ID 不匹配")
	}
	return &g, e
}
func bootID() string {
	b, _ := os.ReadFile("/proc/sys/kernel/random/boot_id")
	return strings.TrimSpace(string(b))
}
func (s *Store) Status() (any, error) {
	assets, e := s.Library()
	if e != nil {
		return nil, e
	}
	pending, e := s.generation("pending")
	if e != nil {
		return nil, e
	}
	active, e := s.generation("active")
	if e != nil {
		return nil, e
	}
	p := DefaultProfile()
	_ = readJSON(s.data("draft.json"), &p)
	var boot BootRecord
	_ = readJSON(s.data("boot.json"), &boot)
	if id := bootID(); id != "" && boot.BootID != id {
		boot.State = "not-run-this-boot"
		boot.Error = ""
		boot.Verified = "none"
	}
	var base Baseline
	_ = readJSON(s.data("baseline.json"), &base)
	visible := false
	visibilityError := ""
	if boot.BootID != "" && boot.BootID == bootID() && boot.State == "mounted" && active != nil {
		if err := s.verifyVisible(*active); err != nil {
			visibilityError = err.Error()
		} else {
			visible = true
		}
	}
	compatNeeded := active != nil && needsFontCompat(*active)
	marker, _ := os.ReadFile(s.rom("/system/fonts/" + fontCompatMarker))
	compatReady := visible && compatNeeded && string(marker) == fontCompatSignature
	compat := map[string]any{"needed": compatNeeded, "preparedThisBoot": compatReady, "requiresZygisk": compatNeeded, "appRenderingVerified": false}
	return map[string]any{"version": Version, "library": assets, "draft": p, "pending": pending, "active": active, "boot": boot, "visible": visible, "visibilityError": visibilityError, "baseline": base, "fontCompatibility": compat, "capabilities": map[string]any{"variable": true, "static": true, "continuousModern": base.SDK >= 35, "bundledAppFonts": false, "notificationRoleHook": false, "automaticReboot": false, "mountBackend": "ephemeral-bind", "deviceValidation": "runtime-content-hashes"}}, nil
}
func (s *Store) Handle(r Request) (any, error) {
	switch r.Op {
	case "status":
		return s.Status()
	case "init":
		return s.Init()
	case "refresh_baseline":
		active, e := s.generation("active")
		if e != nil {
			return nil, e
		}
		if active != nil && !active.Profile.IsSystem() && s.verifyVisible(*active) == nil {
			return nil, fmt.Errorf("当前仍在覆盖字体，请先恢复系统字体并重启")
		}
		for _, p := range ConfigPaths {
			b, e := os.ReadFile(s.rom(p))
			if e == nil && bytesContainHFC(b) {
				return nil, fmt.Errorf("仍有字体覆盖，不能用作原生基线")
			}
		}
		if e = removeIfExists(s.data("baseline.json")); e != nil {
			return nil, e
		}
		return s.Init()
	case "export_profile":
		if r.Profile == nil {
			return nil, fmt.Errorf("缺少配置")
		}
		if _, e := s.Validate(*r.Profile); e != nil {
			return nil, e
		}
		return exportDownload("profile", r.Profile)
	case "export_diagnostics":
		info, e := s.Handle(Request{Op: "diagnostics"})
		if e != nil {
			return nil, e
		}
		return exportDownload("diagnostics", info)
	case "validate":
		if r.Profile == nil {
			return nil, fmt.Errorf("缺少配置")
		}
		w, e := s.Validate(*r.Profile)
		return map[string]any{"warnings": w}, e
	case "save":
		if r.Profile == nil {
			return nil, fmt.Errorf("缺少配置")
		}
		w, e := s.Validate(*r.Profile)
		if e != nil {
			return nil, e
		}
		e = writeJSON(s.data("draft.json"), r.Profile)
		return map[string]any{"warnings": w}, e
	case "stage":
		if r.Profile == nil {
			return nil, fmt.Errorf("缺少配置")
		}
		return s.Stage(*r.Profile)
	case "restore":
		// Keep the user's draft: restoring stages system fonts only.
		return s.stage(DefaultProfile(), false)
	case "cancel_pending":
		return map[string]bool{"cancelled": true}, removeIfExists(s.data("pending"))
	case "list_downloads":
		return s.DownloadFonts()
	case "import_path":
		path, e := filepath.EvalSymlinks(r.Path)
		if e != nil {
			return nil, e
		}
		allowed, e := s.downloads()
		if e != nil {
			return nil, e
		}
		if !strings.HasPrefix(path, allowed+string(os.PathSeparator)) {
			return nil, fmt.Errorf("仅允许从手机 Download 文件夹导入")
		}
		return s.ImportFile(path, r.Label)
	case "upload_begin":
		if r.Size < 12 || r.Size > MaxFontSize {
			return nil, fmt.Errorf("文件大小不支持")
		}
		if _, e := s.cleanupUploads(time.Now(), false); e != nil {
			return nil, e
		}
		entries, e := s.uploadSessions()
		if e != nil {
			return nil, e
		}
		if len(entries) >= 8 {
			return nil, fmt.Errorf("导入队列已满，请先清理未完成导入")
		}
		id := token()
		path := s.data("uploads", id)
		if e := writeAtomic(path, nil, 0600); e != nil {
			return nil, e
		}
		if e := writeJSON(path+".json", r); e != nil {
			_ = os.Remove(path)
			return nil, e
		}
		return map[string]string{"token": id}, nil
	case "upload_chunk":
		if !tokenPattern.MatchString(r.Token) {
			return nil, fmt.Errorf("导入令牌无效")
		}
		path := s.data("uploads", r.Token)
		var begin Request
		if e := readJSON(path+".json", &begin); e != nil {
			return nil, e
		}
		b, e := base64.StdEncoding.DecodeString(r.Chunk)
		if e != nil || len(b) == 0 || len(b) > 48<<10 {
			return nil, fmt.Errorf("导入分块无效")
		}
		f, e := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0600)
		if e != nil {
			return nil, e
		}
		defer f.Close()
		st, e := f.Stat()
		if e != nil {
			return nil, e
		}
		if st.Size() != r.Offset || st.Size()+int64(len(b)) > begin.Size {
			return nil, fmt.Errorf("导入位置不一致")
		}
		_, e = f.Write(b)
		return map[string]int64{"offset": st.Size() + int64(len(b))}, e
	case "upload_finish":
		if !tokenPattern.MatchString(r.Token) {
			return nil, fmt.Errorf("导入令牌无效")
		}
		path := s.data("uploads", r.Token)
		var begin Request
		if e := readJSON(path+".json", &begin); e != nil {
			return nil, e
		}
		st, e := os.Stat(path)
		if e != nil {
			return nil, e
		}
		if st.Size() != begin.Size {
			return nil, fmt.Errorf("文件尚未传完整")
		}
		a, e := s.ImportFile(path, begin.Label)
		if e == nil {
			_ = os.Remove(path)
			_ = os.Remove(path + ".json")
		}
		return a, e
	case "cleanup_uploads":
		count, e := s.cleanupUploads(time.Now(), true)
		return map[string]int{"removed": count}, e
	case "upload_cancel":
		if !tokenPattern.MatchString(r.Token) {
			return nil, fmt.Errorf("导入令牌无效")
		}
		_ = os.Remove(s.data("uploads", r.Token))
		return map[string]bool{"cancelled": true}, removeIfExists(s.data("uploads", r.Token+".json"))
	case "delete_font":
		if !assetPattern.MatchString(r.ID) {
			return nil, fmt.Errorf("字体 ID 无效")
		}
		refs := []Profile{}
		var draft Profile
		if readJSON(s.data("draft.json"), &draft) == nil {
			refs = append(refs, draft)
		}
		for _, pointer := range []string{"active", "pending"} {
			g, e := s.generation(pointer)
			if e != nil {
				return nil, e
			}
			if g != nil {
				refs = append(refs, g.Profile)
			}
		}
		for _, p := range refs {
			b, _ := json.Marshal(p)
			if strings.Contains(string(b), r.ID) {
				return nil, fmt.Errorf("字体仍被当前、待生效或草稿配置使用")
			}
		}
		if e := removeIfExists(s.data("library", r.ID+".font")); e != nil {
			return nil, e
		}
		return map[string]bool{"deleted": true}, removeIfExists(s.data("library", r.ID+".json"))
	case "diagnostics":
		v, e := s.Status()
		if e != nil {
			return nil, e
		}
		m := v.(map[string]any)
		delete(m, "library")
		delete(m, "draft")
		active, _ := s.generation("active")
		pending, _ := s.generation("pending")
		boot := m["boot"].(BootRecord)
		selected := active
		if pending != nil && pending.ID == boot.Generation {
			selected = pending
		}
		m["currentFingerprint"] = s.fingerprint()
		m["currentConfigChecks"] = s.configObservations(selected)
		return m, nil
	default:
		return nil, fmt.Errorf("未知操作")
	}
}
func removeIfExists(p string) error {
	e := os.Remove(p)
	if os.IsNotExist(e) {
		return nil
	}
	return e
}

func exportDownload(kind string, v any) (any, error) {
	dir, e := filepath.EvalSymlinks("/sdcard/Download")
	if e != nil {
		return nil, e
	}
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return nil, e
	}
	path := filepath.Join(dir, "HyperFont-"+kind+"-"+time.Now().Format("20060102-150405")+"-"+token()[:6]+".json")
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if e != nil {
		return nil, e
	}
	if _, e = f.Write(b); e != nil {
		f.Close()
		return nil, e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return nil, e
	}
	e = f.Close()
	return map[string]string{"path": "/sdcard/Download/" + filepath.Base(path)}, e
}
