package kit

import (
	"encoding/json"
	"math"

	"github.com/egoist/mygo/ui"
)

// PaneSpec 描述一条可拖动窗格宽度的默认值与安全范围。
type PaneSpec struct {
	Key               string // PrefStore 里的键
	Default, Min, Max float32
}

// 三条窗格宽度的默认值与范围，与渲染层 stores/ui.store.ts 的常量一致
// （pane_test.go 的 TestPaneSpecsMatchRenderer 读那份源码对拍，改一边忘另一边会失败）。
var (
	PaneSidebar     = PaneSpec{Key: "ui.sidebarPanelWidth", Default: 288, Min: 220, Max: 640}
	PaneItemList    = PaneSpec{Key: "ui.itemListPaneWidth", Default: 320, Min: 240, Max: 720}
	PaneWikiCatalog = PaneSpec{Key: "ui.wikiCatalogPaneWidth", Default: 300, Min: 220, Max: 560}
)

// Clamp 把宽度夹进 [Min, Max]；非有限数取 Min（与渲染层 clamp 一致）。
func (s PaneSpec) Clamp(v float32) float32 {
	if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
		return s.Min
	}
	return min(max(v, s.Min), s.Max)
}

// Pane 是一条窗格宽度的状态：读取已保存的值、夹紧、去抖保存。
// 只在 UI 线程读写（Set 触发的写入在去抖定时器的 goroutine 里，store 必须并发安全）。
type Pane struct {
	spec  PaneSpec
	store PrefStore
	width float32
	save  saver
}

// NewPane 读取已保存的宽度；没有、损坏或越界时取默认值/夹紧后的值。store 为 nil 时只在内存里记。
func NewPane(spec PaneSpec, store PrefStore) *Pane {
	p := &Pane{spec: spec, store: store, width: spec.Clamp(spec.Default)}
	if store == nil {
		return p
	}
	if raw, ok := store.Get(spec.Key); ok {
		// 指针接 null：渲染层 `value ?? 默认值`，null 取默认值而不是夹成下限
		var v *float64
		if err := json.Unmarshal(raw, &v); err == nil && v != nil && !math.IsNaN(*v) && !math.IsInf(*v, 0) {
			p.width = spec.Clamp(float32(*v))
		}
	}
	return p
}

// Spec 返回这条窗格的规格。
func (p *Pane) Spec() PaneSpec { return p.spec }

// Width 返回当前宽度。
func (p *Pane) Width() float32 { return p.width }

// Set 设置宽度（夹紧）；有变化才去抖保存。
func (p *Pane) Set(w float32) {
	w = p.spec.Clamp(w)
	if w == p.width {
		return
	}
	p.width = w
	if p.store == nil {
		return
	}
	store, key := p.store, p.spec.Key
	p.save.schedule(func() error { return store.Set(key, w) })
}

// Reset 回到默认宽度。
func (p *Pane) Reset() { p.Set(p.spec.Default) }

// Flush 立即保存还没写出去的改动；窗口关闭前调用。
func (p *Pane) Flush() { p.save.flush() }

// Split 把 first、second 左右并排，中间是可拖动的分隔条：first 的宽度就是本窗格的宽度，
// 拖动、方向键调整、双击恢复默认都落到 Set（夹紧并去抖保存）。
//
// 分隔条的外观与键盘行为是 MyGo 控件自带的，与渲染层 ColumnResizer（8px 命中区、1px 线、
// ←/→ 每次 16px）不是逐像素一致，见 docs/agents/native-ui-development.md 的对等差异表。
func (p *Pane) Split(c *ui.Context, first, second func()) ui.Element {
	w := p.width
	el := ui.Split(c, &w, first, second).SplitReset(p.spec.Default)
	p.Set(w)
	return el
}
