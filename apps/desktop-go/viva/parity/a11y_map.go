package parity

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// 本文件是 L1 结构比较的归一化规则（docs/agents/viva-ui-development.md §2.2），由 a11y_map_test.go 的哈希守卫固定：
// 改动本文件会让守卫变红，必须在台账里写明理由后才能更新哈希。不要为了让某个场景通过而放宽这里的规则。

// reactIgnoredRoles 是不参与结构比较的 Chromium 角色：结构性容器与纯文本。
// 文字内容由「文字序列」单独比较，所以 StaticText 不在可访问节点序列里。
var reactIgnoredRoles = map[string]bool{
	"RootWebArea":   true,
	"generic":       true,
	"none":          true,
	"presentation":  true,
	"StaticText":    true,
	"InlineTextBox": true,
	"LineBreak":     true,
	"paragraph":     true,
	"Section":       true,
	"LabelText":     true,
	"Abbr":          true,
	// 地标：Viva 没有对应的角色（gfx.Role 里没有 banner / complementary / navigation / main），
	// 它们只是把可访问节点分区，本身没有状态与操作；其中的按钮、标题等照常逐个比较。登记见台账 A-004。
	"banner":        true,
	"complementary": true,
	"navigation":    true,
	"main":          true,
}

// reactIgnoredUnnamed 是「无名时不参与结构比较」的 Chromium 角色：没有可访问名的图片是装饰性图标
// （lucide 的 SVG 在 Chromium 里是无名 image），Viva 的图标不进入可访问树。有名字的图片仍必须逐个决定怎么映射。
var reactIgnoredUnnamed = map[string]bool{
	"image": true,
}

// reactRoleMap 把 Chromium 的 AX 角色换成 Viva 的角色名（gfx.Role.String）。
// 不在这里也不在忽略表里的角色会被报告为「未映射」，逼着在这里明确决定，而不是悄悄放过。
var reactRoleMap = map[string]string{
	"heading":   "heading",
	"switch":    "switch",
	"button":    "button",
	"checkbox":  "checkbox",
	"radio":     "radio",
	"textbox":   "textfield",
	"searchbox": "textfield",
	"combobox":  "combobox",
	"slider":    "slider",
	"link":      "link",
	"tab":       "tab",
	"menuitem":  "menuitem",
	"menu":      "menu",
	"menubar":   "menubar",
	"listbox":   "listbox",
	"option":    "option",
	"separator": "separator",
	"status":    "status",
	"grid":      "grid",
	"gridcell":  "gridcell",
	"group":     "group",
}

// vivaIgnoredRoles 是 Viva 一侧不算可访问节点的角色：未声明角色的区域。
var vivaIgnoredRoles = map[string]bool{
	"region": true,
	"":       true,
}

// popupHasValues 是 aria-haspopup 的取值里让 button 变成「下拉触发器（combobox）」的那些。
var popupHasValues = map[string]bool{"listbox": true, "menu": true, "true": true, "dialog": false}

// roleStates 是各角色参与比较的状态属性。其余角色只比较角色与名称。
var roleStates = map[string][]string{
	"switch":    {"checked"},
	"checkbox":  {"checked"},
	"radio":     {"checked"},
	"heading":   {"level"},
	"combobox":  {"expanded", "value"},
	"tab":       {"selected"},
	"option":    {"selected"},
	"menuitem":  {},
	"slider":    {"value"},
	"textfield": {"value"},
}

// NormText 做名称与文字的归一化：NFC，所有 Unicode 空白折成一个空格，去掉首尾空白。
func NormText(s string) string {
	s = norm.NFC.String(s)
	var b strings.Builder
	space := false
	for _, r := range s {
		if unicode.IsSpace(r) || r == '\u00a0' {
			space = true
			continue
		}
		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}
		space = false
		b.WriteRune(r)
	}
	return b.String()
}

// SquashText 在 NormText 之上去掉全部空白：折行处的空白在两边取舍不同，比较折行文字时用它。
func SquashText(s string) string {
	return strings.ReplaceAll(NormText(s), " ", "")
}

// Norm 是归一化后的可访问节点：比较的最小单位。
type Norm struct {
	Role   string
	Name   string
	States map[string]string // 参与比较的状态，键见 roleStates，值统一成字符串
	Box    Box
	HasBox bool
	Source int // 在原事实 Nodes 里的序号
}

// Key 是对齐用的键：角色加名称。
func (n Norm) Key() string { return n.Role + "\x00" + n.Name }

// StateString 把状态排成稳定的字符串，便于比较与打印。
func (n Norm) StateString() string {
	var parts []string
	for _, k := range []string{"checked", "expanded", "selected", "level", "value", "disabled"} {
		if v, ok := n.States[k]; ok {
			parts = append(parts, k+"="+v)
		}
	}
	return strings.Join(parts, " ")
}

// NormalizeReact 把 React 的 AX 节点序列归一化。unmapped 返回遇到的未映射角色（去重，按出现顺序）。
func NormalizeReact(f *Facts) (out []Norm, unmapped []string) {
	seenUnmapped := map[string]bool{}
	for i, n := range f.Nodes {
		if reactIgnoredRoles[n.Role] {
			continue
		}
		box, hasBox := n.BoxOK()
		if hasBox {
			// 区域外的节点由采集器过滤；这里再保险一次，并把外框裁到区域内。
			box = box.Clip(f.Region.Box())
			if box.W() == 0 || box.H() == 0 {
				continue
			}
		}
		if reactIgnoredUnnamed[n.Role] && NormText(n.Name) == "" {
			continue
		}
		role, ok := reactRoleMap[n.Role]
		if !ok {
			if !seenUnmapped[n.Role] {
				seenUnmapped[n.Role] = true
				unmapped = append(unmapped, n.Role)
			}
			continue
		}
		name := NormText(n.Name)
		if role == "group" && name == "" {
			continue // 无名分组只是结构
		}
		states := map[string]string{}
		if role == "button" {
			if v, has := n.Prop("hasPopup"); has && popupHasValues[v] {
				role = "combobox"
			}
		}
		for _, key := range roleStates[role] {
			switch key {
			case "value":
				if role == "combobox" {
					// 触发器当前值是按钮里的文字（Chromium 不把它放进 value）。
					states["value"] = NormText(reactButtonText(f, i))
				} else if v := NormText(n.Value); v != "" {
					states["value"] = v
				}
			default:
				if v, has := n.Prop(key); has {
					states[key] = v
				}
			}
		}
		if v, has := n.Prop("disabled"); has && v == "true" {
			states["disabled"] = "true"
		}
		out = append(out, Norm{Role: role, Name: name, States: states, Box: box, HasBox: hasBox, Source: i})
	}
	return out, unmapped
}

// reactButtonText 返回节点（按钮）下全部 StaticText 的文字，按顺序拼接。
func reactButtonText(f *Facts, idx int) string {
	var parts []string
	for j := idx + 1; j < len(f.Nodes); j++ {
		if f.Nodes[j].Depth <= f.Nodes[idx].Depth {
			break
		}
		if f.Nodes[j].Role == "StaticText" {
			parts = append(parts, f.Nodes[j].Name)
		}
	}
	return strings.Join(parts, " ")
}

// NormalizeViva 把 Viva 的可访问节点序列归一化（节点已由 Viva 采集器写成与 React 同形的事实）。
//
// 忽略：没有角色的区域；无名且不可聚焦的节点（行级悬停命中区，对辅助技术没有意义，渲染层对应的 div 也不在 AX 树里）。
func NormalizeViva(f *Facts) []Norm {
	var out []Norm
	for i, n := range f.Nodes {
		if vivaIgnoredRoles[n.Role] {
			continue
		}
		name := NormText(n.Name)
		if focusable, has := n.Prop("focusable"); name == "" && (!has || focusable != "true") {
			continue
		}
		box, hasBox := n.BoxOK()
		if hasBox {
			box = box.Clip(f.Region.Box())
			if box.W() == 0 || box.H() == 0 {
				continue
			}
		}
		states := map[string]string{}
		for _, key := range roleStates[n.Role] {
			if key == "value" {
				if v := NormText(n.Value); v != "" {
					states["value"] = v
				}
				continue
			}
			if v, has := n.Prop(key); has {
				states[key] = v
			}
		}
		if v, has := n.Prop("disabled"); has && v == "true" {
			states["disabled"] = "true"
		}
		out = append(out, Norm{Role: n.Role, Name: name, States: states, Box: box, HasBox: hasBox, Source: i})
	}
	return out
}
