package engine

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type node struct {
	Name     xml.Name
	Attr     []xml.Attr
	Text     string
	Children []*node
	Comment  bool
}

func parseXML(b []byte) (*node, error) {
	if len(b) > 4<<20 {
		return nil, fmt.Errorf("XML 过大")
	}
	d := xml.NewDecoder(bytes.NewReader(b))
	root := &node{}
	stack := []*node{root}
	count := 0
	for {
		t, e := d.Token()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
		cur := stack[len(stack)-1]
		switch v := t.(type) {
		case xml.StartElement:
			count++
			if count > 40000 || len(stack) > 32 {
				return nil, fmt.Errorf("XML 结构过于复杂")
			}
			n := &node{Name: v.Name, Attr: append([]xml.Attr(nil), v.Attr...)}
			cur.Children = append(cur.Children, n)
			stack = append(stack, n)
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		case xml.CharData:
			cur.Text += string(v)
		case xml.Comment:
			cur.Children = append(cur.Children, &node{Comment: true, Text: string(v)})
		case xml.Directive:
			return nil, fmt.Errorf("不接受 XML 外部实体或 DTD")
		}
	}
	if len(stack) != 1 {
		return nil, fmt.Errorf("XML 未闭合")
	}
	return root, nil
}
func (n *node) attr(k string) string {
	for _, a := range n.Attr {
		if a.Name.Local == k {
			return a.Value
		}
	}
	return ""
}
func (n *node) set(k, v string) {
	for i, a := range n.Attr {
		if a.Name.Local == k {
			n.Attr[i].Value = v
			return
		}
	}
	n.Attr = append(n.Attr, xml.Attr{Name: xml.Name{Local: k}, Value: v})
}
func (n *node) del(k string) {
	a := n.Attr[:0]
	for _, v := range n.Attr {
		if v.Name.Local != k {
			a = append(a, v)
		}
	}
	n.Attr = a
}
func clone(n *node) *node {
	c := *n
	c.Attr = append([]xml.Attr(nil), n.Attr...)
	c.Children = nil
	for _, ch := range n.Children {
		c.Children = append(c.Children, clone(ch))
	}
	return &c
}
func el(name string) *node { return &node{Name: xml.Name{Local: name}} }

// Match FontListParser's named-family attribute contract. XML well-formedness
// and successful Font.Builder calls do not imply the system can load this table.
func validateAndroidFontXML(root *node) error {
	var visit func(*node, bool) error
	visit = func(n *node, insideNamedList bool) error {
		named := insideNamedList
		for _, attribute := range n.Attr {
			if attribute.Name.Local == "name" {
				named = true
			}
		}
		if n.Name.Local == "family" && named {
			for _, attribute := range n.Attr {
				switch attribute.Name.Local {
				case "lang", "variant", "ignore":
					return fmt.Errorf("Android 字体表不允许 named family %q 使用 %s 属性", n.attr("name"), attribute.Name.Local)
				case "name":
					if insideNamedList {
						return fmt.Errorf("Android 字体表不允许 family-list 的子 family 再设置 name")
					}
				}
			}
		}
		for _, child := range n.Children {
			if err := visit(child, n.Name.Local == "family-list"); err != nil {
				return err
			}
		}
		return nil
	}
	return visit(root, false)
}

func encodeXML(root *node) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(xml.Header)
	e := xml.NewEncoder(&b)
	e.Indent("", "  ")
	var write func(*node) error
	write = func(n *node) error {
		if n.Comment {
			return e.EncodeToken(xml.Comment(n.Text))
		}
		if n.Name.Local != "" {
			if err := e.EncodeToken(xml.StartElement{Name: n.Name, Attr: n.Attr}); err != nil {
				return err
			}
			if strings.TrimSpace(n.Text) != "" {
				if err := e.EncodeToken(xml.CharData(strings.TrimSpace(n.Text))); err != nil {
					return err
				}
			}
		}
		for _, c := range n.Children {
			if err := write(c); err != nil {
				return err
			}
		}
		if n.Name.Local != "" {
			return e.EncodeToken(xml.EndElement{Name: n.Name})
		}
		return nil
	}
	if err := write(root); err != nil {
		return nil, err
	}
	if err := e.Flush(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

type compiler struct {
	store      *Store
	dir        string
	generated  map[string]string
	deps       map[string]string
	references map[string]bool
}

func (c *compiler) fontFile(id string, index int, filter string, stock bool) (string, error) {
	key := fmt.Sprintf("%s:%d:%s:%t", id, index, filter, stock)
	if name, ok := c.generated[key]; ok {
		return name, nil
	}
	var b []byte
	var err error
	if stock {
		path, err := c.store.originalFont(id)
		if err != nil {
			return "", err
		}
		b, err = os.ReadFile(path)
		if err != nil {
			return "", err
		}
		c.deps[id] = digest(b)
		if filter == "without-cjk" {
			yes, e := hasCJK(b, index)
			if e != nil {
				return "", e
			}
			if !yes {
				name := filepath.Base(id)
				c.generated[key] = name
				return name, nil
			}
		}
	} else {
		if !assetPattern.MatchString(id) {
			return "", fmt.Errorf("非法字体 ID")
		}
		b, err = os.ReadFile(c.store.data("library", id+".font"))
		if err != nil {
			return "", err
		}
		if digest(b) != id {
			return "", fmt.Errorf("字体文件校验失败")
		}
	}
	var out []byte
	if stock && filter == "alias" {
		// Xiaomi promotes anonymous Roboto-Regular.ttf to a named sans-serif
		// family, even when fallbackFor/lang/supportedAxes say otherwise.
		// Keep every byte and only give this fallback a non-reserved basename.
		out = b
	} else {
		out, err = RewriteFont(b, index, filter)
		if err != nil {
			return "", err
		}
	}
	sum := sha256.Sum256(out)
	name := "HFC-" + hex.EncodeToString(sum[:]) + ".ttf"
	if err = writeAtomic(filepath.Join(c.dir, "fonts", name), out, 0644); err != nil {
		return "", err
	}
	c.generated[key] = name
	c.references[name] = true
	return name, nil
}
func (c *compiler) makeFonts(s Slot, scope string, modern bool) ([]*node, error) {
	filter := "latin"
	if scope == "chinese" {
		filter = "cjk"
	}
	font := func(id string, index, weight int, style string, axes map[string]float64, continuous bool) (*node, error) {
		name, e := c.fontFile(id, index, filter, false)
		if e != nil {
			return nil, e
		}
		f := el("font")
		f.Text = name
		f.set("style", style)
		if continuous {
			f.set("supportedAxes", "wght")
		} else {
			f.set("weight", fmt.Sprint(weight))
		}
		keys := []string{}
		for k := range axes {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			a := el("axis")
			a.set("tag", k)
			a.set("stylevalue", strconv.FormatFloat(axes[k], 'f', -1, 64))
			f.Children = append(f.Children, a)
		}
		return f, nil
	}
	out := []*node{}
	if s.Mode == "static" {
		faces := append([]FaceRef(nil), s.Faces...)
		sort.SliceStable(faces, func(i, j int) bool {
			if faces[i].Style != faces[j].Style {
				return faces[i].Style < faces[j].Style
			}
			return faces[i].Weight < faces[j].Weight
		})
		for _, r := range faces {
			f, e := font(r.AssetID, r.FaceIndex, r.Weight, r.Style, nil, false)
			if e != nil {
				return nil, e
			}
			out = append(out, f)
		}
		return out, nil
	}
	_, meta, err := c.store.lookup(s.AssetID, s.FaceIndex)
	if err != nil {
		return nil, err
	}
	var w Axis
	for _, a := range meta.Axes {
		if a.Tag == "wght" {
			w = a
		}
	}
	styles := []FaceRef{{AssetID: s.AssetID, FaceIndex: s.FaceIndex, Style: "normal"}}
	if s.ItalicAssetID != "" {
		styles = append(styles, FaceRef{AssetID: s.ItalicAssetID, FaceIndex: s.ItalicFaceIndex, Style: "italic"})
	}
	for _, r := range styles {
		for i := 100; i <= 900; i += 100 {
			axes := map[string]float64{}
			for k, v := range s.Axes {
				axes[k] = v
			}
			continuous := s.FamilyMode == "continuous" && modern
			if !continuous {
				value := float64(i)
				if s.FamilyMode == "instances" {
					value = s.WeightMap[fmt.Sprint(i)]
				} else {
					value = max(w.Min, min(w.Max, value))
				}
				axes["wght"] = value
			}
			f, e := font(r.AssetID, r.FaceIndex, i, r.Style, axes, continuous)
			if e != nil {
				return nil, e
			}
			out = append(out, f)
			if continuous {
				break
			}
		}
	}
	return out, nil
}
func namedSlot(name string) string {
	switch {
	case name == "serif" || strings.HasPrefix(name, "serif-"):
		return "serif"
	case name == "monospace":
		return "monospace"
	case name == "sans-serif" || strings.HasPrefix(name, "sans-serif-") || name == "roboto" || name == "roboto-flex" || name == "source-sans-pro" || strings.HasPrefix(name, "mipro") || strings.HasPrefix(name, "miui") || name == "misans":
		return "latinSans"
	}
	return ""
}
func hasChineseLang(s string) bool {
	for _, x := range strings.Split(s, ",") {
		if strings.HasPrefix(strings.TrimSpace(x), "zh") {
			return true
		}
	}
	return false
}

// HyperOS creates one misans-global-* primary from each locale customization.
// A CJK-only primary then falls back to unrelated scripts for Latin (and vice
// versa). Put the two independently styled families in an explicit primary
// list, and move that list to the customization entry point that reads lists.
func (c *compiler) xiaomiLocalePairs(p Profile) (map[string]*node, error) {
	groups := map[string]*node{}
	if p.Slots["chinese"].Mode == "system" && p.Slots["latinSans"].Mode == "system" {
		return groups, nil
	}
	var base Baseline
	if readJSON(c.store.data("baseline.json"), &base) != nil || base.SDK < 35 || base.Configs["/product/etc/mi_fonts_customization.xml"] == "" || base.Configs["/product/etc/misans_variable_fonts.xml"] == "" {
		return groups, nil
	}
	raw, err := os.ReadFile(c.store.data("baseline/product/etc/misans_variable_fonts.xml"))
	if err != nil {
		return nil, err
	}
	doc, err := parseXML(raw)
	if err != nil {
		return nil, err
	}
	locales := map[string]*node{}
	for _, root := range doc.Children {
		for _, family := range root.Children {
			if family.Name.Local == "family" && family.attr("customizationType") == "new-locale-family" {
				locales[family.attr("lang")] = family
			}
		}
	}
	if locales["en"] == nil || locales["zh-Hans"] == nil {
		return groups, nil
	}
	for _, lang := range []string{"en", "zh-Hans", "zh-Hant"} {
		if locales[lang] == nil {
			continue
		}
		group := el("family-list")
		group.set("name", "misans-global-"+lang)
		group.set("customizationType", "new-named-family")
		for _, key := range []string{"latinSans", "chinese"} {
			family := el("family")
			if p.Slots[key].Mode != "system" {
				family.Children, err = c.makeFonts(p.Slots[key], key, true)
				if err != nil {
					return nil, err
				}
			} else {
				original := locales["en"]
				if key == "chinese" {
					original = locales[lang]
					if lang == "en" {
						original = locales["zh-Hans"]
					}
				}
				for _, font := range original.Children {
					if font.Name.Local == "font" {
						family.Children = append(family.Children, clone(font))
					}
				}
			}
			group.Children = append(group.Children, family)
		}
		groups[lang] = group
	}
	return groups, nil
}

func (c *compiler) compileXML(path string, raw []byte, p Profile) ([]byte, int, error) {
	root, err := parseXML(raw)
	if err != nil {
		return nil, 0, err
	}
	var set *node
	for _, n := range root.Children {
		if n.Name.Local == "familyset" || n.Name.Local == "fonts-modification" {
			set = n
			break
		}
	}
	if set == nil {
		return nil, 0, fmt.Errorf("不支持的字体配置根节点 %s", path)
	}
	modern := strings.Contains(path, "fallback") || strings.HasSuffix(path, "misans_variable_fonts.xml")
	changes := 0
	if set.Name.Local == "fonts-modification" {
		groups := map[string]*node{}
		if path == "/product/etc/misans_variable_fonts.xml" || path == "/product/etc/mi_fonts_customization.xml" {
			groups, err = c.xiaomiLocalePairs(p)
			if err != nil {
				return nil, 0, err
			}
		}
		if path == "/product/etc/misans_variable_fonts.xml" && len(groups) > 0 {
			kept := []*node{}
			for _, family := range set.Children {
				if family.Name.Local == "family" && family.attr("customizationType") == "new-locale-family" && groups[family.attr("lang")] != nil {
					changes++
					continue
				}
				kept = append(kept, family)
			}
			set.Children = kept
		}
		for _, family := range set.Children {
			if family.Name.Local != "family" {
				continue
			}
			key := ""
			if hasChineseLang(family.attr("lang")) {
				key = "chinese"
			} else if family.attr("lang") == "en" {
				key = "latinSans"
			} else if family.attr("name") == "misans-for-fallback" {
				key = "latinSans"
			}
			if key == "" || p.Slots[key].Mode == "system" {
				continue
			}
			fonts, e := c.makeFonts(p.Slots[key], key, modern)
			if e != nil {
				return nil, 0, e
			}
			family.Children = fonts
			family.Text = ""
			family.del("supportedAxes")
			changes++
		}
		if path == "/product/etc/mi_fonts_customization.xml" {
			for _, lang := range []string{"en", "zh-Hans", "zh-Hant"} {
				if group := groups[lang]; group != nil {
					for i := len(set.Children) - 1; i >= 0; i-- {
						if set.Children[i].attr("name") == group.attr("name") {
							set.Children = append(set.Children[:i], set.Children[i+1:]...)
						}
					}
					set.Children = append(set.Children, group)
					changes++
				}
			}
		}
	} else {
		if p.Slots["chinese"].Mode != "system" || p.Slots["latinSans"].Mode != "system" {
			for _, alias := range set.Children {
				if alias.Name.Local == "alias" && alias.attr("name") == "mipro-normal" && alias.attr("to") == "sans-serif" && alias.attr("weight") != "400" {
					// The ROM's 350 alias is reclassified as sans-serif-light by
					// FontNameUtil. A regular-weight alias retains requested weights.
					alias.set("weight", "400")
					changes++
				}
			}
		}
		// Named families must not keep a CJK-capable primary that masks the new Han
		// fallback. Filter only its cmap; keep stock outlines, metrics and axes.
		extras := []*node{}
		for _, family := range set.Children {
			if family.Name.Local != "family" {
				continue
			}
			name := family.attr("name")
			key := namedSlot(name)
			if key == "" {
				continue
			}
			sel := p.Slots[key]
			chinese := p.Slots["chinese"].Mode != "system"
			if sel.Mode != "system" {
				original := clone(family)
				original.del("name")
				original.del("id")
				original.del("customizationType")
				for _, f := range original.Children {
					if f.Name.Local == "font" {
						if key == "latinSans" && chinese {
							// Skia prioritizes family-specific fallbacks over generic
							// CJK, regardless of document order. Keep the original
							// as an ordinary later fallback so it cannot mask Han.
							f.del("fallbackFor")
						} else {
							f.set("fallbackFor", name)
						}
					}
				}
				extras = append(extras, original)
				fs, e := c.makeFonts(sel, key, modern)
				if e != nil {
					return nil, 0, e
				}
				family.Children = fs
				family.Text = ""
				family.del("supportedAxes")
				changes++
			} else if chinese {
				for _, f := range family.Children {
					if f.Name.Local != "font" {
						continue
					}
					filename := strings.TrimSpace(f.Text)
					if filename == "" {
						continue
					}
					idx := 0
					if f.attr("index") != "" {
						idx, err = strconv.Atoi(f.attr("index"))
						if err != nil {
							return nil, 0, err
						}
					}
					fontdir := "/system/fonts"
					if strings.HasPrefix(path, "/product/") {
						fontdir = "/product/fonts"
					}
					src := filepath.Join(fontdir, filename)
					if filepath.Base(filename) != filename {
						return nil, 0, fmt.Errorf("非标准字体路径，需设备适配: %s", filename)
					}
					generated, e := c.fontFile(src, idx, "without-cjk", true)
					if e != nil {
						return nil, 0, e
					}
					if generated == filename {
						continue
					}
					f.Text = generated
					f.del("postScriptName")
					f.del("index")
					changes++
				}
			}
		}
		prepend := []*node{}
		var nativeChinese *node
		// Xiaomi's runtime builds misans-global-* from locale families. Those
		// families inherit the anonymous fallbacks, not the named sans-serif
		// primary. Keep the selected Latin available in that chain too.
		if p.Slots["latinSans"].Mode != "system" {
			f := el("family")
			f.set("lang", "und-Latn")
			fs, e := c.makeFonts(p.Slots["latinSans"], "latinSans", modern)
			if e != nil {
				return nil, 0, e
			}
			f.Children = fs
			prepend = append(prepend, f)
			changes++
		}
		if p.Slots["chinese"].Mode != "system" {
			f := el("family")
			f.set("lang", "zh-Hans,zh-Hant")
			fs, e := c.makeFonts(p.Slots["chinese"], "chinese", modern)
			if e != nil {
				return nil, 0, e
			}
			f.Children = fs
			prepend = append(prepend, f)
			changes++
			// This ROM's native matcher scans all named families before
			// anonymous fallbacks when no requested family/language covers a
			// glyph. Keep Android's anonymous route and add a named CJK-only
			// entry before unrelated named/decorative families for that path.
			nativeChinese = clone(f)
			// HyperOS sorts its native named map lexically. Document position
			// alone leaves "hyperfont-*" after "coca-cola-*"; use an internal
			// prefix that was verified first in this ROM's native enumeration.
			nativeChinese.set("name", "00-hyperfont-chinese")
			// Named families reject lang/variant/ignore in Android's parser.
			// Keep locale tags only on the original anonymous fallback.
			nativeChinese.del("lang")
			nativeChinese.del("variant")
			nativeChinese.del("ignore")
		}
		// Insert before stock fallbacks; named families are resolved independently.
		prepend = append(prepend, extras...)
		at := len(set.Children)
		for i, n := range set.Children {
			if n.Name.Local == "family" && n.attr("name") == "" {
				at = i
				break
			}
		}
		children := append([]*node{}, set.Children[:at]...)
		children = append(children, prepend...)
		children = append(children, set.Children[at:]...)
		set.Children = children
		if nativeChinese != nil {
			for _, family := range set.Children {
				if family.attr("name") == nativeChinese.attr("name") {
					return nil, 0, fmt.Errorf("系统字体表占用了模块内部字体名称: %s", nativeChinese.attr("name"))
				}
			}
			// Preserve the first/default family and explicit decorative names.
			at := len(set.Children)
			for i, family := range set.Children {
				if family.Name.Local == "family" {
					at = i + 1
					break
				}
			}
			set.Children = append(set.Children[:at], append([]*node{nativeChinese}, set.Children[at:]...)...)
			changes++
		}
		if p.Slots["latinSans"].Mode != "system" {
			xiaomi := false
			for _, n := range set.Children {
				if strings.HasPrefix(n.attr("name"), "mipro-") {
					xiaomi = true
				}
			}
			if xiaomi {
				for _, name := range []string{"mipro", "miui", "misanslatinvf"} {
					found := false
					for _, n := range set.Children {
						if n.attr("name") == name {
							found = true
						}
					}
					if !found {
						n := el("alias")
						n.set("name", name)
						n.set("to", "sans-serif")
						set.Children = append(set.Children, n)
						changes++
					}
				}
			}
		}
		if p.Slots["latinSans"].Mode != "system" {
			for _, family := range set.Children {
				if family.Name.Local != "family" || family.attr("name") != "" {
					continue
				}
				for _, font := range family.Children {
					if font.Name.Local != "font" || strings.TrimSpace(font.Text) != "Roboto-Regular.ttf" {
						continue
					}
					dir := "/system/fonts"
					if strings.HasPrefix(path, "/product/") {
						dir = "/product/fonts"
					}
					name, e := c.fontFile(filepath.Join(dir, "Roboto-Regular.ttf"), 0, "alias", true)
					if e != nil {
						return nil, 0, e
					}
					font.Text = name
					font.del("postScriptName")
					changes++
				}
			}
		}
	}
	if changes == 0 {
		return raw, 0, nil
	}
	if err = validateAndroidFontXML(root); err != nil {
		return nil, 0, err
	}
	out, err := encodeXML(root)
	return out, changes, err
}
