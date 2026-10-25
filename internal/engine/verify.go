package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (s *Store) verifyGeneration(g Generation) error {
	if !tokenPattern.MatchString(g.ID) {
		return fmt.Errorf("无效生成 ID")
	}
	if _, e := s.Validate(g.Profile); e != nil {
		return e
	}
	if g.Profile.IsSystem() {
		if len(g.Targets)+len(g.Fonts) != 0 {
			return fmt.Errorf("恢复配置不应包含任何覆盖文件")
		}
		return nil
	}
	var base Baseline
	if e := readJSON(s.data("baseline.json"), &base); e != nil {
		return e
	}
	if g.Fingerprint != s.fingerprint() || base.Fingerprint != g.Fingerprint {
		return fmt.Errorf("系统版本已变化，保留原生字体；请重新识别系统")
	}
	for path, h := range base.Configs {
		_, selector, routeErr := s.configSource(path, func(p string) bool { _, ok := base.Configs[p]; return ok }, true)
		if routeErr != nil {
			return routeErr
		}
		if selector {
			generated := generationTargets(g)
			for _, physical := range xiaomiTables(path) {
				if _, ok := generated[physical]; !ok {
					return fmt.Errorf("缺少原生字体入口的生成配置，请重新提交: %s", physical)
				}
			}
			continue
		}
		current, e := hashFile(s.rom(path))
		if e != nil || current != h {
			return fmt.Errorf("系统字体表已变化: %s", path)
		}
	}
	for path, h := range g.Dependencies {
		original, e := s.originalFont(path)
		if e != nil {
			return e
		}
		current, e := hashFile(original)
		if e != nil || current != h {
			return fmt.Errorf("原生字体已变化: %s", path)
		}
	}
	for _, t := range g.Targets {
		valid := false
		for _, p := range ConfigPaths {
			if p == t.Path {
				valid = true
			}
		}
		if !valid || t.File != filepath.Join("config", strings.TrimPrefix(t.Path, "/")) {
			return fmt.Errorf("生成清单含有越界路径")
		}
		actual, e := hashFile(s.data("generations", g.ID, t.File))
		if e != nil || actual != t.SHA {
			return fmt.Errorf("生成配置校验失败")
		}
		raw, e := os.ReadFile(s.data("generations", g.ID, t.File))
		if e != nil {
			return e
		}
		doc, e := parseXML(raw)
		if e != nil {
			return e
		}
		if e = validateAndroidFontXML(doc); e != nil {
			return fmt.Errorf("生成字体表不符合 Android 解析规则，拒绝挂载: %s: %w", t.Path, e)
		}
		var check func(*node) error
		check = func(n *node) error {
			if n.Name.Local == "font" {
				name := strings.TrimSpace(n.Text)
				if strings.HasPrefix(name, "HFC-") {
					if _, ok := g.Fonts[name]; !ok {
						return fmt.Errorf("字体表引用了清单外的生成字体")
					}
				}
			}
			for _, child := range n.Children {
				if e := check(child); e != nil {
					return e
				}
			}
			return nil
		}
		if e = check(doc); e != nil {
			return e
		}
	}
	for name, h := range g.Fonts {
		if name != "HFC-"+h+".ttf" || !assetPattern.MatchString(h) {
			return fmt.Errorf("生成字体路径无效")
		}
		actual, e := hashFile(s.data("generations", g.ID, "fonts", name))
		if e != nil || actual != h {
			return fmt.Errorf("生成字体校验失败")
		}
	}
	return nil
}
func (s *Store) verifyVisible(g Generation) error {
	return s.verifyVisibleAtBoot(g, false)
}
func (s *Store) verifyVisibleAtBoot(g Generation, deferMissing bool) error {
	targets := generationTargets(g)
	for _, t := range g.Targets {
		source, selector, err := s.configSource(t.Path, func(path string) bool { _, ok := targets[path]; return ok }, deferMissing)
		if err != nil {
			return err
		}
		if selector && source == "" && deferMissing {
			continue
		}
		want := t.SHA
		if selector {
			want = targets[source].SHA
		}
		h, e := hashFile(s.rom(t.Path))
		if e != nil || h != want {
			return fmt.Errorf("字体表覆盖不可见: %s", t.Path)
		}
	}
	type checkedFont struct {
		info os.FileInfo
		hash string
	}
	checked := []checkedFont{}
	for name, want := range g.Fonts {
		for _, dir := range []string{"/system/fonts", "/product/fonts"} {
			if _, e := os.Stat(s.rom(dir)); os.IsNotExist(e) {
				continue
			}
			path := s.rom(filepath.Join(dir, name))
			st, e := os.Stat(path)
			if e != nil || !st.Mode().IsRegular() || st.Size() == 0 {
				return fmt.Errorf("字体路径不可见: %s", name)
			}
			actual := ""
			for _, prior := range checked {
				if os.SameFile(st, prior.info) {
					actual = prior.hash
					break
				}
			}
			if actual == "" {
				actual, e = hashFile(path)
				if e != nil {
					return fmt.Errorf("无法读取覆盖字体: %s: %w", path, e)
				}
				checked = append(checked, checkedFont{st, actual})
			}
			if actual != want {
				return fmt.Errorf("覆盖字体内容校验失败: %s", path)
			}
		}
	}
	return nil
}
