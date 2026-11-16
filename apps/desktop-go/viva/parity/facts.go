// Package parity 是渲染层（React）与 Viva 界面的对照工具：两侧各采一份「事实」（可访问树、外框、文字几何、
// 绘制元素）与一张 PNG，再按 L1 结构、L2 度量、L3 像素三层比较，输出报告。L4 行为由各屏幕的无头会话测试承担。
//
// 事实的 JSON 形状由 apps/desktop-go/scripts/parity/lib/react-facts.mjs 定义（schema 2），Viva 一侧写同一形状，
// 所以两边可以用同一套比较代码。定义见 docs/agents/viva-ui-development.md §2。
//
// Package parity compares the renderer's rendering with the Viva UI.
package parity

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// SchemaVersion 是事实 JSON 的版本；React 采集器与本包必须一致。
const SchemaVersion = 2

// Box 是一个矩形：x、y、宽、高，单位 CSS 像素 / 逻辑像素，坐标相对区域左上角。
type Box [4]float64

func (b Box) X() float64 { return b[0] }
func (b Box) Y() float64 { return b[1] }
func (b Box) W() float64 { return b[2] }
func (b Box) H() float64 { return b[3] }

// Right 与 Bottom 是右、下边界。
func (b Box) Right() float64  { return b[0] + b[2] }
func (b Box) Bottom() float64 { return b[1] + b[3] }

// Clip 返回 b 与 r 的交集；不相交时返回零尺寸的盒子。
func (b Box) Clip(r Box) Box {
	x0, y0 := max(b[0], r[0]), max(b[1], r[1])
	x1, y1 := min(b.Right(), r.Right()), min(b.Bottom(), r.Bottom())
	if x1 <= x0 || y1 <= y0 {
		return Box{x0, y0, 0, 0}
	}
	return Box{x0, y0, x1 - x0, y1 - y0}
}

// Variant 是一个场景的外观与视口维度；键与 React 采集器的 variantKey 相同。
type Variant struct {
	Mode     string `json:"mode"`     // light | dark
	Hue      string `json:"hue"`      // 色相预设 ID
	Font     string `json:"font"`     // 字号档 ID：small | medium | large
	Viewport string `json:"viewport"` // 如 1280x800
}

// Key 是变体键，如 light-royal-blue-medium-1280x800。
func (v Variant) Key() string {
	return strings.Join([]string{v.Mode, v.Hue, v.Font, v.Viewport}, "-")
}

// Size 解析视口宽高。
func (v Variant) Size() (int, int, error) {
	w, h, ok := strings.Cut(v.Viewport, "x")
	if !ok {
		return 0, 0, fmt.Errorf("视口 %q 不是 宽x高", v.Viewport)
	}
	wi, err1 := strconv.Atoi(w)
	hi, err2 := strconv.Atoi(h)
	if err1 != nil || err2 != nil || wi <= 0 || hi <= 0 {
		return 0, 0, fmt.Errorf("视口 %q 不是 宽x高", v.Viewport)
	}
	return wi, hi, nil
}

// Region 是被比较的区域在窗口里的位置与尺寸（React 侧由 selector 取得）。
type Region struct {
	X        int    `json:"x"`
	Y        int    `json:"y"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	Selector string `json:"selector,omitempty"`
}

// Box 是区域自身的盒子（原点为 0,0）。
func (r Region) Box() Box { return Box{0, 0, float64(r.Width), float64(r.Height)} }

// Capture 记录采集环境。
type Capture struct {
	DPR      float64 `json:"dpr"`
	Chromium string  `json:"chromium,omitempty"`
	Engine   string  `json:"engine,omitempty"`
	// ForcedDSF 为 true 表示 React 侧以 --force-device-scale-factor=1 采集，排版取整不带宿主显示缩放的印记。
	// 没有这个标记的参照来自 2026-11-16 之前的采集器（200% 缩放机器上文字基线按 2× 取整），不可用于比较。
	ForcedDSF bool `json:"forcedDsf,omitempty"`
}

// Node 是可访问树里的一个节点（React 侧来自 Chromium 的完整 AX 树，Viva 侧来自 Session.Access）。
type Node struct {
	Role        string         `json:"role"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Value       string         `json:"value,omitempty"`
	Props       map[string]any `json:"props,omitempty"`
	Box         []float64      `json:"box"` // 无外框（未渲染）时为 null
	Kind        string         `json:"kind,omitempty"`
	Tag         string         `json:"tag,omitempty"`
	TestID      string         `json:"testId,omitempty"`
	Text        string         `json:"text,omitempty"`
	Parent      int            `json:"parent"`
	Depth       int            `json:"depth"`
}

// BoxOK 返回外框；没有外框时 ok 为 false。
func (n Node) BoxOK() (Box, bool) {
	if len(n.Box) != 4 {
		return Box{}, false
	}
	return Box{n.Box[0], n.Box[1], n.Box[2], n.Box[3]}, true
}

// Prop 取状态属性并统一成字符串：React 的 "true"/"false" 与 Viva 的布尔都变成 "true"/"false"，数字取整数写法。
func (n Node) Prop(key string) (string, bool) {
	v, ok := n.Props[key]
	if !ok {
		return "", false
	}
	switch x := v.(type) {
	case string:
		return x, true
	case bool:
		return strconv.FormatBool(x), true
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64), true
	default:
		return fmt.Sprint(x), true
	}
}

// Text 是一段文字：原文、每个行片段的矩形、字体与行高、颜色。
//
// React 侧的矩形是 Range 的内容区矩形（高 = ascent + descent）；Viva 侧的矩形是文字盒（行盒），
// 两者的差异由比较端按各自语义处理，见 compare_text.go。
type Text struct {
	Text       string    `json:"text"`
	Rects      []Box     `json:"rects"`
	Font       string    `json:"font,omitempty"`
	Size       float64   `json:"size"`
	Weight     float64   `json:"weight"`             // CSS 字重：400 / 500 / 600 / 700
	LineHeight *float64  `json:"lineHeight"`         // null 表示 normal
	Color      string    `json:"color,omitempty"`    // CSS 颜色串
	Tag        string    `json:"tag,omitempty"`      // React：所在元素
	TestID     string    `json:"testId,omitempty"`   // React：最近的 data-testid
	Painted    []float64 `json:"painted,omitempty"`  // Viva：渲染引擎量出的绘制宽度（每行一个）
	Baseline   []float64 `json:"baseline,omitempty"` // Viva：基线的 y 坐标（每行一个）
}

// Paint 是一个有绘制的元素（React：背景/边框/圆角/阴影/轮廓/SVG；Viva：图元归并后的同类描述）。
type Paint struct {
	Order   int      `json:"order"`
	Tag     string   `json:"tag,omitempty"`
	TestID  string   `json:"testId,omitempty"`
	Box     Box      `json:"box"`
	Bg      string   `json:"bg,omitempty"`
	BgImage string   `json:"bgImage,omitempty"`
	Shadow  string   `json:"shadow,omitempty"`
	Radius  []string `json:"radius,omitempty"`
	Opacity float64  `json:"opacity"`
	Color   string   `json:"color,omitempty"`
}

// Font 是一个字体键对应的平台字体取证（React：CSS.getPlatformFontsForNode）。
type Font struct {
	Key    string `json:"key"`
	Family string `json:"family"`
	Size   string `json:"size"`
	Weight string `json:"weight"`
	Style  string `json:"style"`
}

// Facts 是一个场景在一个变体下的全部可比较事实。
type Facts struct {
	Schema    int     `json:"schema"`
	Scene     string  `json:"scene"`
	Variant   Variant `json:"variant"`
	Region    Region  `json:"region"`
	Capture   Capture `json:"capture"`
	Nodes     []Node  `json:"nodes"`
	Texts     []Text  `json:"texts"`
	TextRects []Box   `json:"textRects"`
	Paints    []Paint `json:"paints"`
	// Icons 是 Viva 一侧画出的图标的外框（经 kit.Look.IconSink 记录）；React 一侧的图标取自 Paints 里 tag 为 svg 的项，见 reactIcons。
	Icons []Box  `json:"icons,omitempty"`
	Fonts []Font `json:"fonts,omitempty"`
	Aria  string `json:"aria,omitempty"`
}

// LoadFacts 读事实 JSON；版本不符时明确报错，不静默接受。
func LoadFacts(path string) (*Facts, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f Facts
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("%s：解析事实失败：%w", path, err)
	}
	if f.Schema != SchemaVersion {
		return nil, fmt.Errorf("%s：事实 schema 是 %d，需要 %d（重新采集）", path, f.Schema, SchemaVersion)
	}
	return &f, nil
}

// Save 把事实写成缩进的 JSON。
func (f *Facts) Save(path string) error {
	data, err := json.MarshalIndent(f, "", " ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
