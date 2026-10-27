package engine

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Small structural font for parser/compiler boundary tests. Real fonts are
// exercised separately by TestRealFonts when HFC_REAL_FONTS is supplied.
func fixtureFont(variable bool) []byte {
	t := map[string]fontData{}
	head := make([]byte, 54)
	put16(head, 18, 1000)
	t["head"] = head
	maxp := make([]byte, 32)
	put32(maxp, 0, 65536)
	put16(maxp, 4, 16)
	t["maxp"] = maxp
	hhea := make([]byte, 36)
	put16(hhea, 4, 1000)
	put16(hhea, 6, -250)
	put16(hhea, 34, 16)
	t["hhea"] = hhea
	t["hmtx"] = make([]byte, 64)
	t["glyf"] = make([]byte, 16)
	t["loca"] = make([]byte, 34)
	os2 := make([]byte, 96)
	put16(os2, 4, 400)
	t["OS/2"] = os2
	t["name"] = make([]byte, 6)
	t["cmap"] = makeCmap(map[uint32]uint16{'A': 1, 'a': 2, '0': 3, 0x4e2d: 4, 0x6587: 5, 0x1f600: 6, 0xe000: 7}, "all")
	if variable {
		a := make([]byte, 36)
		put32(a, 0, 65536)
		put16(a, 4, 16)
		put16(a, 8, 1)
		put16(a, 10, 20)
		copy(a[16:], "wght")
		put32(a, 20, 100*65536)
		put32(a, 24, 400*65536)
		put32(a, 28, 900*65536)
		t["fvar"] = a
	}
	out := make([]byte, 12+len(t)*16)
	put32(out, 0, 65536)
	put16(out, 4, len(t))
	i := 0
	for tag, v := range t {
		p := 12 + i*16
		copy(out[p:], tag)
		put32(out, p+8, len(out))
		put32(out, p+12, len(v))
		out = append(out, v...)
		for len(out)%4 != 0 {
			out = append(out, 0)
		}
		i++
	}
	return out
}
func fixtureStore(t *testing.T) (*Store, Asset, Asset) {
	t.Helper()
	dir := t.TempDir()
	s := &Store{Root: filepath.Join(dir, "data"), RomRoot: filepath.Join(dir, "rom"), Runtime: filepath.Join(dir, "runtime")}
	xml := `<familyset version="23"><family name="sans-serif"><font weight="400">Stock.ttf<axis tag="wght" stylevalue="330"/></font></family><family name="serif"><font weight="400">Stock.ttf</font></family><family name="monospace"><font weight="400">Stock.ttf</font></family><family lang="zh-Hans"><font weight="400">Stock.ttf</font></family><family lang="und-Zsye"><font>Emoji.ttf</font></family><alias name="mipro-normal" to="sans-serif" weight="350"/><alias name="sans-serif-light" to="sans-serif" weight="300"/></familyset>`
	for _, path := range []string{"/system/etc/fonts.xml", "/system/etc/font_fallback.xml"} {
		if e := writeAtomic(s.rom(path), []byte(xml), 0600); e != nil {
			t.Fatal(e)
		}
	}
	stock := fixtureFont(true)
	if e := writeAtomic(s.rom("/system/fonts/Stock.ttf"), stock, 0600); e != nil {
		t.Fatal(e)
	}
	a, e := s.ImportFile(s.rom("/system/fonts/Stock.ttf"), "Variable")
	if e != nil {
		t.Fatal(e)
	}
	static := s.rom("/system/fonts/Static.ttf")
	if e = writeAtomic(static, fixtureFont(false), 0600); e != nil {
		t.Fatal(e)
	}
	b, e := s.ImportFile(static, "Static")
	if e != nil {
		t.Fatal(e)
	}
	return s, a, b
}
func variableProfile(a Asset) Profile {
	p := DefaultProfile()
	p.Name = "Test"
	p.Slots["chinese"] = Slot{Mode: "variable", AssetID: a.ID, FamilyMode: "instances", WeightMap: map[string]float64{"100": 100, "200": 200, "300": 350, "400": 450, "500": 500, "600": 600, "700": 700, "800": 800, "900": 900}}
	return p
}
func TestFontParseAndFiltering(t *testing.T) {
	b := fixtureFont(true)
	f, e := ParseFont(b)
	if e != nil || len(f) != 1 || !f[0].Chinese || !f[0].Latin || len(f[0].Axes) != 1 {
		t.Fatalf("%+v %v", f, e)
	}
	for _, filter := range []string{"cjk", "latin", "without-cjk"} {
		out, e := RewriteFont(b, 0, filter)
		if e != nil {
			t.Fatal(e)
		}
		if checksum(out) != 0xB1B0AFBA {
			t.Fatal("checksum")
		}
		orig := tables(fontData(b), 0)
		now := tables(fontData(out), 0)
		for _, tag := range []string{"glyf", "hmtx", "hhea", "fvar"} {
			if !bytes.Equal(orig[tag], now[tag]) {
				t.Fatalf("%s changed", tag)
			}
		}
		cm := charmap(now)
		if filter == "cjk" && (cm['A'] != 0 || cm[0xe000] != 0 || cm[0x1f600] != 0 || cm[0x4e2d] == 0) {
			t.Fatal("CJK leaks")
		}
		if filter == "without-cjk" && (cm[0x4e2d] != 0 || cm['A'] == 0 || cm[0xe000] == 0) {
			t.Fatal("stock filter")
		}
	}
	for _, bad := range [][]byte{nil, []byte("wOF2"), b[:24], bytes.Repeat([]byte{255}, 128)} {
		if _, e := ParseFont(bad); e == nil {
			t.Fatal("accepted malformed font")
		}
	}
}
func TestVariableValidation(t *testing.T) {
	s, a, _ := fixtureStore(t)
	p := variableProfile(a)
	if _, e := s.Validate(p); e != nil {
		t.Fatal(e)
	}
	v := p.Slots["chinese"]
	v.WeightMap["400"] = 901
	if _, e := s.Validate(p); e == nil {
		t.Fatal("out of range")
	}
	v.WeightMap["400"] = 200
	if _, e := s.Validate(p); e == nil {
		t.Fatal("non monotonic")
	}
	v.WeightMap["400"] = 450
	v.FamilyMode = "continuous"
	p.Slots["chinese"] = v
	if _, e := s.Validate(p); e == nil {
		t.Fatal("continuous with map")
	}
	delete(p.Slots, "chinese")
	if _, e := s.Validate(p); e == nil {
		t.Fatal("missing slot")
	}
}
func TestStaticValidation(t *testing.T) {
	s, _, a := fixtureStore(t)
	p := DefaultProfile()
	v := Slot{Mode: "static", MissingWeightPolicy: "nearest", Faces: []FaceRef{{AssetID: a.ID, Weight: 400, Style: "normal"}}}
	p.Slots["chinese"] = v
	if _, e := s.Validate(p); e != nil {
		t.Fatal(e)
	}
	v.MissingWeightPolicy = "reject"
	p.Slots["chinese"] = v
	if _, e := s.Validate(p); e == nil {
		t.Fatal("accepted missing weights")
	}
	v.MissingWeightPolicy = "nearest"
	v.Faces = append(v.Faces, v.Faces[0])
	p.Slots["chinese"] = v
	if _, e := s.Validate(p); e == nil {
		t.Fatal("duplicate style weight")
	}
}
func TestStagePreservesStockAndAliases(t *testing.T) {
	s, a, _ := fixtureStore(t)
	p := variableProfile(a)
	original, _ := os.ReadFile(s.rom("/system/etc/fonts.xml"))
	g, e := s.Stage(p)
	if e != nil {
		t.Fatal(e)
	}
	unchanged, _ := os.ReadFile(s.rom("/system/etc/fonts.xml"))
	if !bytes.Equal(original, unchanged) {
		t.Fatal("stage wrote ROM")
	}
	if len(g.Targets) != 2 || len(g.Fonts) < 2 {
		t.Fatalf("incomplete generation %+v", g)
	}
	out, e := os.ReadFile(s.data("generations", g.ID, "config/system/etc/fonts.xml"))
	if e != nil {
		t.Fatal(e)
	}
	doc, e := parseXML(out)
	if e != nil {
		t.Fatal(e)
	}
	enc, _ := encodeXML(doc)
	for _, needle := range []string{`name="mipro-normal" to="sans-serif" weight="400"`, `name="sans-serif-light" to="sans-serif" weight="300"`, `<font>Emoji.ttf</font>`, `stylevalue="450"`} {
		if !strings.Contains(string(enc), needle) {
			t.Fatalf("lost %s", needle)
		}
	}
	if e = s.verifyGeneration(g); e != nil {
		t.Fatal(e)
	}
	target := s.data("generations", g.ID, g.Targets[0].File)
	_ = os.WriteFile(target, []byte("corrupt"), 0600)
	if e = s.verifyGeneration(g); e == nil {
		t.Fatal("corrupt generation accepted")
	}
}
func TestContinuousAndStaticXML(t *testing.T) {
	s, a, b := fixtureStore(t)
	for _, mode := range []string{"continuous", "static"} {
		p := DefaultProfile()
		if mode == "continuous" {
			p.Slots["latinSans"] = Slot{Mode: "variable", AssetID: a.ID, FamilyMode: "continuous"}
		} else {
			p.Slots["latinSans"] = Slot{Mode: "static", MissingWeightPolicy: "nearest", Faces: []FaceRef{{AssetID: b.ID, Weight: 400, Style: "normal"}}}
		}
		g, e := s.Stage(p)
		if e != nil {
			t.Fatal(e)
		}
		legacy, _ := os.ReadFile(s.data("generations", g.ID, "config/system/etc/fonts.xml"))
		modern, _ := os.ReadFile(s.data("generations", g.ID, "config/system/etc/font_fallback.xml"))
		if strings.Contains(string(legacy), "supportedAxes") {
			t.Fatal("modern declaration in legacy")
		}
		if mode == "continuous" && !strings.Contains(string(modern), `supportedAxes="wght"`) {
			t.Fatal("missing continuous font attribute")
		}
		if mode == "static" && strings.Contains(string(modern), "HFC-") && strings.Contains(string(modern), `stylevalue="450"`) {
			t.Fatal("forged static axis")
		}
	}
}
func TestBadStageRetainsPendingAndRestore(t *testing.T) {
	s, a, _ := fixtureStore(t)
	g, e := s.Stage(variableProfile(a))
	if e != nil {
		t.Fatal(e)
	}
	bad := variableProfile(a)
	v := bad.Slots["chinese"]
	v.AssetID = strings.Repeat("a", 64)
	bad.Slots["chinese"] = v
	if _, e = s.Stage(bad); e == nil {
		t.Fatal("missing asset accepted")
	}
	pending, _ := s.generation("pending")
	if pending.ID != g.ID {
		t.Fatal("pending overwritten on failure")
	}
	r, e := s.Stage(DefaultProfile())
	if e != nil || len(r.Fonts)+len(r.Targets) != 0 {
		t.Fatalf("restore %+v %v", r, e)
	}
}
func TestOEMIsTargeted(t *testing.T) {
	s, a, _ := fixtureStore(t)
	xml := `<fonts-modification><family customizationType="new-locale-family" operation="prepend" lang="en"><font supportedAxes="wght">MiSansLatinVF.ttf</font></family><family customizationType="new-locale-family" operation="prepend" lang="zh-Hans"><font supportedAxes="wght">MiSansVF.ttf</font></family><family customizationType="new-locale-family" operation="prepend" lang="ja"><font supportedAxes="wght">MiSansJapaneseVF.ttf</font></family></fonts-modification>`
	c := compiler{s, t.TempDir(), map[string]string{}, map[string]string{}, map[string]bool{}}
	out, n, e := c.compileXML("/product/etc/misans_variable_fonts.xml", []byte(xml), variableProfile(a))
	if e != nil || n != 1 {
		t.Fatalf("%d %v", n, e)
	}
	for _, x := range []string{"MiSansLatinVF.ttf", "MiSansJapaneseVF.ttf"} {
		if !strings.Contains(string(out), x) {
			t.Fatal("untargeted language lost")
		}
	}
	if strings.Contains(string(out), ">MiSansVF.ttf<") {
		t.Fatal("CJK not replaced")
	}
}
func TestTraversalAndUnknownCommands(t *testing.T) {
	s, _, _ := fixtureStore(t)
	for _, r := range []Request{{Op: "delete_font", ID: "../../owner"}, {Op: "upload_chunk", Token: "../../owner"}, {Op: "upload_cancel", Token: "../x"}, {Op: "exec", Path: "rm"}} {
		if _, e := s.Handle(r); e == nil {
			t.Fatalf("accepted %+v", r)
		}
	}
	if _, e := parseXML([]byte(`<!DOCTYPE x SYSTEM "file:///etc/passwd"><familyset/>`)); e == nil {
		t.Fatal("DTD accepted")
	}
	if e := strictJSON([]byte(`{"op":"status","shell":"x"}`), &Request{}); e == nil {
		t.Fatal("unknown field")
	}
}
func TestLibraryDeleteReferences(t *testing.T) {
	s, a, _ := fixtureStore(t)
	g, e := s.Stage(variableProfile(a))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Handle(Request{Op: "delete_font", ID: a.ID}); e == nil {
		t.Fatal("deleted pending asset")
	}
	_ = g
	if _, e = s.Handle(Request{Op: "cancel_pending"}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Handle(Request{Op: "delete_font", ID: a.ID}); e == nil {
		t.Fatal("deleted draft asset")
	}
}
func TestRealFonts(t *testing.T) {
	dir := os.Getenv("HFC_REAL_FONTS")
	if dir == "" {
		t.Skip("set HFC_REAL_FONTS for real-font checks")
	}
	for _, name := range []string{"MiSansVF.ttf", "MiSansLatinVF.ttf", "xhs-font_normal.ttf"} {
		t.Run(name, func(t *testing.T) {
			b, e := os.ReadFile(filepath.Join(dir, name))
			if e != nil {
				t.Fatal(e)
			}
			f, e := ParseFont(b)
			if e != nil {
				t.Fatal(e)
			}
			raw, _ := json.Marshal(f)
			t.Log(string(raw))
			out, e := RewriteFont(b, 0, "cjk")
			if e != nil {
				t.Fatal(e)
			}
			if checksum(out) != 0xB1B0AFBA {
				t.Fatal("checksum")
			}
			orig := tables(fontData(b), offsets(fontData(b))[0])
			now := tables(fontData(out), 0)
			for _, tag := range []string{"glyf", "hmtx", "hhea", "OS/2", "gvar", "CFF ", "CFF2"} {
				if !bytes.Equal(orig[tag], now[tag]) {
					t.Fatalf("changed %s", tag)
				}
			}
		})
	}
}

func TestCollectionFaceExtraction(t *testing.T) {
	a, b := fixtureFont(false), fixtureFont(false)
	tb := tables(fontData(b), 0)
	put16(tb["OS/2"], 4, 700)
	out := make([]byte, 20)
	copy(out, "ttcf")
	put32(out, 4, 65536)
	put32(out, 8, 2)
	for i, face := range [][]byte{a, b} {
		off := len(out)
		put32(out, 12+i*4, off)
		part := bytes.Clone(face)
		n := fontData(part).u16(4)
		for j := 0; j < n; j++ {
			p := 12 + j*16 + 8
			put32(part, p, fontData(part).u32(p)+off)
		}
		out = append(out, part...)
	}
	faces, e := ParseFont(out)
	if e != nil || len(faces) != 2 || faces[1].Weight != 700 {
		t.Fatalf("TTC: %+v %v", faces, e)
	}
	extracted, e := RewriteFont(out, 1, "cjk")
	if e != nil {
		t.Fatal(e)
	}
	actual, e := ParseFont(extracted)
	if e != nil || actual[0].Weight != 700 {
		t.Fatalf("wrong face: %+v %v", actual, e)
	}
}

func TestChineseRangeKeepsOtherScripts(t *testing.T) {
	for _, c := range []uint32{0x3042, 0x30A2, 0x3131, 0xAC00, 0xFF76, 0xFFA1, 0xE000, 0x1F600, 'A'} {
		if isCJK(c) {
			t.Fatalf("Chinese slot captures U+%04X", c)
		}
	}
	for _, c := range []uint32{0x4E2D, 0x3002, 0xFF0C, 0x3105, 0x3400, 0x20000} {
		if !isCJK(c) {
			t.Fatalf("missed U+%04X", c)
		}
	}
}

func TestSystemRestoreAfterROMChange(t *testing.T) {
	s, _, _ := fixtureStore(t)
	g, e := s.Stage(DefaultProfile())
	if e != nil {
		t.Fatal(e)
	}
	_ = os.WriteFile(s.rom("/system/etc/fonts.xml"), []byte("new-ROM"), 0600)
	if e = s.verifyGeneration(g); e != nil {
		t.Fatalf("restore must not depend on old ROM: %v", e)
	}
}

func TestGenerationRetention(t *testing.T) {
	s, _, _ := fixtureStore(t)
	first, e := s.Stage(DefaultProfile())
	if e != nil {
		t.Fatal(e)
	}
	_ = writeAtomic(s.data("active"), []byte(first.ID), 0600)
	for i := 0; i < 5; i++ {
		if _, e = s.Stage(DefaultProfile()); e != nil {
			t.Fatal(e)
		}
	}
	entries, e := os.ReadDir(s.data("generations"))
	if e != nil {
		t.Fatal(e)
	}
	if len(entries) > 3 {
		t.Fatalf("unbounded generations: %d", len(entries))
	}
	if _, e = os.Stat(s.data("generations", first.ID)); e != nil {
		t.Fatal("active was pruned")
	}
}

func TestXiaomiRobotoFallbackDoesNotOverrideSelectedLatin(t *testing.T) {
	s, a, _ := fixtureStore(t)
	stock := fixtureFont(true)
	if e := writeAtomic(s.rom("/system/fonts/Roboto-Regular.ttf"), stock, 0600); e != nil {
		t.Fatal(e)
	}
	x := `<familyset><family name="sans-serif"><font>Roboto-Regular.ttf</font></family><family><font supportedAxes="wght,ital">Roboto-Regular.ttf<axis tag="wdth" stylevalue="100"/></font></family><family name="roboto"><font>Roboto-Regular.ttf</font></family></familyset>`
	if e := writeAtomic(s.rom("/system/etc/fonts.xml"), []byte(x), 0600); e != nil {
		t.Fatal(e)
	}
	p := DefaultProfile()
	p.Slots["latinSans"] = Slot{Mode: "variable", AssetID: a.ID, FamilyMode: "continuous"}
	g, e := s.Stage(p)
	if e != nil {
		t.Fatal(e)
	}
	out, e := os.ReadFile(s.data("generations", g.ID, "config/system/etc/fonts.xml"))
	if e != nil {
		t.Fatal(e)
	}
	r, e := parseXML(out)
	if e != nil {
		t.Fatal(e)
	}
	var check func(*node)
	check = func(n *node) {
		if n.Name.Local == "family" && n.attr("name") == "" {
			for _, f := range n.Children {
				if f.Name.Local == "font" && strings.TrimSpace(f.Text) == "Roboto-Regular.ttf" {
					t.Fatal("ROM would register another sans-serif")
				}
			}
		}
		for _, child := range n.Children {
			check(child)
		}
	}
	check(r)
	alias := "HFC-" + digest(stock) + ".ttf"
	b, e := os.ReadFile(s.data("generations", g.ID, "fonts", alias))
	if e != nil || !bytes.Equal(b, stock) {
		t.Fatal("stock fallback bytes were not preserved", e)
	}
	if namedSlot("roboto") != "latinSans" {
		t.Fatal("explicit roboto route not covered")
	}
}

func TestUpgradeAddsMissingMiCustomizationBaselineOnly(t *testing.T) {
	s, a, _ := fixtureStore(t)
	base, e := s.Init()
	if e != nil {
		t.Fatal(e)
	}
	before := base.Configs["/system/etc/fonts.xml"]
	// Simulate the old generation being mounted while the added file is stock.
	_ = os.WriteFile(s.rom("/system/etc/fonts.xml"), []byte(`<familyset><!-- HFC-old --></familyset>`), 0600)
	mi := `<fonts-modification><family customizationType="new-locale-family" operation="prepend" lang="zh-Hans"><font>Stock.ttf</font></family><family customizationType="new-named-family" name="miclock"><font>Clock.ttf</font></family></fonts-modification>`
	_ = writeAtomic(s.rom("/product/etc/mi_fonts_customization.xml"), []byte(mi), 0600)
	updated, e := s.Init()
	if e != nil {
		t.Fatal(e)
	}
	if updated.Configs["/system/etc/fonts.xml"] != before {
		t.Fatal("captured active overlay as baseline")
	}
	if updated.Configs["/product/etc/mi_fonts_customization.xml"] != digest([]byte(mi)) {
		t.Fatal("new path missing")
	}
	g, e := s.Stage(variableProfile(a))
	if e != nil {
		t.Fatal(e)
	}
	out, e := os.ReadFile(s.data("generations", g.ID, "config/product/etc/mi_fonts_customization.xml"))
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(string(out), "HFC-") || !strings.Contains(string(out), "Clock.ttf") {
		t.Fatal("CJK not replaced or decorative clock changed")
	}
}

func TestSelectedLatinAvailableToXiaomiLocaleFamilies(t *testing.T) {
	s, a, _ := fixtureStore(t)
	p := DefaultProfile()
	p.Slots["latinSans"] = Slot{Mode: "variable", AssetID: a.ID, FamilyMode: "continuous"}
	g, err := s.Stage(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range g.Targets {
		raw, err := os.ReadFile(s.data("generations", g.ID, target.File))
		if err != nil {
			t.Fatal(err)
		}
		tree, err := parseXML(raw)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, set := range tree.Children {
			if set.Name.Local != "familyset" {
				continue
			}
			for _, family := range set.Children {
				if family.Name.Local == "family" && family.attr("name") == "" && family.attr("lang") == "und-Latn" {
					for _, font := range family.Children {
						if font.Name.Local == "font" && strings.HasPrefix(strings.TrimSpace(font.Text), "HFC-") && font.attr("fallbackFor") == "" {
							found = true
						}
					}
				}
			}
			if !found {
				t.Fatalf("%s: locale families cannot fall back to selected Latin font", target.Path)
			}
		}
	}
}

func TestXiaomiLocalePairsKeepBothScriptsAsPrimary(t *testing.T) {
	for _, custom := range []string{"both", "chinese", "latinSans"} {
		t.Run(custom, func(t *testing.T) {
			s, a, _ := fixtureStore(t)
			mi := `<fonts-modification><family customizationType="new-named-family" name="miclock"><font>Clock.ttf</font></family></fonts-modification>`
			vf := `<fonts-modification><family customizationType="new-locale-family" operation="prepend" lang="en"><font weight="400">StockLatin.ttf</font></family><family customizationType="new-locale-family" operation="prepend" lang="zh-Hans"><font weight="400">StockSC.ttf</font></family><family customizationType="new-locale-family" operation="prepend" lang="zh-Hant"><font weight="400">StockTC.ttf</font></family><family customizationType="new-locale-family" operation="prepend" lang="ja"><font>StockJA.ttf</font></family></fonts-modification>`
			if err := writeAtomic(s.rom("/product/etc/mi_fonts_customization.xml"), []byte(mi), 0600); err != nil {
				t.Fatal(err)
			}
			if err := writeAtomic(s.rom("/product/etc/misans_variable_fonts.xml"), []byte(vf), 0600); err != nil {
				t.Fatal(err)
			}
			p := DefaultProfile()
			for _, key := range []string{"chinese", "latinSans"} {
				if custom == "both" || custom == key {
					p.Slots[key] = Slot{Mode: "variable", AssetID: a.ID, FamilyMode: "continuous"}
				}
			}
			g, err := s.Stage(p)
			if err != nil {
				t.Fatal(err)
			}
			output, err := os.ReadFile(s.data("generations", g.ID, "config/product/etc/mi_fonts_customization.xml"))
			if err != nil {
				t.Fatal(err)
			}
			tree, err := parseXML(output)
			if err != nil {
				t.Fatal(err)
			}
			pairs := 0
			for _, root := range tree.Children {
				for _, n := range root.Children {
					if n.Name.Local != "family-list" || !strings.HasPrefix(n.attr("name"), "misans-global-") {
						continue
					}
					pairs++
					if len(n.Children) != 2 {
						t.Fatalf("expected separate Latin and Chinese primary families: %+v", n)
					}
					for i, key := range []string{"latinSans", "chinese"} {
						fonts := n.Children[i].Children
						if len(fonts) == 0 {
							t.Fatal("empty primary")
						}
						generated := strings.HasPrefix(strings.TrimSpace(fonts[0].Text), "HFC-")
						if generated != (p.Slots[key].Mode != "system") {
							t.Fatalf("wrong script selection for %s", key)
						}
					}
				}
			}
			if pairs != 3 || !strings.Contains(string(output), "Clock.ttf") {
				t.Fatal("missing runtime pair or lost clock")
			}
			output, err = os.ReadFile(s.data("generations", g.ID, "config/product/etc/misans_variable_fonts.xml"))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(output), `lang="zh-Hans"`) || strings.Contains(string(output), `lang="en"`) || !strings.Contains(string(output), "StockJA.ttf") {
				t.Fatal("auto-generated names still override pairs, or unrelated locale changed")
			}
		})
	}
}

func TestSelectedChineseOutranksPreservedNativeSansFallback(t *testing.T) {
	s, a, _ := fixtureStore(t)
	p := variableProfile(a)
	p.Slots["latinSans"] = Slot{Mode: "variable", AssetID: a.ID, FamilyMode: "continuous"}
	g, err := s.Stage(p)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(s.data("generations", g.ID, "config/system/etc/fonts.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `fallbackFor="sans-serif"`) {
		t.Fatal("Skia would prioritize old sans CJK over selected Chinese")
	}
	if !strings.Contains(string(raw), "Stock.ttf") {
		t.Fatal("lost original missing-glyph fallback")
	}
}

func TestRuntimeFontPayloadSurvivesPersistentSourceRemoval(t *testing.T) {
	s, a, _ := fixtureStore(t)
	g, err := s.Stage(variableProfile(a))
	if err != nil {
		t.Fatal(err)
	}
	s.Runtime = t.TempDir()
	for name, want := range g.Fonts {
		payload, err := s.runtimeFont(g, name)
		if err != nil {
			t.Fatal(err)
		}
		system := filepath.Join(s.Runtime, "system-"+name)
		product := filepath.Join(s.Runtime, "product-"+name)
		if err = os.Link(payload, system); err != nil {
			t.Fatal(err)
		}
		if err = os.Link(payload, product); err != nil {
			t.Fatal(err)
		}
		si, _ := os.Stat(system)
		pi, _ := os.Stat(product)
		if !os.SameFile(si, pi) {
			t.Fatal("duplicate payloads instead of shared inode")
		}
		if err = os.Remove(s.data("generations", g.ID, "fonts", name)); err != nil {
			t.Fatal(err)
		}
		for _, path := range []string{system, product} {
			got, err := hashFile(path)
			if err != nil || got != want {
				t.Fatal("font depends on removed /data source", err)
			}
		}
		if err = os.WriteFile(payload, []byte("damaged"), 0644); err != nil {
			t.Fatal(err)
		}
		if _, err = s.runtimeFont(g, name); err == nil {
			t.Fatal("corrupt payload silently reused")
		}
	}
}

func TestXiaomiNormalMapsToRegularWithoutChangingLight(t *testing.T) {
	s, a, _ := fixtureStore(t)
	g, err := s.Stage(variableProfile(a))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(s.data("generations", g.ID, "config/system/etc/fonts.xml"))
	if err != nil {
		t.Fatal(err)
	}
	tree, err := parseXML(raw)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]string{}
	for _, set := range tree.Children {
		for _, n := range set.Children {
			if n.Name.Local == "alias" {
				found[n.attr("name")] = n.attr("weight")
			}
		}
	}
	if found["mipro-normal"] != "400" || found["sans-serif-light"] != "300" {
		t.Fatalf("unexpected normal/light weights: %v", found)
	}
}

func TestRestoreKeepsDraft(t *testing.T) {
	s, a, _ := fixtureStore(t)
	draft := variableProfile(a)
	draft.Name = "Saved draft"
	if _, e := s.Handle(Request{Op: "save", Profile: &draft}); e != nil {
		t.Fatal(e)
	}
	before, _ := os.ReadFile(s.data("draft.json"))
	if _, e := s.Handle(Request{Op: "restore"}); e != nil {
		t.Fatal(e)
	}
	after, _ := os.ReadFile(s.data("draft.json"))
	if string(before) != string(after) {
		t.Fatalf("restore replaced the draft: %s", after)
	}
	pending, _ := s.generation("pending")
	if pending == nil || !pending.Profile.IsSystem() {
		t.Fatalf("restore did not stage system fonts: %+v", pending)
	}
}

func TestWeightMapErrorsNameValueAndRange(t *testing.T) {
	s, a, _ := fixtureStore(t)
	p := variableProfile(a)
	m := p.Slots["chinese"]
	m.WeightMap["900"] = 5000
	_, e := s.Validate(p)
	if e == nil || !strings.Contains(e.Error(), "5000 超出字体范围") {
		t.Fatalf("out of range: %v", e)
	}
	m.WeightMap["900"] = 750
	_, e = s.Validate(p)
	if e == nil || !strings.Contains(e.Error(), "750 小于上一档的 800") {
		t.Fatalf("not monotonic: %v", e)
	}
}

func TestDownloadFontsListsAndMarksImported(t *testing.T) {
	root := t.TempDir()
	downloads := filepath.Join(root, "Download")
	for _, dir := range []string{downloads, filepath.Join(downloads, "fonts"), filepath.Join(downloads, "fonts", "deep"), filepath.Join(downloads, ".hidden")} {
		if e := os.MkdirAll(dir, 0o755); e != nil {
			t.Fatal(e)
		}
	}
	font := fixtureFont(false)
	for _, p := range []string{"A-25S.ttf", "fonts/B.OTF", "fonts/deep/C.ttf", ".hidden/D.ttf", "._A-25S.ttf"} {
		if e := os.WriteFile(filepath.Join(downloads, p), font, 0o644); e != nil {
			t.Fatal(e)
		}
	}
	_ = os.WriteFile(filepath.Join(downloads, "notes.txt"), font, 0o644)
	missing := &Store{Root: filepath.Join(root, "data"), RomRoot: root, Downloads: filepath.Join(root, "nowhere")}
	if _, e := missing.DownloadFonts(); e == nil || !strings.Contains(e.Error(), "找不到 Download 文件夹") {
		t.Fatalf("missing Download folder: %v", e)
	}
	s := &Store{Root: filepath.Join(root, "data"), RomRoot: root, Downloads: downloads}
	list, e := s.DownloadFonts()
	if e != nil {
		t.Fatal(e)
	}
	names := []string{}
	for _, f := range list {
		names = append(names, f.Name)
		if f.Imported {
			t.Fatalf("%s marked imported before import", f.Name)
		}
	}
	if strings.Join(names, ",") != "A-25S.ttf,B.OTF" {
		t.Fatalf("listed %v", names)
	}
	if _, e = s.Handle(Request{Op: "import_path", Path: list[0].Path}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Handle(Request{Op: "import_path", Path: filepath.Join(root, "outside.ttf")}); e == nil {
		t.Fatal("import outside Download accepted")
	}
	list, _ = s.DownloadFonts()
	if !list[0].Imported && !list[1].Imported {
		t.Fatalf("imported file not marked: %+v", list)
	}
	// Same name and size but different bytes is not the imported font.
	changed := append([]byte{}, font...)
	changed[len(changed)-1] ^= 0xff
	if e = os.WriteFile(filepath.Join(downloads, "A-25S.ttf"), changed, 0o644); e != nil {
		t.Fatal(e)
	}
	list, _ = s.DownloadFonts()
	for _, f := range list {
		if f.Name == "A-25S.ttf" && f.Imported {
			t.Fatal("changed file still marked imported")
		}
	}
}
