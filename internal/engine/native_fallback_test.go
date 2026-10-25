package engine

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Replay the relevant selection order from this ROM's native matcher, not
// current upstream Skia: requested name then empty name; each language (and
// its parent) then no language; named families before anonymous fallbacks.
// Device enumeration also proves that named entries are sorted by name,
// regardless of where their family appeared in an XML document.
// All fixture faces have the default variant and identical coverage per weight.
// This is a compiler regression model, not execution of the Android engine.
func nativeFallbackReplay(t *testing.T, s *Store, g Generation, families []*node, request, locale string, character uint32) string {
	t.Helper()
	namedFamilies := append([]*node(nil), families...)
	sort.SliceStable(namedFamilies, func(i, j int) bool {
		return namedFamilies[i].attr("name") < namedFamilies[j].attr("name")
	})
	languages := []string{}
	for tag := locale; tag != ""; {
		languages = append(languages, tag)
		at := strings.LastIndex(tag, "-")
		if at < 0 {
			break
		}
		tag = tag[:at]
	}
	languages = append(languages, "")
	for _, name := range []string{request, ""} {
		for _, language := range languages {
			for _, named := range []bool{true, false} {
				ordered := families
				if named {
					ordered = namedFamilies
				}
				for _, family := range ordered {
					if family.Name.Local != "family" || (family.attr("name") != "") != named {
						continue
					}
					if named && name != "" && family.attr("name") != name {
						continue
					}
					matched := language == ""
					for _, tag := range strings.Split(family.attr("lang"), ",") {
						matched = matched || strings.HasPrefix(tag, language)
					}
					if !matched {
						continue
					}
					for _, font := range family.Children {
						if font.Name.Local != "font" || (!named && name != "" && font.attr("fallbackFor") != name) {
							continue
						}
						file := strings.TrimSpace(font.Text)
						path := s.rom("/system/fonts/" + file)
						if strings.HasPrefix(file, "HFC-") {
							path = s.data("generations", g.ID, "fonts", file)
						}
						data, err := os.ReadFile(path)
						if err != nil {
							t.Fatal(err)
						}
						if charmap(tables(fontData(data), 0))[character] != 0 {
							return file
						}
					}
				}
			}
		}
	}
	return ""
}

func TestNativeChineseFallbackWithoutLocale(t *testing.T) {
	for _, mode := range []string{"continuous", "instances", "static"} {
		t.Run(mode, func(t *testing.T) {
			s, variable, static := fixtureStore(t)
			stock := `<familyset version="23"><family name="sans-serif"><font>Stock.ttf</font></family><family name="coca-cola-care-font-kaiti"><font>Decorative.ttf</font></family><family lang="zh-Hans"><font>Stock.ttf</font></family><family lang="und-Zsye"><font>Emoji.ttf</font></family></familyset>`
			for _, file := range []string{"/system/etc/fonts.xml", "/system/etc/font_fallback.xml"} {
				if err := writeAtomic(s.rom(file), []byte(stock), 0600); err != nil {
					t.Fatal(err)
				}
			}
			for _, file := range []string{"Decorative.ttf", "Emoji.ttf"} {
				if err := writeAtomic(s.rom("/system/fonts/"+file), fixtureFont(false), 0600); err != nil {
					t.Fatal(err)
				}
			}
			p := variableProfile(variable)
			chinese := p.Slots["chinese"]
			if mode == "continuous" {
				chinese.FamilyMode = mode
				chinese.WeightMap = nil
			} else if mode == "static" {
				chinese = Slot{Mode: "static", MissingWeightPolicy: "nearest", Faces: []FaceRef{{AssetID: static.ID, Weight: 400, Style: "normal"}, {AssetID: static.ID, Weight: 700, Style: "normal"}}}
			}
			p.Slots["chinese"] = chinese
			p.Slots["latinSans"] = Slot{Mode: "variable", AssetID: variable.ID, FamilyMode: "continuous"}
			g, err := s.Stage(p)
			if err != nil {
				t.Fatal(err)
			}
			for _, target := range g.Targets {
				raw, err := os.ReadFile(s.data("generations", g.ID, target.File))
				if err != nil {
					t.Fatal(err)
				}
				doc, err := parseXML(raw)
				if err != nil {
					t.Fatal(err)
				}
				families := doc.Children[0].Children
				if families[0].attr("name") != "sans-serif" {
					t.Fatal("changed the first/default family")
				}
				var anonymous, named *node
				for _, f := range families {
					if f.Name.Local == "family" && f.attr("lang") == "zh-Hans,zh-Hant" && f.attr("name") == "" {
						anonymous = f
					}
					if f.Name.Local == "family" && f.attr("name") == "00-hyperfont-chinese" {
						named = f
					}
				}
				if anonymous == nil {
					t.Fatal("lost Android's anonymous CJK fallback")
				}
				want := strings.TrimSpace(anonymous.Children[0].Text)
				for _, request := range []string{"sans-serif", "mipro", "unknown-family", ""} {
					for _, locale := range []string{"", "en-US", "zh-Hans", "zh-CN"} {
						got := nativeFallbackReplay(t, s, g, families, request, locale, 0x4e2d)
						if got != want {
							t.Fatalf("%s %s request=%q lang=%q: Chinese chose %s, want selected %s", mode, target.Path, request, locale, got, want)
						}
					}
				}
				if named == nil {
					t.Fatal("missing native CJK entry")
				}
				a, _ := encodeXML(&node{Children: anonymous.Children})
				b, _ := encodeXML(&node{Children: named.Children})
				if !bytes.Equal(a, b) {
					t.Fatal("native and Android CJK weights differ")
				}
				if got := nativeFallbackReplay(t, s, g, families, "coca-cola-care-font-kaiti", "", 0x4e2d); got != "Decorative.ttf" {
					t.Fatal("explicit decorative request was changed")
				}
				data, err := os.ReadFile(s.data("generations", g.ID, "fonts", filepath.Base(want)))
				if err != nil || charmap(tables(fontData(data), 0))['A'] != 0 {
					t.Fatal("Chinese entry must not mask selected Latin")
				}
			}
		})
	}
}

func TestSystemChineseDoesNotAddNativeEntry(t *testing.T) {
	s, a, _ := fixtureStore(t)
	p := DefaultProfile()
	p.Slots["latinSans"] = Slot{Mode: "variable", AssetID: a.ID, FamilyMode: "continuous"}
	g, err := s.Stage(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range g.Targets {
		raw, _ := os.ReadFile(s.data("generations", g.ID, target.File))
		if bytes.Contains(raw, []byte(`name="00-hyperfont-chinese"`)) {
			t.Fatal("system Chinese gained a replacement entry")
		}
	}
}
