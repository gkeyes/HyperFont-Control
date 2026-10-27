package engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"regexp"
	"sort"
)

const Version = "2.4.1"
const MaxFontSize = 256 << 20

var assetPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var tokenPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)
var SlotNames = []string{"chinese", "latinSans", "serif", "monospace"}

type Axis struct {
	Tag     string  `json:"tag"`
	Min     float64 `json:"min"`
	Default float64 `json:"default"`
	Max     float64 `json:"max"`
}
type Face struct {
	Index      int    `json:"index"`
	Name       string `json:"name"`
	PostScript string `json:"postScript"`
	Weight     int    `json:"weight"`
	Italic     bool   `json:"italic"`
	Units      int    `json:"unitsPerEm"`
	Ascender   int    `json:"ascender"`
	Descender  int    `json:"descender"`
	LineGap    int    `json:"lineGap"`
	Axes       []Axis `json:"axes"`
	Chinese    bool   `json:"chinese"`
	Latin      bool   `json:"latin"`
}
type Asset struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Size  int64  `json:"size"`
	Faces []Face `json:"faces"`
}
type FaceRef struct {
	AssetID   string `json:"assetId"`
	FaceIndex int    `json:"faceIndex"`
	Weight    int    `json:"weight"`
	Style     string `json:"style"`
}
type Slot struct {
	Mode                string             `json:"mode"`
	AssetID             string             `json:"assetId,omitempty"`
	FaceIndex           int                `json:"faceIndex,omitempty"`
	ItalicAssetID       string             `json:"italicAssetId,omitempty"`
	ItalicFaceIndex     int                `json:"italicFaceIndex,omitempty"`
	FamilyMode          string             `json:"familyMode,omitempty"`
	Axes                map[string]float64 `json:"axes,omitempty"`
	WeightMap           map[string]float64 `json:"weightMap,omitempty"`
	SavedWeightMap      map[string]float64 `json:"savedWeightMap,omitempty"`
	Faces               []FaceRef          `json:"faces,omitempty"`
	MissingWeightPolicy string             `json:"missingWeightPolicy,omitempty"`
}
type Profile struct {
	SchemaVersion int             `json:"schemaVersion"`
	Name          string          `json:"name"`
	Activation    string          `json:"activation"`
	Slots         map[string]Slot `json:"slots"`
}

func DefaultProfile() Profile {
	p := Profile{1, "系统字体", "manual-reboot", map[string]Slot{}}
	for _, s := range SlotNames {
		p.Slots[s] = Slot{Mode: "system"}
	}
	return p
}
func (p Profile) IsSystem() bool {
	for _, s := range p.Slots {
		if s.Mode != "system" {
			return false
		}
	}
	return true
}

// slotTitles are the category names the panel shows, for messages about one category.
var slotTitles = map[string]string{"chinese": "中文", "latinSans": "无衬线英文", "serif": "衬线英文", "monospace": "等宽英文"}

func (s *Store) lookup(id string, idx int) (Asset, Face, error) {
	if !assetPattern.MatchString(id) {
		return Asset{}, Face{}, fmt.Errorf("字体 ID 无效")
	}
	var a Asset
	if err := readJSON(s.data("library", id+".json"), &a); err != nil {
		return a, Face{}, fmt.Errorf("字体未导入: %.12s", id)
	}
	if idx < 0 || idx >= len(a.Faces) {
		return a, Face{}, fmt.Errorf("字体集合索引超出范围")
	}
	return a, a.Faces[idx], nil
}
func (s *Store) Validate(p Profile) ([]string, error) {
	var warnings []string
	if p.SchemaVersion != 1 || p.Activation != "manual-reboot" || len(p.Name) == 0 || len([]rune(p.Name)) > 80 {
		return nil, fmt.Errorf("配置版本、名称或生效方式无效")
	}
	if len(p.Slots) != len(SlotNames) {
		return nil, fmt.Errorf("需要完整的四个字体分类")
	}
	for _, key := range SlotNames {
		v, ok := p.Slots[key]
		if !ok {
			return nil, fmt.Errorf("缺少分类 %s", key)
		}
		if v.Mode == "system" {
			if v.AssetID != "" || v.ItalicAssetID != "" || v.FamilyMode != "" || len(v.Axes)+len(v.WeightMap)+len(v.SavedWeightMap)+len(v.Faces) > 0 || v.MissingWeightPolicy != "" || v.FaceIndex != 0 || v.ItalicFaceIndex != 0 {
				return nil, fmt.Errorf("%s: 系统模式不接受字体参数", key)
			}
			continue
		}
		checkCoverage := func(f Face) error {
			if key == "chinese" && !f.Chinese {
				return fmt.Errorf("%s: 字体缺少基本汉字", key)
			}
			if key != "chinese" && !f.Latin {
				return fmt.Errorf("%s: 字体缺少基本英文字母", key)
			}
			return nil
		}
		switch v.Mode {
		case "variable":
			if len(v.Faces) > 0 || v.MissingWeightPolicy != "" {
				return nil, fmt.Errorf("可变模式不接受静态参数")
			}
			_, f, e := s.lookup(v.AssetID, v.FaceIndex)
			if e != nil {
				return nil, e
			}
			if e = checkCoverage(f); e != nil {
				return nil, e
			}
			if v.FamilyMode != "instances" && v.FamilyMode != "continuous" {
				return nil, fmt.Errorf("请选择连续或九档映射")
			}
			ax := map[string]Axis{}
			for _, a := range f.Axes {
				ax[a.Tag] = a
			}
			w, ok := ax["wght"]
			if !ok {
				return nil, fmt.Errorf("%s: 字体没有 wght 可变轴", key)
			}
			if f.Italic {
				return nil, fmt.Errorf("正体位置不能选择斜体文件")
			}
			for tag, val := range v.Axes {
				a, ok := ax[tag]
				if !ok || tag == "wght" || math.IsNaN(val) || math.IsInf(val, 0) || val < a.Min || val > a.Max {
					return nil, fmt.Errorf("%s: 可变轴 %s 超出范围或重复指定", key, tag)
				}
			}
			if v.FamilyMode == "continuous" {
				if len(v.WeightMap) > 0 {
					return nil, fmt.Errorf("连续模式不接受九档映射")
				}
				warnings = append(warnings, key+": 新格式使用连续 wght；旧格式降级为九档实例。实际连续效果需应用验证。")
			} else {
				if len(v.WeightMap) != 9 {
					return nil, fmt.Errorf("九档映射必须完整")
				}
				prev := -math.MaxFloat64
				for i := 100; i <= 900; i += 100 {
					n, ok := v.WeightMap[fmt.Sprint(i)]
					switch {
					case !ok || math.IsNaN(n) || math.IsInf(n, 0):
						return nil, fmt.Errorf("%s: %d 档轴值未填写", key, i)
					case n < w.Min || n > w.Max:
						return nil, fmt.Errorf("%s: %d 档轴值 %g 超出字体范围 %g–%g", key, i, n, w.Min, w.Max)
					case n < prev:
						return nil, fmt.Errorf("%s: %d 档轴值 %g 小于上一档的 %g，九档须从细到粗", key, i, n, prev)
					}
					prev = n
				}
			}
			if len(v.SavedWeightMap) > 0 {
				if v.FamilyMode != "continuous" || len(v.SavedWeightMap) != 9 {
					return nil, fmt.Errorf("仅连续模式允许保留完整的九档设置")
				}
				prev := -math.MaxFloat64
				for i := 100; i <= 900; i += 100 {
					n, ok := v.SavedWeightMap[fmt.Sprint(i)]
					if !ok || math.IsNaN(n) || math.IsInf(n, 0) || n < w.Min || n > w.Max || n < prev {
						return nil, fmt.Errorf("%s: 保留的 %d 档设置无效", key, i)
					}
					prev = n
				}
			}
			if v.ItalicAssetID != "" {
				_, it, e := s.lookup(v.ItalicAssetID, v.ItalicFaceIndex)
				if e != nil {
					return nil, e
				}
				if e = checkCoverage(it); e != nil {
					return nil, e
				}
				if !it.Italic {
					return nil, fmt.Errorf("斜体位置需要真实斜体文件")
				}
				ia := map[string]Axis{}
				for _, a := range it.Axes {
					ia[a.Tag] = a
				}
				iw, ok := ia["wght"]
				if !ok {
					return nil, fmt.Errorf("斜体没有 wght 轴")
				}
				if iw.Min > w.Min || iw.Max < w.Max {
					return nil, fmt.Errorf("正斜体 wght 范围不兼容")
				}
				for tag, val := range v.Axes {
					a, ok := ia[tag]
					if !ok || val < a.Min || val > a.Max {
						return nil, fmt.Errorf("斜体轴 %s 不兼容", tag)
					}
				}
			} else {
				warnings = append(warnings, key+": 未提供真实斜体，应用可能合成倾斜。")
			}
		case "static":
			if v.AssetID != "" || v.ItalicAssetID != "" || v.FamilyMode != "" || len(v.Axes)+len(v.WeightMap)+len(v.SavedWeightMap) > 0 {
				return nil, fmt.Errorf("静态模式不接受可变参数")
			}
			if len(v.Faces) == 0 {
				return nil, fmt.Errorf("“%s”选了静态多字重但还没有字体文件，请在“① 选择字体”选一个字体家族", slotTitles[key])
			}
			if len(v.Faces) > 18 || (v.MissingWeightPolicy != "reject" && v.MissingWeightPolicy != "nearest") {
				return nil, fmt.Errorf("静态字重配置无效")
			}
			seen := map[string]bool{}
			normal := 0
			for _, r := range v.Faces {
				if r.Weight < 100 || r.Weight > 900 || r.Weight%100 != 0 || (r.Style != "normal" && r.Style != "italic") {
					return nil, fmt.Errorf("静态字重或样式无效")
				}
				k := fmt.Sprint(r.Weight) + r.Style
				if seen[k] {
					return nil, fmt.Errorf("同一字重样式重复")
				}
				seen[k] = true
				_, f, e := s.lookup(r.AssetID, r.FaceIndex)
				if e != nil {
					return nil, e
				}
				if len(f.Axes) > 0 {
					return nil, fmt.Errorf("请用可变模式配置带轴字体")
				}
				if e = checkCoverage(f); e != nil {
					return nil, e
				}
				if (r.Style == "italic") != f.Italic {
					return nil, fmt.Errorf("声明的样式与字体实际正斜体不符")
				}
				if r.Style == "normal" {
					normal++
				}
				if r.Weight != f.Weight {
					warnings = append(warnings, fmt.Sprintf("%s: 请求 %d 使用文件声明的 %d 字重；不会改变字形", key, r.Weight, f.Weight))
				}
			}
			if normal == 0 || v.MissingWeightPolicy == "reject" && normal != 9 {
				return nil, fmt.Errorf("缺少正体字重；补齐九档或选择最近字重")
			}
			if normal < 9 {
				warnings = append(warnings, key+": 缺档由 Android 选择最近的已有字重。")
			}
		default:
			return nil, fmt.Errorf("不支持的字体模式 %q", v.Mode)
		}
	}
	if !p.IsSystem() {
		warnings = append(warnings, "应用主动使用的私有字体和网页下载字体不由系统字体表控制；小米默认字重适配需启用 Zygisk 并重启。", "保留字体原始行距；导入不同字体可能改变界面排版，请在通知与设置中验收。")
	}
	sort.Strings(warnings)
	return warnings, nil
}
func strictJSON(b []byte, dst any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return err
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return fmt.Errorf("多余 JSON 内容")
	}
	return nil
}

func DecodeRequest(b []byte) (Request, error) {
	var r Request
	e := strictJSON(b, &r)
	return r, e
}
