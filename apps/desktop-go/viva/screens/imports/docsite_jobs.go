package imports

import (
	"strconv"
	"strings"

	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// 文档站页的下半部分：采集批次列表，与选中批次的页面明细（渲染层 DocumentSitePanel 的 jobs.map）。

// docPagesMaxHeight 是页面明细列表的最大高度（max-h-80 = 20rem），超出在列表里滚动。
const docPagesMaxHeight = 20

// docJobs 是批次列表（space-y-3）。第一次读取成功且没有批次时写一句空态说明；还没读到或读取失败时不写——
// 那两种情形里「没有批次」都不是事实（读取失败由页面顶部的错误态说明）。
func (s *Screen) docJobs(c ui.C, v DocSiteView) {
	l := s.Look
	ui.Column(c.Key("jobs")).FillWidth().Gap(l.Space(3)).Children(func() {
		if v.HasLoaded && v.LoadError == "" && len(v.Jobs) == 0 {
			l.Text(c.Key("empty"), locales.NativeDocSiteEmpty(), kit.TextSM, kit.WeightNormal, l.T.MutedForeground, kit.Wrapped())
			return
		}
		for _, job := range v.Jobs {
			s.docJob(c.Key("job-"+job.ID), v, job)
		}
	})
}

// docJob 是一个批次的卡片：rounded-lg border border-border p-3 space-y-2。
// 标题（入口网址 · 状态，点击展开页面明细）、各状态的页数、批次错误、控制按钮；选中的批次下面是页面明细。
func (s *Screen) docJob(c ui.C, v DocSiteView, job services.CrawlJob) {
	l := s.Look
	t := l.T
	st := s.DocSite
	ui.Column(c).FillWidth().Gap(l.Space(2)).Pad(l.Space(3) + 1).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Border: t.Border, BorderW: 1, Radius: l.Rem(0.75)})
		}).
		Children(func() {
			title := JobTitleURL(job) + " \u00b7 " + JobStatusLabel(job.Status)
			if s.docClickable(c.Key("title"), title, kit.TextSM, "") {
				id := job.ID
				st.Select(s.baseCtx(), id)
			}
			if counts := docCountsText(job); counts != "" {
				l.Text(c.Key("counts"), counts, kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Wrapped())
			}
			if job.Error != "" {
				l.Text(c.Key("error"), job.Error, kit.TextSM, kit.WeightNormal, t.Destructive, kit.Wrapped())
			}
			s.docActions(c, v, job)
			if v.Selected == job.ID {
				s.docPages(c, v)
			}
		})
}

// docCountsText 是批次各状态的页数：「新增 3 · 失败 1」，顺序固定（OrderedCounts）。
func docCountsText(job services.CrawlJob) string {
	entries := OrderedCounts(job)
	parts := make([]string, 0, len(entries))
	for _, e := range entries {
		parts = append(parts, e.Label+" "+strconv.Itoa(e.Count))
	}
	return strings.Join(parts, " \u00b7 ")
}

// docActions 是批次上的控制按钮（flex flex-wrap gap-3 text-sm 的纯文字按钮）：运行中显示「暂停」，否则显示「继续」
// （已取消的没有）；「重试失败项」总在，没有失败页或有操作在进行时禁用；已结束与已取消的没有「取消」。
// 操作会阻塞到处理器返回，放到后台做；失败由 Store 弹带原因的提示。
func (s *Screen) docActions(c ui.C, v DocSiteView, job services.CrawlJob) {
	l := s.Look
	t := l.T
	st := s.DocSite
	id := job.ID
	act := ActionsFor(job)
	style := kit.ButtonStyle{Radius: l.Rem(0.25), Text: kit.TextSM, Weight: kit.WeightNormal, Fg: t.Foreground}
	ui.Flow(c.Key("actions")).FillWidth().Gap(l.Space(3)).LineGap(l.Space(3)).Children(func() {
		switch {
		case act.ShowPause:
			if l.Button(c.Key("pause"), kit.ButtonSpec{Label: locales.WebCapturePause()}, style).Clicked() {
				st.Spawn(func() { st.Pause(s.baseCtx(), id) })
			}
		case act.ShowResume:
			if l.Button(c.Key("resume"), kit.ButtonSpec{Label: locales.WebCaptureResume()}, style).Clicked() {
				st.Spawn(func() { st.Resume(s.baseCtx(), id) })
			}
		}
		if l.Button(c.Key("retry"), kit.ButtonSpec{Label: locales.WebCaptureRetryFailed(), Disabled: !act.CanRetry || v.Busy}, style).Clicked() {
			st.Spawn(func() { st.Retry(s.baseCtx(), id) })
		}
		if act.ShowCancel {
			if l.Button(c.Key("cancel"), kit.ButtonSpec{Label: locales.WebCaptureCancel()}, style).Clicked() {
				st.Spawn(func() { st.Cancel(s.baseCtx(), id) })
			}
		}
	})
}

// docPages 是选中批次的页面明细（max-h-80 overflow-auto space-y-2）：每页一行「状态 · 网址」，失败页下面写原因。
// 读回来之前什么也不画；读回来是空的写一句说明。
func (s *Screen) docPages(c ui.C, v DocSiteView) {
	l := s.Look
	t := l.T
	if !v.PagesLoaded {
		return
	}
	if len(v.Pages) == 0 {
		l.Text(c.Key("no-pages"), locales.NativeDocSiteNoPages(), kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Wrapped())
		return
	}
	l.ScrollArea(c.Key("pages"), [4]float32{}, func() {
		ui.Column(c.Key("pages-list")).FillWidth().Gap(l.Space(2)).Children(func() {
			for _, p := range v.Pages {
				s.docPage(c.Key("page-"+p.ID), p)
			}
		})
	}).FillWidth().MaxHeight(l.Rem(docPagesMaxHeight))
}

// docPage 是一个页面：text-xs break-all。已入库的页面（有关联条目）整行可点，打开知识库里的条目。
func (s *Screen) docPage(c ui.C, p services.CrawlPage) {
	l := s.Look
	t := l.T
	text := PageStatusLabel(p.Status) + " \u00b7 " + p.URL
	ui.Column(c).FillWidth().Children(func() {
		if p.ItemID != "" && s.Hooks.OpenItem != nil {
			if s.docClickable(c.Key("link"), text, kit.TextXS, locales.ImportsOpenItem()) {
				s.Hooks.OpenItem(p.ItemID)
			}
		} else {
			l.TextBreakAll(c.Key("text"), text, kit.TextXS, kit.WeightNormal, t.Foreground)
		}
		if p.Error != "" {
			l.TextBreakAll(c.Key("error"), p.Error, kit.TextXS, kit.WeightNormal, t.Destructive)
		}
	})
}
