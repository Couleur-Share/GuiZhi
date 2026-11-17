package kit

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/tokens"
)

// ColumnResizer 是竖向的拖拽把手（渲染层 components/ui/ColumnResizer.tsx）：调整它左侧一栏的宽度。
//
// 行为与渲染层一致：
//   - 按下主键开始拖动，移动时宽度 = 起点宽度 + 指针位移，夹在 [Min, Max]；拖动期间指针被捕获，可以移出把手。
//   - 双击回到 Default；键盘 ←/→ 每次 16 px，Shift 加成 64 px；Home、End、回车、空格回到 Default。
//   - 命中区 8 px 宽（w-2），里面画一条 1 px 的竖线：平时 border/40，悬停 primary/60，拖动中 primary/80，
//     键盘聚焦时整个命中区铺 primary/20。
//
// 宽度读写都在 ColumnResizerState.Width：调用方用它去定那一栏的宽度，拖动在绘制阶段更新它并请求重绘。
// 保存由 OnCommit 负责——拖动结束、按键与双击才调用，不在每次指针移动时写盘。

// 键盘步长（渲染层 ColumnResizer.handleKeyDown）。
const (
	columnResizerStep      = 16
	columnResizerStepShift = 64
)

// ColumnResizerState 是一条把手的状态，由持有它的窗口保存。
type ColumnResizerState struct {
	// Width 是当前宽度（逻辑像素）；首帧前用已保存的值初始化，缺省 0 表示用 Default。
	Width float32

	dragging           bool
	startX, startWidth float32
}

// Dragging 报告把手是否正在被拖动。
func (s *ColumnResizerState) Dragging() bool { return s.dragging }

// ColumnResizerSpec 是一条把手的固定参数。
type ColumnResizerSpec struct {
	// Name 是可访问名（渲染层 aria-label）。
	Name string
	// Min、Max、Default 是宽度的下限、上限与双击/回车回到的值。
	Min, Max, Default float32
	// BarAtStart 让竖线贴在命中区最左边（barPosition="start"）；缺省居中。
	BarAtStart bool
	// OnCommit 在宽度因拖动结束、按键或双击而定下来之后调用，用来保存；可空。
	OnCommit func(width float32)
}

func clampWidth(v, lo, hi float32) float32 { return min(max(v, lo), hi) }

// ColumnResizer 声明把手：8 px 宽、撑满父容器高度，不收缩。调用方把它放进一个定位好的容器（通常是绝对定位在栏的右缘）。
func (l *Look) ColumnResizer(c ui.C, st *ColumnResizerState, spec ColumnResizerSpec) ui.Box {
	if st.Width == 0 {
		st.Width = clampWidth(spec.Default, spec.Min, spec.Max)
	}
	id := ui.ID(c)
	box := ui.Column(c).Width(l.Rem(0.5)).FillHeight().Shrink(0).HitWith(id, spec.Name, gfx.HitOptions{
		Role:   gfx.RoleSeparator,
		Cursor: gfx.PointerCursorResizeHorizontal,
		// 渲染层是 role=separator 加 aria-valuenow / valuemin / valuemax：当前宽度与上下限。
		Range: gfx.ValueRange{Now: float64(st.Width), Min: float64(spec.Min), Max: float64(spec.Max), Set: true},
	})
	commit := func(width float32) {
		st.Width = width
		if spec.OnCommit != nil {
			spec.OnCommit(width)
		}
	}
	// 键盘：与渲染层相同的步长；快捷键只在把手有焦点时生效。
	for _, step := range []struct {
		mods gfx.Modifiers
		key  string
		to   func() float32
	}{
		{0, "left", func() float32 { return st.Width - columnResizerStep }},
		{0, "right", func() float32 { return st.Width + columnResizerStep }},
		{ui.Shift, "left", func() float32 { return st.Width - columnResizerStepShift }},
		{ui.Shift, "right", func() float32 { return st.Width + columnResizerStepShift }},
		{0, "home", func() float32 { return spec.Default }},
		{0, "end", func() float32 { return spec.Default }},
		{0, "enter", func() float32 { return spec.Default }},
		{0, " ", func() float32 { return spec.Default }}, // 空格键按字符给出：Shortcut 的键名表里没有 "space"
	} {
		if box.Shortcut(step.mods, step.key) {
			commit(clampWidth(step.to(), spec.Min, spec.Max))
		}
	}
	hovered := box.State().Hovered
	return box.Draw(func(d *ui.DrawContext) {
		canvas := d.Canvas
		if ev, ok := canvas.PointerOn(id); ok {
			switch {
			case ev.Kind == gfx.PointerDown && ev.Button <= gfx.PointerButtonPrimary:
				if ev.ClickCount >= 2 {
					// 双击回到默认宽度（第一下按下已经开始了拖动，位移为零，等于没有动）。
					st.dragging = false
					commit(clampWidth(spec.Default, spec.Min, spec.Max))
				} else {
					st.dragging, st.startX, st.startWidth = true, ev.X, st.Width
				}
				canvas.Consume()
			case ev.Kind == gfx.PointerMove && st.dragging:
				st.Width = clampWidth(st.startWidth+ev.X-st.startX, spec.Min, spec.Max)
				canvas.Consume()
			case ev.Kind == gfx.PointerUp && st.dragging:
				st.dragging = false
				commit(st.Width)
				canvas.Consume()
			}
		}
		scale := canvas.PixelScale()
		if d.State.FocusVisible {
			canvas.Fill(SnapRect(d.Rect, scale), d.Fade(l.Col(l.T.Primary.WithAlpha(0.2))))
		}
		// 竖线：1 px 宽，居中（left-1/2 -translate-x-1/2，落在半像素上，再吸附）或贴左。
		barX := d.Rect.X + d.Rect.Width/2 - 0.5
		if spec.BarAtStart {
			barX = d.Rect.X
		}
		bar := SnapRect(gfx.Rect{X: barX, Y: d.Rect.Y, Width: 1, Height: d.Rect.Height}, scale)
		canvas.Fill(bar, d.Fade(l.Col(l.barTone(canvas, id, st.dragging, hovered))))
	})
}

// barTone 是竖线颜色（transition-colors duration-quick）：平时 border/40，悬停 primary/60，拖动 primary/80。
// 用两个独立的过渡进度（悬停、拖动）叠加，目标变化时从当前显示的颜色出发，悬停离开或拖动开始都不会跳变。
func (l *Look) barTone(canvas *gfx.Canvas, id gfx.ID, dragging, hovered bool) tokens.Color {
	hover := l.hoverTo(canvas, id, 0, hovered || dragging, DurationQuick)
	drag := l.hoverTo(canvas, id, 1, dragging, DurationQuick)
	t := l.T
	base := MixToken(t.Border.WithAlpha(0.4), t.Primary.WithAlpha(0.6), hover)
	return MixToken(base, t.Primary.WithAlpha(0.8), drag)
}
