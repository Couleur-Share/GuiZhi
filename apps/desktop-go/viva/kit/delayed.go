package kit

import (
	"time"

	"github.com/vivaui/viva/gfx"
)

// Delayed 是延迟登场的指示器（渲染层 globals.css 的 delayed-fade-in）：出现后先隐藏 LoadingDelay（250 ms），
// 再按 base 档时长以 ease-enter 淡入。本地 SQLite 走一趟通常几十毫秒就回来，立刻画转圈再抹掉本身就是闪烁。
//
// 延迟不属于装饰：动效关闭档只压缩淡入的时长，不抹掉这 250 ms，所以这里不用 Viva 的动画时钟
// （它在关闭档直接到终态），而是自己记下出现的时刻。零值可用；由持有指示器的屏幕保存，每帧调用 Opacity。
type Delayed struct {
	active bool
	since  time.Duration
}

// Opacity 返回指示器此刻的不透明度。shown 为假时复位（下次出现重新计时）；
// 还没到终态时向画布请求后续帧，保证延迟结束与淡入过程有帧可画。
func (g *Delayed) Opacity(l *Look, canvas *gfx.Canvas, shown bool) float32 {
	if !shown {
		g.active = false
		return 0
	}
	now := canvas.Now()
	if !g.active {
		g.active, g.since = true, now
	}
	elapsed := now - g.since
	if elapsed < LoadingDelay {
		canvas.RequestFrameIn(LoadingDelay - elapsed)
		return 0
	}
	fade := l.Dur(DurationBase)
	if fade <= 0 {
		return 1
	}
	progress := float32(elapsed-LoadingDelay) / float32(fade)
	if progress >= 1 {
		return 1
	}
	canvas.RequestFrame()
	return gfx.EaseOut.At(progress)
}
