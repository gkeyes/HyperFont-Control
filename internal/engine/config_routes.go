package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func xiaomiTables(path string) []string {
	switch path {
	case "/system/etc/fonts.xml":
		return []string{"/system_ext/etc/hyper_fonts.xml", "/system_ext/etc/miui_fonts.xml"}
	case "/system/etc/font_fallback.xml":
		return []string{"/system_ext/etc/hyper_font_fallback.xml", "/system_ext/etc/miui_font_fallback.xml"}
	}
	return nil
}

func (s *Store) logicalLink(path string) (string, error) {
	target, err := os.Readlink(s.rom(path))
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(s.rom(path)), target)
	}
	target = filepath.Clean(target)
	if s.RomRoot != "" && s.RomRoot != "/" {
		root := filepath.Clean(s.RomRoot)
		if strings.HasPrefix(target, root+string(filepath.Separator)) {
			target = "/" + strings.TrimPrefix(target, root+string(filepath.Separator))
		}
	}
	return target, nil
}

// Xiaomi's immutable /system aliases select one of two separately verified
// system_ext tables through a system-owned theme_webview link. Never bind or
// rewrite the selector; doing so stacks a second mount on the selected table.
// A missing intermediate link may be deferred only during early boot. Both
// physical tables must still pass their independent baseline/file checks.
func (s *Store) configSource(path string, present func(string) bool, deferMissing bool) (source string, selector bool, err error) {
	pair := xiaomiTables(path)
	if len(pair) == 0 || !present(pair[0]) || !present(pair[1]) {
		return path, false, nil
	}
	info, err := os.Lstat(s.rom(path))
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return path, false, err
	}
	for _, physical := range pair {
		st, e := os.Stat(s.rom(physical))
		if e != nil || !st.Mode().IsRegular() {
			return "", true, fmt.Errorf("原生字体表不可用: %s", physical)
		}
	}
	target, err := s.logicalLink(path)
	if err != nil {
		return "", true, err
	}
	intermediate := "/data/system/fonts/theme_webview/" + filepath.Base(path)
	if target == intermediate {
		target, err = s.logicalLink(intermediate)
		if os.IsNotExist(err) && deferMissing {
			return "", true, nil
		}
		if err != nil {
			return "", true, fmt.Errorf("系统字体选择链接尚不可用: %s: %w", path, err)
		}
	}
	for _, physical := range pair {
		if target == physical {
			return physical, true, nil
		}
	}
	return "", true, fmt.Errorf("系统字体入口指向未知配置: %s -> %s", path, target)
}

func generationTargets(g Generation) map[string]Target {
	byPath := map[string]Target{}
	for _, t := range g.Targets {
		byPath[t.Path] = t
	}
	return byPath
}

func (s *Store) configMountTargets(g Generation) ([]Target, error) {
	byPath := generationTargets(g)
	present := func(path string) bool { _, ok := byPath[path]; return ok }
	result := []Target{}
	for _, target := range g.Targets {
		_, selector, err := s.configSource(target.Path, present, true)
		if err != nil {
			return nil, err
		}
		if !selector {
			result = append(result, target)
		}
	}
	return result, nil
}

type ConfigObservation struct {
	Path        string `json:"path"`
	LinkTarget  string `json:"linkTarget,omitempty"`
	Source      string `json:"source,omitempty"`
	BaselineSHA string `json:"baselineSha256,omitempty"`
	ExpectedSHA string `json:"expectedSha256,omitempty"`
	ActualSHA   string `json:"actualSha256,omitempty"`
	State       string `json:"state"`
	Error       string `json:"error,omitempty"`
}

func (s *Store) configObservations(g *Generation) []ConfigObservation {
	var base Baseline
	_ = readJSON(s.data("baseline.json"), &base)
	targets := map[string]Target{}
	if g != nil {
		targets = generationTargets(*g)
	}
	present := func(path string) bool { _, ok := base.Configs[path]; return ok }
	paths := []string{}
	for path := range base.Configs {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	result := make([]ConfigObservation, 0, len(paths))
	for _, path := range paths {
		row := ConfigObservation{Path: path, State: "unreadable"}
		row.LinkTarget, _ = s.logicalLink(path)
		source, _, routeErr := s.configSource(path, present, false)
		if source == "" {
			source = path
		}
		row.Source = source
		row.BaselineSHA = base.Configs[source]
		row.ExpectedSHA = targets[source].SHA
		actual, err := hashFile(s.rom(path))
		row.ActualSHA = actual
		if err != nil {
			row.Error = err.Error()
		} else if routeErr != nil {
			row.Error = routeErr.Error()
			row.State = "unknown-selector"
		} else {
			switch {
			case actual == row.ExpectedSHA:
				row.State = "generated"
			case actual == row.BaselineSHA:
				row.State = "original"
			default:
				row.State = "different"
			}
		}
		result = append(result, row)
	}
	return result
}
