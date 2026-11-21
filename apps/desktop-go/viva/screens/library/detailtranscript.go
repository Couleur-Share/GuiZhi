package library

import (
	"fmt"
	"math"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// 文字稿面板（渲染层 ContentPanel 里的 TranscriptPane 与 transcribeStatusText、formatClock，台账 B-067）。
//
// 没有文字稿：居中一句提示加一枚「生成文字稿」钮（转写中钮上是进度）。有文字稿：一块可滚动的正文（保留段落空行与折行），
// 转写或排版在途时正文上方各有一条进度胶囊。这一页不走阅读岛：正文是原生文字（kit.TextPreWrap）。
//
// 页内查找：渲染层 TranscriptPane 的 highlightQuery 传的是 findQuery（不是 snapshotFindQuery），所以高亮、命中计数与「滚到当前命中」
// 与查找条开着与否无关——关掉查找条之后词还在、高亮还在，直到换条目（渲染层同）。命中计数与当前命中的位置由文字块在每次绘制时回报
// （PreWrapView.Layout），当前命中在查找词、命中序号或文字变了时滚到视口正中（渲染层 scrollElementIntoContainer 同一条居中规则，
// 这里没有平滑滚动）。
//
// 阅读位置：滚动量变了 200 ms 后写进阅读记忆（渲染层 useDebouncedScrollSave 的 200 ms 防抖）；离开这一页（换标签、换条目、卸载）时
// 把还没写的那一笔立刻补上（渲染层 effect 的清理函数 flush）。打开时按记忆恢复（restoreTop，与阅读岛同一份状态）。

const (
	// scrollSaveDebounce 是阅读位置的防抖（渲染层 useDebouncedScrollSave 的 setTimeout 200）。
	scrollSaveDebounce = 200 * time.Millisecond
	// transcriptRestoreFrames 是恢复阅读位置要重复几帧：首帧还没有量到内容高度，有了滚动条之后正文变窄、内容变高，要再收敛一两帧。
	transcriptRestoreFrames = 4
	// stalledMinMs 是「已有多久没有进展」值得提示的下限（渲染层 stalledMs >= 60_000）。
	stalledMinMs = 60_000
)

// transcriptState 是文字稿面板的界面状态（panelState 里的一份，随条目重置）。
type transcriptState struct {
	// vp 是滚动区的外框（窗口坐标，上一帧到这一帧之间由外层盒子记下）：只画看得见的行、把命中滚到正中都按它算。
	vp gfx.Rect
	// top 是最近一次看到的滚动量；dirty 表示它还没写进阅读记忆，due 是防抖到点的时刻，stop 取消当前的唤醒排期。
	top   float32
	dirty bool
	due   time.Time
	stop  func()
	// restoreFrames 是恢复阅读位置已经重复的帧数。
	restoreFrames int
	// revealed 是已经滚到过的「条目 + 查找词 + 命中序号 + 文字长度」：同一份不再滚（渲染层 effect 依赖 activeIndex、contentKey、query）。
	revealed transcriptReveal
}

type transcriptReveal struct {
	itemID, query string
	index, length int
	set           bool
}

// transcribeStageLabel 是转写阶段的名字（渲染层 STAGE_LABELS）；不认得的阶段按转写算。
func transcribeStageLabel(stage string) string {
	switch stage {
	case services.LibraryTranscribeStageFormatting:
		return locales.LibraryStageFormatting()
	case services.LibraryTranscribeStageSummarizing:
		return locales.LibraryStageSummarizing()
	}
	return locales.LibraryStageTranscribing()
}

// transcribeStatusText 是转写进度的一句话（渲染层 transcribeStatusText）：还没收到首个推送是「正在转写（可能需要几分钟）…」，
// 收到之后是「{阶段}…已用 m:ss」，已经停滞 60 秒以上再加「但已有 m:ss 没有进展」。
func transcribeStatusText(progress *services.LibraryTranscribeProgress) string {
	if progress == nil {
		return locales.LibraryTranscribeRunning()
	}
	stage := transcribeStageLabel(progress.Stage)
	elapsed := formatClock(progress.ElapsedMs)
	if progress.StalledMs != nil && *progress.StalledMs >= stalledMinMs {
		return locales.LibraryTranscribeStalled(stage, elapsed, formatClock(*progress.StalledMs))
	}
	return locales.LibraryTranscribeElapsed(stage, elapsed)
}

// formatClock 把毫秒写成 m:ss（渲染层 formatClock：四舍五入到秒，负数当 0，分钟不补零）。
func formatClock(ms int64) string {
	total := max(int64(math.Floor(float64(ms)/1000+0.5)), 0)
	return fmt.Sprintf("%d:%02d", total/60, total%60)
}

// formatProgressText 是排版进度的一句话：有逐块进度是「已完成 n/m 块」，没有是「正在排版…」。
func formatProgressText(progress *services.LibraryTranscriptFormatProgress) string {
	if progress == nil {
		return locales.LibraryTranscriptFormatting()
	}
	return locales.LibraryTranscriptFormatProgress(progress.Current, progress.Total)
}

// transcriptPane 是文字稿标签页的阅读区（渲染层 TranscriptPane）。
func (s *Screen) transcriptPane(c ui.C, v View, item services.LibraryItem, caps ItemCaps) {
	if caps.Transcript == "" {
		s.transcriptEmpty(c, v, caps)
		return
	}
	l := s.Look
	t := l.T
	p := &s.detail.panel
	tx := &p.tx
	media := v.Detail.Media
	// 外层盒子（h-full）：它的矩形就是滚动区的视口，文字块绘制时据此裁行、定位。
	ui.Column(c.Key("transcript")).FillWidth().Grow(1).MinHeight(0).
		Draw(func(d *ui.DrawContext) { tx.vp = d.Rect }).
		Children(func() {
			// h-full overflow-y-auto px-6 py-4
			l.ScrollAreaView(c.Key("scroll"), [4]float32{l.Space(4), l.Space(6), l.Space(4), l.Space(6)}, func(view kit.ScrollView) {
				s.restoreTranscriptScroll(c, view)
				s.noteTranscriptScroll(view.Offset)
				if media.RunningAction != "" || media.Formatting {
					// 两条胶囊都是 inline-flex 的 <p>，中间没有空白：并排落在同一个行盒里，放不下才折行。
					ui.Flow(c.Key("pills")).FillWidth().Items(ui.Center).Children(func() {
						if media.RunningAction != "" {
							s.progressPill(c.Key("running"), transcribeStatusText(media.TranscribeProgress))
						}
						if media.Formatting {
							s.progressPill(c.Key("formatting"), formatProgressText(media.FormatProgress))
						}
					})
				}
				// mx-auto max-w-[48rem] whitespace-pre-wrap text-base leading-8 text-foreground/90
				ui.Column(c.Key("body")).FillWidth().MaxWidth(l.Rem(48)).Self(ui.Center).Children(func() {
					l.TextPreWrap(c.Key("text"), caps.Transcript, p.findQuery, l.Rem(1), l.Rem(2), kit.WeightNormal, t.Foreground.WithAlpha(0.9), kit.PreWrapView{
						Window: func() (float32, float32) { return tx.vp.Y, tx.vp.Y + tx.vp.Height },
						Layout: func(rect gfx.Rect, matches int, matchTop func(i int) float32) {
							s.transcriptLayout(c, view, item.ID, caps.Transcript, l.Rem(2), matches, matchTop)
						},
					})
				})
			}).Grow(1).FillWidth().MinHeight(0)
		})
	s.flushTranscriptScroll(false)
}

// progressPill 是转写与排版的进度胶囊：mb-3 inline-flex items-center gap-1.5 rounded-md bg-accent/50 px-2.5 py-1 text-xs text-muted-foreground，
// 里面一枚 0.75rem 的转圈。
func (s *Screen) progressPill(c ui.C, text string) {
	l := s.Look
	t := l.T
	ui.Row(c).Items(ui.Center).Gap(l.Space(1.5)).PadSides(l.Space(1), l.Space(2.5), l.Space(1), l.Space(2.5)).MarginBottom(l.Space(3)).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: t.Accent.WithAlpha(0.5), Radius: l.Rem(0.75) - 2})
		}).
		Children(func() {
			l.Spinner(c.Key("spinner"), l.Rem(0.75), t.MutedForeground)
			l.Text(c.Key("text"), text, kit.TextXS, kit.WeightNormal, t.MutedForeground)
		})
}

// transcriptEmpty 是还没有文字稿的空态：flex h-full flex-col items-center justify-center gap-3 px-6 text-center。
func (s *Screen) transcriptEmpty(c ui.C, v View, caps ItemCaps) {
	l := s.Look
	t := l.T
	media := v.Detail.Media
	running := media.RunningAction != ""
	ui.Column(c.Key("transcript-empty")).FillWidth().Grow(1).MinHeight(0).Items(ui.Center).Justify(ui.Center).
		Gap(l.Space(3)).PadSides(0, l.Space(6), 0, l.Space(6)).
		Children(func() {
			l.Text(c.Key("hint"), locales.LibraryTranscriptEmpty(), kit.TextSM, kit.WeightNormal, t.MutedForeground, kit.Centered(), kit.Wrapped())
			label := locales.LibraryTranscribeGenerate()
			switch {
			case running:
				label = transcribeStatusText(media.TranscribeProgress)
			case caps.IsOnlineVideo:
				label = locales.LibraryTranscribeRegenerateOnline()
			}
			spec := kit.ButtonSpec{Label: label, Disabled: running, Busy: running, BusySize: l.Rem(0.875)}
			if !running {
				spec.Icon, spec.IconSize = icons.AudioLines, l.Rem(0.875)
			}
			// inline-flex h-8 rounded-lg bg-primary px-3 text-sm font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-60
			box := l.Button(c.Key("generate"), spec, kit.ButtonStyle{
				Height: l.Rem(2), PadX: l.Space(3), Radius: l.Rem(0.75), Gap: l.Space(1.5), Text: kit.TextSM, Weight: kit.WeightMedium,
				Fg: t.PrimaryForeground, Bg: t.Primary, BgHover: t.Primary.WithAlpha(0.9),
				Transition: kit.DurationDefault, DisabledOpacity: 0.6, Surface: t.UiPanelBg,
			})
			if box.Clicked() {
				s.Store.Transcribe(s.Ctx, false)
			}
		})
}

// transcriptLayout 是文字块每次绘制时的回报：命中数交给查找条（p.findCount，变了就再出一帧让条上的数字跟上），
// 当前命中在需要时滚到视口正中。
func (s *Screen) transcriptLayout(c ui.C, view kit.ScrollView, itemID, text string, line float32, matches int, matchTop func(i int) float32) {
	p := &s.detail.panel
	tx := &p.tx
	if matches != p.findCount {
		p.findCount = matches
		c.Canvas().RequestFrame()
	}
	key := transcriptReveal{itemID: itemID, query: jsTrim(p.findQuery), index: p.findActive, length: len(text), set: true}
	if tx.revealed == key {
		return
	}
	if matches > 0 && tx.vp.Height <= 0 {
		c.Canvas().RequestFrame() // 滚动区还没量到：下一帧再滚，这一份不算滚过
		return
	}
	tx.revealed = key
	if matches == 0 {
		return
	}
	i := ((p.findActive % matches) + matches) % matches
	center := matchTop(i) + line/2
	delta := center - (tx.vp.Y + tx.vp.Height/2)
	if math.Abs(float64(delta)) < 0.5 {
		return
	}
	target := view.State.Offset + delta
	view.State.ScrollTo(target)
	tx.top = max(target, 0) // 这是程序滚的，也算阅读位置，随后照常防抖写入
	s.markTranscriptDirty()
	c.Canvas().RequestFrame()
}

// restoreTranscriptScroll 按阅读记忆恢复位置（渲染层「恢复滚动位置」的 requestAnimationFrame）：等滚动区量过一次、有了视口与内容高度之后开始，
// 重复几帧到位置收敛，然后放下待恢复的位置（只在刚打开时恢复，换标签即作废，见 changeTab）。
func (s *Screen) restoreTranscriptScroll(c ui.C, view kit.ScrollView) {
	p := &s.detail.panel
	tx := &p.tx
	if p.restoreTop == nil || p.restoreTab != TabTranscript {
		return
	}
	if view.Viewport <= 0 || view.Content <= 0 {
		c.Canvas().RequestFrame() // 首帧还没量到
		return
	}
	top := float32(*p.restoreTop)
	view.State.ScrollTo(top)
	tx.top = top
	tx.restoreFrames++
	if tx.restoreFrames >= transcriptRestoreFrames {
		p.restoreTop = nil
		return
	}
	c.Canvas().RequestFrame()
}

// noteTranscriptScroll 记下滚动量；变了就开始（重新计时）一次防抖。
func (s *Screen) noteTranscriptScroll(offset float32) {
	tx := &s.detail.panel.tx
	if offset == tx.top {
		return
	}
	tx.top = offset
	s.markTranscriptDirty()
}

// markTranscriptDirty 标记阅读位置待写，并排一次唤醒（防抖到点时界面要再出一帧，才会走到 flushTranscriptScroll）。
func (s *Screen) markTranscriptDirty() {
	tx := &s.detail.panel.tx
	tx.dirty = true
	tx.due = s.nowTime().Add(scrollSaveDebounce)
	if tx.stop != nil {
		tx.stop()
	}
	tx.stop = s.Store.After(scrollSaveDebounce, s.Store.wake)
}

// flushTranscriptScroll 把待写的阅读位置写进记忆：force 为假时要等防抖到点。离开这一页的几处（换条目、换标签、卸载）传 force。
func (s *Screen) flushTranscriptScroll(force bool) {
	p := &s.detail.panel
	tx := &p.tx
	if !tx.dirty || (!force && s.nowTime().Before(tx.due)) {
		return
	}
	tx.dirty = false
	if tx.stop != nil {
		tx.stop()
		tx.stop = nil
	}
	if p.itemID == "" {
		return
	}
	s.patchReading(p.itemID, ReadingPatch{Tab: TabTranscript, ScrollTopByTab: map[ReadingTab]float64{TabTranscript: float64(tx.top)}})
}

// leaveTranscript 是内容面板这一帧不在文字稿标签上：补写还没写的位置，并让下次进来重新开始（新的滚动区从 0 起）。
func (s *Screen) leaveTranscript() {
	tx := &s.detail.panel.tx
	if tx.dirty {
		s.flushTranscriptScroll(true)
	}
	if tx.stop != nil {
		tx.stop()
	}
	*tx = transcriptState{vp: tx.vp}
}
