package imports

import (
	"context"
	"strconv"
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// 「采集文档站」整页（渲染层 components/imports/DocumentSitePanel.tsx）：返回、标题、创建采集批次的表单、实际范围预览、
// 开始按钮，下面是批次列表与选中批次的页面明细（docsite_jobs.go）。状态都在 DocSiteStore 里，这里只放界面自己的东西：
// 输入框与下拉的编辑状态、本页是否挂着（轮询）。所有方法都在界面线程里调用。

// docSiteUI 是文档站整页的界面状态。
type docSiteUI struct {
	url, dir, pages, depth ui.Field
	collection, policy     kit.SelectState

	inited   bool // 输入框已用 Store 里的表单值起过一次文字
	attached bool // 本页挂着轮询（Attach 过且还没 Detach）
	drawn    bool // 这一帧画过本页：EndFrame 据此发现「这一帧没画」（模块或页面被切走）
}

// docSitePadding 是整页的内边距（p-6）。
func (s *Screen) docSitePadding() [4]float32 {
	p := s.Look.Space(6)
	return [4]float32{p, p, p, p}
}

// baseCtx 是后台请求用的上下文；没有装配（测试里没给 Ctx）时取 Background。
func (s *Screen) baseCtx() context.Context {
	if s.Ctx != nil {
		return s.Ctx
	}
	return context.Background()
}

// docSitePage 声明整页：h-full overflow-auto p-6，里面 space-y-5。每一帧都会 Attach（幂等）：
// 挂着才轮询；离开（返回钮、或这一帧没画，见 EndFrame）时 Detach，停掉轮询并丢弃在途的读取。
func (s *Screen) docSitePage(c ui.C) {
	l := s.Look
	st := s.DocSite
	u := &s.docUI
	u.drawn, u.attached = true, true
	st.Attach(s.baseCtx())
	v := st.View()
	if !u.inited {
		u.inited = true
		u.url.SetText(v.Form.URL)
		u.dir.SetText(v.Form.Directory)
		u.pages.SetText(strconv.Itoa(v.Form.MaxPages))
		u.depth.SetText(strconv.Itoa(v.Form.MaxDepth))
	}
	ui.Column(c.Key("documents")).FillWidth().Grow(1).MinHeight(0).Clip().Children(func() {
		l.ScrollArea(c.Key("scroll"), s.docSitePadding(), func() {
			ui.Column(c.Key("page")).FillWidth().Gap(l.Space(5)).Children(func() {
				s.docBack(c)
				s.docHeader(c)
				if v.LoadError != "" {
					// 读取失败不能画成「没有批次」：整块错误态带原因与重试（渲染层 loadError && LoadErrorState）。
					if l.LoadErrorState(c.Key("load-error"), v.LoadError) {
						st.Spawn(func() { st.Load(s.baseCtx()) })
					}
				}
				s.docForm(c, v)
				s.docScope(c, v)
				s.docStart(c, v)
				s.docJobs(c, v)
			})
		}).FillWidth().Grow(1).MinHeight(0)
	})
}

// EndFrame 在窗口每一帧画完之后调用：这一帧没有画文档站页（切到了别的模块、设置页或回到了队列）而它还挂着，就摘掉。
// 返回钮已经主动摘过；这里兜住所有别的离开方式。没有装配 DocSite 时什么也不做。
func (s *Screen) EndFrame() {
	u := &s.docUI
	drawn := u.drawn
	u.drawn = false
	if s.DocSite != nil && u.attached && !drawn {
		u.attached = false
		s.DocSite.Detach()
	}
}

// Close 在窗口关闭时停掉文档站页的轮询与在途的读取。
func (s *Screen) Close() {
	s.docUI.attached = false
	if s.DocSite != nil {
		s.DocSite.Detach()
	}
}

// leaveDocuments 回到任务队列并摘掉文档站页（渲染层 onBack：组件卸载，轮询随之清掉）。
func (s *Screen) leaveDocuments() {
	s.documents = false
	s.docUI.attached = false
	s.DocSite.Detach()
}

// docBack 是左上角的返回钮：text-sm text-muted-foreground 的纯文字按钮。
func (s *Screen) docBack(c ui.C) {
	l := s.Look
	t := l.T
	ui.Row(c.Key("back-row")).FillWidth().Children(func() {
		b := l.Button(c.Key("back"), kit.ButtonSpec{Label: locales.WebCaptureBack()}, kit.ButtonStyle{
			Radius: l.Rem(0.25), Text: kit.TextSM, Weight: kit.WeightNormal, Fg: t.MutedForeground,
		})
		if b.Clicked() {
			s.leaveDocuments()
		}
	})
}

// docHeader 是标题（text-lg font-semibold）与说明（text-sm text-muted-foreground）。
func (s *Screen) docHeader(c ui.C) {
	l := s.Look
	t := l.T
	ui.Column(c.Key("header")).FillWidth().Children(func() {
		l.Heading(c.Key("title"), 2, locales.WebCaptureImportSite(), kit.TextLG, kit.WeightSemibold, t.Foreground)
		l.Text(c.Key("desc"), locales.WebCaptureSiteDescription(), kit.TextSM, kit.WeightNormal, t.MutedForeground, kit.Wrapped())
	})
}

// docForm 是表单：grid gap-3 sm:grid-cols-2（窗口最窄也在 sm 断点之上，所以恒为两列）。
// 入口网址、目录范围、最多页数、发现深度、目标合集、重复处理，依次两个一行。
func (s *Screen) docForm(c ui.C, v DocSiteView) {
	l := s.Look
	st := s.DocSite
	u := &s.docUI
	ui.Column(c.Key("form")).FillWidth().Gap(l.Space(3)).Children(func() {
		ui.Row(c.Key("row-url")).FillWidth().Gap(l.Space(3)).Children(func() {
			s.docField(c, "url", locales.WebCaptureEntryUrl(), "https://docs.example.com/guide/", &u.url, st.SetURL)
			s.docField(c, "dir", locales.WebCaptureDirectoryScope(), locales.WebCaptureDefaultDirectory(), &u.dir, st.SetDirectory)
		})
		ui.Row(c.Key("row-limits")).FillWidth().Gap(l.Space(3)).Children(func() {
			s.docField(c, "pages", locales.WebCapturePageLimit(), "", &u.pages, func(text string) { st.SetMaxPages(parseCount(text)) })
			s.docField(c, "depth", locales.WebCaptureDepthLimit(), "", &u.depth, func(text string) { st.SetMaxDepth(parseCount(text)) })
		})
		ui.Row(c.Key("row-select")).FillWidth().Gap(l.Space(3)).Children(func() {
			s.docCollectionSelect(c, v)
			s.docPolicySelect(c, v)
		})
	})
}

// docField 是一个带标签的文本框（<label className="text-sm">文字<Input/></label>）：标签行紧贴着输入框，没有间距。
// 文字编辑后经 onChange 交给 Store；文字留在 Field 里，Store 里的表单值只由这里写入，两边不会各说各话。
func (s *Screen) docField(c ui.C, key, label, placeholder string, f *ui.Field, onChange func(string)) {
	l := s.Look
	t := l.T
	cc := c.Key(key)
	ui.Column(cc).Grow(1).Basis(0).MinWidth(0).Children(func() {
		l.Text(cc.Key("label"), label, kit.TextSM, kit.WeightNormal, t.Foreground)
		box := l.FieldInput(cc.Key("input"), f, kit.FieldSpec{Name: label, Placeholder: placeholder, PlaceholderInk: t.MutedForeground})
		if box.Changed() {
			onChange(f.Text())
		}
	})
}

// parseCount 是渲染层 Number(text) 的对应物：整数照读，读不出来（空串、字母、小数）给 0——
// 主进程的 crawl:create 会拒绝越界的数值并把原因带回，用户在提示里看得到，不会悄悄换成别的值去采集。
func parseCount(text string) int {
	n, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil {
		return 0
	}
	return n
}

// docCollectionSelect 是目标合集下拉：第一项「未归合集」，其后是全部合集。
func (s *Screen) docCollectionSelect(c ui.C, v DocSiteView) {
	options := []kit.SelectOption{{Value: "", Label: locales.WebCaptureUnfiled()}}
	for _, col := range v.Collections {
		options = append(options, kit.SelectOption{Value: col.ID, Label: col.Name})
	}
	s.docSelect(c, "collection", &s.docUI.collection, kit.SelectSpec{
		Label: locales.WebCaptureCollection(), Value: v.Form.CollectionID, Options: options, Width: -1,
	}, s.DocSite.SetCollection)
}

// docPolicySelect 是重复处理下拉：跳过重复条目 / 检查原文更新，保护手动编辑。
func (s *Screen) docPolicySelect(c ui.C, v DocSiteView) {
	options := []kit.SelectOption{
		{Value: services.CrawlDuplicateSkip, Label: locales.WebCaptureSkipDuplicates()},
		{Value: services.CrawlDuplicateUpdate, Label: locales.WebCaptureUpdatePolicy()},
	}
	s.docSelect(c, "policy", &s.docUI.policy, kit.SelectSpec{
		Label: locales.WebCaptureDuplicatePolicy(), Value: v.Form.Policy, Options: options, Width: -1,
	}, s.DocSite.SetPolicy)
}

// docSelect 把一个下拉放进表单的一格（grid 子项，撑满格宽）：选中后经 apply 交给 Store。
func (s *Screen) docSelect(c ui.C, key string, st *kit.SelectState, spec kit.SelectSpec, apply func(string)) {
	cc := c.Key(key)
	ui.Column(cc).Grow(1).Basis(0).MinWidth(0).Children(func() {
		if picked, changed := s.Look.Select(cc.Key("select"), st, spec); changed {
			apply(picked)
		}
	})
}

// docScope 是「实际范围」预览（break-all text-sm text-muted-foreground）：范围无效时写「请输入有效网址和目录」。
func (s *Screen) docScope(c ui.C, v DocSiteView) {
	scope := v.Scope
	if scope == "" {
		scope = locales.WebCaptureValidScope()
	}
	text := locales.WebCaptureActualScope() + scope + locales.WebCaptureScopeHint()
	s.Look.TextBreakAll(c.Key("scope"), text, kit.TextSM, kit.WeightNormal, s.Look.T.MutedForeground)
}

// docStart 是「开始采集」按钮：rounded-lg bg-primary px-4 py-2 text-sm text-primary-foreground disabled:opacity-50；
// 范围无效或有操作在进行（Busy）时禁用。创建会阻塞到处理器返回，放到后台做。
func (s *Screen) docStart(c ui.C, v DocSiteView) {
	l := s.Look
	t := l.T
	st := s.DocSite
	ui.Row(c.Key("start-row")).FillWidth().Children(func() {
		b := l.Button(c.Key("start"), kit.ButtonSpec{Label: locales.WebCaptureStart(), Disabled: !v.CanStart()}, kit.ButtonStyle{
			PadX: l.Space(4), PadY: l.Space(2), Radius: l.Rem(0.75), Text: kit.TextSM, Weight: kit.WeightNormal,
			Fg: t.PrimaryForeground, Bg: t.Primary, Transition: kit.DurationNone,
		})
		if b.Clicked() {
			st.Spawn(func() { st.Start(s.baseCtx()) })
		}
	})
}

// docClickable 是一段可点的折行文字（break-all）：悬停变主色，键盘可达。返回这一帧是否被点。
// hint 是悬停提示（也是可访问描述），补充文字之外的信息；空串不带提示。
func (s *Screen) docClickable(c ui.C, text string, st kit.TextStyle, hint string) bool {
	l := s.Look
	t := l.T
	id := ui.ID(c)
	box := ui.Column(c).FillWidth().HitWith(id, text, gfx.HitOptions{
		Role: gfx.RoleButton, Description: l.HintDescription(id, hint), Hint: hint, Cursor: gfx.PointerCursorHand,
	})
	l.TabStop(box)
	p := l.HoverProgress(c, id, 0, box.State().Hovered, kit.DurationDefault)
	ink := kit.MixToken(t.Foreground, t.Primary, p)
	box.Children(func() { l.TextBreakAll(c.Key("text"), text, st, kit.WeightNormal, ink) })
	return box.Clicked()
}
