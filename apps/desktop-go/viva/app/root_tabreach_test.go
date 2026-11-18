package app

import (
	_ "embed"
	"encoding/json"
	"math"
	"testing"

	"github.com/vivaui/viva/gfx"
)

// reactTabProbeJSON 是 React 参照里在显示设置页（800×600、浅色、宝蓝、中字号）连续按 Tab 的实测记录：
// 每次按键后聚焦的控件名、外框，以及内容区的滚动位置。采集脚本 scripts/parity/tools/probe-tab-reach.mjs。
//
//go:embed testdata/tab-reach-react-800x600.json
var reactTabProbeJSON []byte

type reactTabPress struct {
	N         int     `json:"n"`
	Tag       string  `json:"tag"`
	Name      string  `json:"name"`
	Top       float64 `json:"top"`
	Bottom    float64 `json:"bottom"`
	ScrollTop float64 `json:"scrollTop"`
}

// 探针从「显示设置」导航项开始按 Tab（场景进入时点击过它，焦点留在上面）。
// 比对前 38 次：设置导航（1–7）、内容区（8–27：含两次要滚动才够得着的跳转与滚到最大偏移）、
// 左侧栏（28–33）、顶栏（34–38：展开、搜索框、命令面板提示钮、新建、创建方式）。
// 第 39、40 次是窗口的最小化、最大化按钮：Viva 自己绘制，不在 Tab 环里（台账 A-005），不比。
const reactTabPresses = 38

// 台账 B-030：Tab 在内容区里逐个走到每个控件（包括滚出视野的），并把它滚进视野；
// 聚焦时的外框与 React（Chromium）一致——第 21 次按键之后滚动 357px、第 26 次滚到最大偏移，控件的位置与 React 逐个对上。
func TestRootTabReachMatchesReact(t *testing.T) {
	var probe struct {
		Presses []reactTabPress `json:"presses"`
	}
	if err := json.Unmarshal(reactTabProbeJSON, &probe); err != nil {
		t.Fatalf("读不懂 React 探针记录：%v", err)
	}
	if len(probe.Presses) < reactTabPresses {
		t.Fatalf("React 探针记录只有 %d 次按键，期望至少 %d", len(probe.Presses), reactTabPresses)
	}

	f := openRootSize(t, 800, 600, func(o *Options) {
		// 与生产一致：顶栏的快捷键提示钮（命令面板入口）在宽度够时才画，且要有生效的组合键。
		o.SearchShortcut, o.CaptureShortcut = "Alt+Shift+F", "Alt+Shift+N"
	})
	if err := f.session.ClickLabel("显示设置"); err != nil {
		t.Fatalf("点击导航「显示设置」失败：%v", err)
	}
	f.frames(t, 4)

	for _, want := range probe.Presses[:reactTabPresses] {
		if err := f.session.Keys("tab"); err != nil {
			t.Fatalf("第 %d 次 Tab 失败：%v", want.N, err)
		}
		f.frames(t, 3)
		label, bounds, ok := focusedNode(f)
		if !ok {
			t.Fatalf("第 %d 次 Tab（React：%s「%s」）之后没有聚焦任何控件", want.N, want.Tag, want.Name)
		}
		// 名称：React 的输入框没有可访问名（滑块、搜索框），只比有名字的控件；Viva 给了它们名字，是 L1 已登记的补充。
		if want.Name != "" && label != want.Name {
			t.Errorf("第 %d 次 Tab：聚焦「%s」，React 是「%s」", want.N, label, want.Name)
		}
		// 位置：与 React 的外框在 0.2px 之内（React 的读数取到 0.1px）。
		if top, bottom := float64(bounds.Y), float64(bounds.Y+bounds.Height); !near(top, want.Top, 0.2) || !near(bottom, want.Bottom, 0.2) {
			t.Errorf("第 %d 次 Tab（%s「%s」，React 滚动位置 %.0f）：外框 top=%.1f bottom=%.1f，React top=%.1f bottom=%.1f",
				want.N, want.Tag, want.Name, want.ScrollTop, top, bottom, want.Top, want.Bottom)
		}
	}
}

// focusedNode 取可访问树里当前持有键盘焦点的节点。
func focusedNode(f *rootFixture) (label string, bounds gfx.Rect, ok bool) {
	for _, node := range f.session.Access() {
		if node.Focused {
			return node.Label, node.Bounds, true
		}
	}
	return "", gfx.Rect{}, false
}

// near 报告两个坐标是否在 tol 之内。
func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol }
