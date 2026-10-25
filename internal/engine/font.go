package engine

// This parser deliberately accepts SFNT/TTC only. No decompression, native font
// execution, bytecode interpretation or third-party parser runs as root.
import (
	"bytes"
	"encoding/binary"
	"fmt"
	"sort"
	"strings"
	"unicode/utf16"
)

type fontData []byte

func (b fontData) cut(o, n int) fontData {
	if o < 0 || n < 0 || o > len(b) || n > len(b)-o {
		panic("字体表越界")
	}
	return b[o : o+n]
}
func (b fontData) u16(o int) int       { return int(binary.BigEndian.Uint16(b.cut(o, 2))) }
func (b fontData) i16(o int) int       { return int(int16(b.u16(o))) }
func (b fontData) u32(o int) int       { return int(binary.BigEndian.Uint32(b.cut(o, 4))) }
func (b fontData) fixed(o int) float64 { return float64(int32(b.u32(o))) / 65536 }
func put16(b []byte, o, n int)         { binary.BigEndian.PutUint16(b[o:], uint16(n)) }
func put32(b []byte, o, n int)         { binary.BigEndian.PutUint32(b[o:], uint32(n)) }
func fontRecover(err *error) {
	if x := recover(); x != nil {
		*err = fmt.Errorf("字体文件无效: %v", x)
	}
}
func offsets(b fontData) []int {
	if string(b.cut(0, 4)) != "ttcf" {
		return []int{0}
	}
	n := b.u32(8)
	if n < 1 || n > 32 {
		panic("集合字体面数不支持")
	}
	r := make([]int, n)
	for i := range r {
		r[i] = b.u32(12 + i*4)
	}
	return r
}
func tables(b fontData, offset int) map[string]fontData {
	magic := string(b.cut(offset, 4))
	if magic != "\x00\x01\x00\x00" && magic != "OTTO" && magic != "true" {
		panic("仅支持 TTF/OTF/TTC/OTC")
	}
	n := b.u16(offset + 4)
	if n < 3 || n > 256 {
		panic("字体表数量无效")
	}
	m := map[string]fontData{}
	for i := 0; i < n; i++ {
		p := offset + 12 + i*16
		tag := string(b.cut(p, 4))
		if _, ok := m[tag]; ok {
			panic("重复字体表")
		}
		m[tag] = b.cut(b.u32(p+8), b.u32(p+12))
	}
	for _, k := range []string{"head", "maxp", "cmap", "hhea", "hmtx", "name"} {
		if _, ok := m[k]; !ok {
			panic("缺少必需字体表 " + k)
		}
	}
	if len(m["glyf"]) == 0 && len(m["CFF "]) == 0 && len(m["CFF2"]) == 0 {
		panic("缺少轮廓表")
	}
	return m
}
func charmap(t map[string]fontData) map[uint32]uint16 {
	b := t["cmap"]
	n := b.u16(2)
	if n > 256 {
		panic("cmap 子表过多")
	}
	out := map[uint32]uint16{}
	maxGlyph := t["maxp"].u16(4)
	add := func(c, g int) {
		if c < 0 || c > 0x10FFFF || c >= 0xD800 && c <= 0xDFFF || g < 0 || g >= maxGlyph {
			panic("cmap 字符或字形索引越界")
		}
		if g > 0 {
			out[uint32(c)] = uint16(g)
		}
	}
	// Unicode format 12 overrides format 4 if both are present.
	for _, format := range []int{4, 12} {
		for i := 0; i < n; i++ {
			platform, enc := b.u16(4+i*8), b.u16(6+i*8)
			if platform != 0 && !(platform == 3 && (enc == 1 || enc == 10)) {
				continue
			}
			sub := b.cut(b.u32(8+i*8), len(b)-b.u32(8+i*8))
			if sub.u16(0) != format {
				continue
			}
			if format == 12 {
				sub = sub.cut(0, sub.u32(4))
				groups := sub.u32(12)
				if groups > 100000 {
					panic("cmap 分组过多")
				}
				total := 0
				for j := 0; j < groups; j++ {
					p := 16 + j*12
					a, z, g := sub.u32(p), sub.u32(p+4), sub.u32(p+8)
					if a > z || z > 0x10FFFF {
						panic("cmap 范围错误")
					}
					total += z - a + 1
					if total > 1114112 {
						panic("cmap 重叠或过大")
					}
					for c := a; c <= z; c++ {
						add(c, g+c-a)
					}
				}
			} else {
				sub = sub.cut(0, sub.u16(2))
				segs := sub.u16(6) / 2
				if segs < 1 || segs > 32767 {
					panic("cmap 段无效")
				}
				previousEnd := -1
				for j := 0; j < segs; j++ {
					end := sub.u16(14 + j*2)
					start := sub.u16(16 + segs*2 + j*2)
					if start <= previousEnd {
						panic("cmap 段重叠或倒序")
					}
					previousEnd = end
					delta := sub.i16(16 + segs*4 + j*2)
					rp := 16 + segs*6 + j*2
					off := sub.u16(rp)
					if start > end {
						panic("cmap 段倒序")
					}
					for c := start; c <= end && c != 65535; c++ {
						g := (c + delta) & 65535
						if off != 0 {
							g = sub.u16(rp + off + (c-start)*2)
							if g != 0 {
								g = (g + delta) & 65535
							}
						}
						add(c, g)
					}
				}
			}
		}
	}
	if len(out) == 0 {
		panic("没有支持的 Unicode cmap")
	}
	return out
}
func fontNames(b fontData) map[int]string {
	n := b.u16(2)
	if n > 8192 {
		panic("名称记录过多")
	}
	base := b.u16(4)
	out := map[int]string{}
	for i := 0; i < n; i++ {
		p := 6 + i*12
		platform, lang, id := b.u16(p), b.u16(p+4), b.u16(p+6)
		if platform != 0 && platform != 3 {
			continue
		}
		raw := b.cut(base+b.u16(p+10), b.u16(p+8))
		if len(raw)%2 != 0 {
			continue
		}
		u := make([]uint16, len(raw)/2)
		for j := range u {
			u[j] = uint16(raw.u16(j * 2))
		}
		v := strings.TrimSpace(string(utf16.Decode(u)))
		if v != "" && (out[id] == "" || lang == 0x409) {
			out[id] = v
		}
	}
	return out
}
func ParseFont(b []byte) (faces []Face, err error) {
	defer fontRecover(&err)
	if len(b) < 12 || len(b) > MaxFontSize {
		return nil, fmt.Errorf("字体大小不支持")
	}
	data := fontData(b)
	for i, off := range offsets(data) {
		t := tables(data, off)
		names := fontNames(t["name"])
		f := Face{Index: i, Name: names[1], PostScript: names[6], Weight: 400, Units: t["head"].u16(18), Ascender: t["hhea"].i16(4), Descender: t["hhea"].i16(6), LineGap: t["hhea"].i16(8), Axes: []Axis{}}
		if names[16] != "" {
			f.Name = names[16]
		}
		if names[2] != "" {
			f.Name += " " + names[2]
		}
		if len([]rune(f.Name)) > 200 {
			panic("字体名称过长")
		}
		if f.Units < 16 || f.Units > 16384 {
			panic("unitsPerEm 无效")
		}
		if os2 := t["OS/2"]; len(os2) > 0 {
			f.Weight = os2.u16(4)
			f.Italic = os2.u16(62)&1 != 0
		}
		f.Italic = f.Italic || t["head"].u16(44)&2 != 0
		if a := t["fvar"]; len(a) > 0 {
			start, count, size := a.u16(4), a.u16(8), a.u16(10)
			if count > 16 || size < 20 {
				panic("可变轴数量无效")
			}
			seen := map[string]bool{}
			for j := 0; j < count; j++ {
				p := start + j*size
				ax := Axis{string(a.cut(p, 4)), a.fixed(p + 4), a.fixed(p + 8), a.fixed(p + 12)}
				if seen[ax.Tag] || ax.Min > ax.Default || ax.Default > ax.Max {
					panic("可变轴无效")
				}
				seen[ax.Tag] = true
				f.Axes = append(f.Axes, ax)
			}
		}
		cm := charmap(t)
		f.Chinese = cm[0x4e2d] != 0 && cm[0x6587] != 0
		f.Latin = cm['A'] != 0 && cm['a'] != 0 && cm['0'] != 0
		faces = append(faces, f)
	}
	return
}
func isCJK(c uint32) bool {
	// Keep Japanese kana and Korean hangul out of the Chinese replacement.
	return c >= 0x2E80 && c <= 0x2FFF || c >= 0x3000 && c <= 0x303F ||
		c >= 0x3100 && c <= 0x312F || c >= 0x31A0 && c <= 0x31EF ||
		c >= 0x3400 && c <= 0x4DBF || c >= 0x4E00 && c <= 0x9FFF ||
		c >= 0xF900 && c <= 0xFAFF || c >= 0xFE10 && c <= 0xFE1F ||
		c >= 0xFE30 && c <= 0xFE4F || c >= 0xFF01 && c <= 0xFF60 ||
		c >= 0xFFE0 && c <= 0xFFE6 || c >= 0x20000 && c <= 0x323AF
}

func hasCJK(b []byte, index int) (yes bool, err error) {
	defer fontRecover(&err)
	offs := offsets(fontData(b))
	if index < 0 || index >= len(offs) {
		return false, fmt.Errorf("集合索引错误")
	}
	for code := range charmap(tables(fontData(b), offs[index])) {
		if isCJK(code) {
			return true, nil
		}
	}
	return false, nil
}
func isLatin(c uint32) bool {
	return c >= 0x20 && c <= 0x24F || c >= 0x1E00 && c <= 0x1EFF || c >= 0x2000 && c <= 0x206F || c == 0x20AC || c == 0x2122
}
func makeCmap(cm map[uint32]uint16, filter string) []byte {
	keys := []int{}
	for c := range cm {
		keep := filter == "all" || filter == "cjk" && isCJK(c) || filter == "without-cjk" && !isCJK(c) || filter == "latin" && isLatin(c)
		if keep {
			keys = append(keys, int(c))
		}
	}
	sort.Ints(keys)
	type group struct{ a, z, g int }
	groups := []group{}
	for _, c := range keys {
		g := int(cm[uint32(c)])
		n := len(groups)
		if n > 0 && c == groups[n-1].z+1 && g == groups[n-1].g+c-groups[n-1].a {
			groups[n-1].z = c
		} else {
			groups = append(groups, group{c, c, g})
		}
	}
	b := make([]byte, 28+12*len(groups))
	put16(b, 2, 1)
	put16(b, 4, 3)
	put16(b, 6, 10)
	put32(b, 8, 12)
	put16(b, 12, 12)
	put32(b, 16, len(b)-12)
	put32(b, 24, len(groups))
	for i, g := range groups {
		p := 28 + i*12
		put32(b, p, g.a)
		put32(b, p+4, g.z)
		put32(b, p+8, g.g)
	}
	return b
}
func checksum(b []byte) uint32 {
	var n uint32
	for i := 0; i < len(b); i += 4 {
		var a [4]byte
		copy(a[:], b[i:min(i+4, len(b))])
		n += binary.BigEndian.Uint32(a[:])
	}
	return n
}
func RewriteFont(b []byte, index int, filter string) (result []byte, err error) {
	defer fontRecover(&err)
	data := fontData(b)
	offs := offsets(data)
	if index < 0 || index >= len(offs) {
		return nil, fmt.Errorf("字体集合索引错误")
	}
	off := offs[index]
	t := tables(data, off)
	if filter != "cjk" && filter != "latin" && filter != "without-cjk" && filter != "all" {
		return nil, fmt.Errorf("未知过滤范围")
	}
	// Preserve all outlines, variation data, shaping tables and vertical metrics.
	// Only Unicode entry points change. Format 14 is intentionally not propagated
	// into replacement fonts: stock fallback remains responsible for emoji/IVS.
	t["cmap"] = makeCmap(charmap(t), filter)
	delete(t, "DSIG")
	head := bytes.Clone(t["head"])
	put32(head, 8, 0)
	t["head"] = head
	keys := []string{}
	for k := range t {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	n := len(keys)
	p := 12 + n*16
	result = make([]byte, p)
	copy(result, data.cut(off, 4))
	put16(result, 4, n)
	power, entry := 1, 0
	for power*2 <= n {
		power *= 2
		entry++
	}
	put16(result, 6, power*16)
	put16(result, 8, entry)
	put16(result, 10, n*16-power*16)
	headPos := 0
	for i, k := range keys {
		v := t[k]
		e := 12 + i*16
		copy(result[e:e+4], k)
		put32(result, e+4, int(checksum(v)))
		put32(result, e+8, p)
		put32(result, e+12, len(v))
		if k == "head" {
			headPos = p
		}
		result = append(result, v...)
		for len(result)%4 != 0 {
			result = append(result, 0)
		}
		p = len(result)
	}
	put32(result, headPos+8, int(uint32(0xB1B0AFBA)-checksum(result)))
	return
}
