package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func xiaomiSelectorFixture(t *testing.T) (*Store, Generation) {
	t.Helper()
	s, asset, _ := fixtureStore(t)
	for _, pair := range [][3]string{{"fonts.xml", "hyper_fonts.xml", "miui_fonts.xml"}, {"font_fallback.xml", "hyper_font_fallback.xml", "miui_font_fallback.xml"}} {
		alias := s.rom("/system/etc/" + pair[0])
		raw, err := os.ReadFile(alias)
		if err != nil {
			t.Fatal(err)
		}
		for i, name := range pair[1:] {
			data := strings.Replace(string(raw), "<familyset", "<!-- native variant "+name+" -->\n<familyset", 1)
			if i == 1 {
				data = strings.Replace(data, `stylevalue="330"`, `stylevalue="400"`, 1)
			}
			if err = writeAtomic(s.rom("/system_ext/etc/"+name), []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
		}
		selector := s.rom("/data/system/fonts/theme_webview/" + pair[0])
		if err = os.MkdirAll(filepath.Dir(selector), 0700); err != nil {
			t.Fatal(err)
		}
		target, _ := filepath.Rel(filepath.Dir(selector), s.rom("/system_ext/etc/"+pair[1]))
		if err = os.Symlink(target, selector); err != nil {
			t.Fatal(err)
		}
		if err = os.Remove(alias); err != nil {
			t.Fatal(err)
		}
		target, _ = filepath.Rel(filepath.Dir(alias), selector)
		if err = os.Symlink(target, alias); err != nil {
			t.Fatal(err)
		}
	}
	g, err := s.Stage(variableProfile(asset))
	if err != nil {
		t.Fatal(err)
	}
	return s, g
}

func selectMiui(t *testing.T, s *Store) {
	t.Helper()
	for _, pair := range [][2]string{{"fonts.xml", "miui_fonts.xml"}, {"font_fallback.xml", "miui_font_fallback.xml"}} {
		selector := s.rom("/data/system/fonts/theme_webview/" + pair[0])
		if err := os.Remove(selector); err != nil {
			t.Fatal(err)
		}
		target, _ := filepath.Rel(filepath.Dir(selector), s.rom("/system_ext/etc/"+pair[1]))
		if err := os.Symlink(target, selector); err != nil {
			t.Fatal(err)
		}
	}
}

func TestXiaomiSelectorSwitchIsNotROMChange(t *testing.T) {
	s, g := xiaomiSelectorFixture(t)
	selectMiui(t, s)
	if err := s.verifyGeneration(g); err != nil {
		t.Fatalf("unchanged native files, legitimate selector switch: %v", err)
	}
}

func TestXiaomiSelectorStillRejectsChangedPhysicalTable(t *testing.T) {
	s, g := xiaomiSelectorFixture(t)
	selectMiui(t, s)
	p := s.rom("/system_ext/etc/miui_font_fallback.xml")
	if err := os.WriteFile(p, []byte(`<familyset/>`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.verifyGeneration(g); err == nil {
		t.Fatal("accepted modified physical system table")
	}
}

func exposeFixtureGeneration(t *testing.T, s *Store, g Generation) {
	t.Helper()
	plan, err := s.configMountTargets(g)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range plan {
		if len(xiaomiTables(target.Path)) > 0 {
			t.Fatal("must not mount a mutable selector", target.Path)
		}
		data, err := os.ReadFile(s.data("generations", g.ID, target.File))
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(s.rom(target.Path), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for name := range g.Fonts {
		data, err := os.ReadFile(s.data("generations", g.ID, "fonts", name))
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(s.rom("/system/fonts/"+name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestXiaomiVisibilityFollowsBothPhysicalTables(t *testing.T) {
	s, g := xiaomiSelectorFixture(t)
	plan, err := s.configMountTargets(g)
	if err != nil || len(plan) != 4 {
		t.Fatal("expected four physical tables", len(plan), err)
	}
	exposeFixtureGeneration(t, s, g)
	if err = s.verifyVisible(g); err != nil {
		t.Fatal("hyper selector", err)
	}
	selectMiui(t, s)
	if err = s.verifyVisible(g); err != nil {
		t.Fatal("miui selector", err)
	}
	if err = os.WriteFile(s.rom("/system_ext/etc/miui_fonts.xml"), []byte("different"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = s.verifyVisible(g); err == nil {
		t.Fatal("false visible state")
	}
}

func TestXiaomiEarlyBootCanWaitForSelectorCreation(t *testing.T) {
	s, g := xiaomiSelectorFixture(t)
	for _, name := range []string{"fonts.xml", "font_fallback.xml"} {
		if err := os.Remove(s.rom("/data/system/fonts/theme_webview/" + name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.verifyGeneration(g); err != nil {
		t.Fatal("known selector absent before Android initializes it", err)
	}
	exposeFixtureGeneration(t, s, g)
	if err := s.verifyVisibleAtBoot(g, true); err != nil {
		t.Fatal(err)
	}
	if err := s.verifyVisible(g); err == nil {
		t.Fatal("must not claim alias visible before it exists")
	}
	for _, pair := range [][2]string{{"fonts.xml", "miui_fonts.xml"}, {"font_fallback.xml", "miui_font_fallback.xml"}} {
		link := s.rom("/data/system/fonts/theme_webview/" + pair[0])
		target, _ := filepath.Rel(filepath.Dir(link), s.rom("/system_ext/etc/"+pair[1]))
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.verifyVisible(g); err != nil {
		t.Fatal("framework-created selector", err)
	}
}

func TestXiaomiUnknownSelectorIsRejected(t *testing.T) {
	s, g := xiaomiSelectorFixture(t)
	link := s.rom("/data/system/fonts/theme_webview/font_fallback.xml")
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../unexpected-fonts.xml", link); err != nil {
		t.Fatal(err)
	}
	if err := s.verifyGeneration(g); err == nil {
		t.Fatal("unknown selector target accepted")
	}
}

func TestFontDiagnosticsIncludesObservedHashesAndSelector(t *testing.T) {
	s, g := xiaomiSelectorFixture(t)
	selectMiui(t, s)
	if err := writeJSON(s.data("boot.json"), BootRecord{Generation: g.ID, State: "failed"}); err != nil {
		t.Fatal(err)
	}
	v, err := s.Handle(Request{Op: "diagnostics"})
	if err != nil {
		t.Fatal(err)
	}
	d := v.(map[string]any)
	if len(d["baseline"].(Baseline).Configs) != 6 || d["currentFingerprint"] != "host-fixture" {
		t.Fatal("diagnostics lost system evidence")
	}
	for _, row := range d["currentConfigChecks"].([]ConfigObservation) {
		if row.Path == "/system/etc/font_fallback.xml" {
			if row.Source != "/system_ext/etc/miui_font_fallback.xml" || row.ActualSHA == "" || row.BaselineSHA != row.ActualSHA || row.ExpectedSHA == "" || row.State != "original" {
				t.Fatalf("bad observed selector: %+v", row)
			}
			return
		}
	}
	t.Fatal("missing selector diagnostics")
}
