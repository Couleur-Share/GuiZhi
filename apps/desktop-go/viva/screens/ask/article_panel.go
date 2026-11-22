package ask

import (
	"fmt"
	"sync/atomic"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/aiflow"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// 围绕本文的对话面板（渲染层 ArticleConversation）：知识库详情的右侧栏与全局问答里的本文对话是同一块界面，
// 读写同一个 ArticleStore。ArticleSpec 是调用方（阅读器或全局问答）给的目标与回调。

// ArticleSpec 是一次声明面板所需的东西。除 ResolveTarget 外都在界面线程里使用。
type ArticleSpec struct {
	// Target 是此刻的提问目标（条目、阅读视图、版本）；SessionID 为空取该文章最近一次会话（阅读器侧栏），
	// 非空打开指定会话（全局问答里的本文会话）。
	Target    aiflow.ArticleTarget
	SessionID string
	// Prompt 与 PromptKey：选段提问带来的提示语。PromptKey 每变一次，若 Target 带选段就把选段挂成引用、提示语写进草稿。
	Prompt    string
	PromptKey int
	// ResolveTarget 在发送前取阅读器此刻的目标（选区、版本可能已变）；返回错误按「提问失败」处理。它在后台线程里调用：
	// 实现必须只读调用方在声明这一帧时捕获的值，不能碰界面状态。为空时就用 Target。
	ResolveTarget func() (aiflow.ArticleTarget, error)
	// OnClose 非空时头部有「收起」钮，Esc 也能收起：停止回答并保存，保存成功才调用（界面线程，下一帧）。
	OnClose func()
	// OnSessionChange 在本文问答换了会话（新对话、从历史打开）之后调用，参数是新会话 ID。可能在后台线程里调用。
	OnSessionChange func(id string)
	// OnLocate 把一条文章来源定位到阅读区里的原文：结果经 done 回报（found 为 false 就是没找到，卡片亮出行内提示）。
	// 界面线程调用，done 可在任意 goroutine 调用；为空时定位按失败处理。
	OnLocate func(source aiflow.ArticleSource, done func(found bool))
	// OnSubmitted 在问题已被接收、草稿已清空时调用（可能在后台线程里调用；如通知阅读器清掉已发出的选段）。
	OnSubmitted func()
	// AutoFocus 让面板第一次出现的那一帧把焦点放到输入框（阅读器侧栏打开时）。
	AutoFocus bool
	// StopOnUnmount 让面板不再被声明的那一帧停掉进行中的回答（全局问答里的本文对话：组件卸载时 stop）。
	StopOnUnmount bool
	// ScopeKey 区分同时可能出现的两处面板的焦点作用域；缺省为空。
	ScopeKey string
}

// articleUI 是面板的界面状态。它随面板挂载而生、卸载而清空；卸载按「一帧没声明」判断（endArticleFrame）。
type articleUI struct {
	mounted, shown bool
	stopOnUnmount  bool

	in      chatInput
	history bool
	// openKey 是最近一次发起 Open 的目标记号：目标或会话变了才再发起（渲染层 useEffect 的依赖）。
	openKey string
	// consumedKey 是已经处理过的选段提问序号（渲染层 consumedPrompt ref）。
	consumed    int
	consumedSet bool
	toEnd       bool   // 下一帧把消息列表滚到底（发出问题、换会话、读取完成）
	viewKey     string // 上一帧显示的会话：换了就滚到底
	cards       map[string]*articleCardState
	hist        articleHistoryUI

	rect     gfx.Rect // 面板上一次画出的外框：Esc 只在焦点落进它时收起面板
	hasClose bool
	// onLocate 是这一帧调用方给的定位入口（来源卡片点「定位到原文」时用）；sessionKey 是 StopOnUnmount 的面板上一帧显示的会话。
	onLocate   func(source aiflow.ArticleSource, done func(found bool))
	sessionKey string

	// closed 与 closeHistory 由后台保存、打开历史会话的结果回到界面线程。
	closed       atomic.Bool
	closeHistory atomic.Bool
}

// ArticleEscape 处理 Esc：持有键盘焦点的控件在面板的外框里、面板有收起钮时开始收起（停止回答并保存），并告诉调用方
// 按键被拦下了（渲染层面板上 stopPropagation）。控件的位置取它最近一次声明的命中区。
func (s *Screen) ArticleEscape(canvas *gfx.Canvas) bool {
	a := &s.art
	if !a.mounted || !a.hasClose || canvas == nil || a.rect.Width <= 0 {
		return false
	}
	id := canvas.Focused()
	if id.IsZero() {
		return false
	}
	b, ok := canvas.HitBounds(id)
	if !ok {
		return false
	}
	x, y := b.X+b.Width/2, b.Y+b.Height/2
	if x < a.rect.X || x > a.rect.X+a.rect.Width || y < a.rect.Y || y > a.rect.Y+a.rect.Height {
		return false
	}
	s.closeArticle()
	return true
}

// ReleaseArticle 在阅读器卸载（换了文章、详情关闭）时调用：停止进行中的回答（渲染层 ArticleAskReader 的 effect 清理）。
func (s *Screen) ReleaseArticle() {
	s.Store.Article().Stop()
}

// endArticleFrame 在每帧结束时判断面板和历史列表有没有被声明：没有就是卸载，状态清空，下次出现重新挂载。
func (s *Screen) endArticleFrame() {
	a := &s.art
	if !a.shown && a.mounted {
		a.mounted = false
		if a.stopOnUnmount {
			s.Store.Article().Stop()
		}
		a.in, a.history, a.openKey, a.consumedSet, a.cards = chatInput{}, false, "", false, nil
		a.viewKey, a.toEnd, a.hasClose, a.onLocate, a.sessionKey = "", false, false, nil, ""
	}
	a.shown = false
	h := &a.hist
	if !h.shown && h.mounted {
		h.mounted, h.itemID, h.deleting = false, "", nil
		h.rename.session = nil
		h.confirm = kit.ConfirmState{}
	}
	h.shown = false
}

// mountArticle 在面板第一次出现时初始化界面状态。
func (s *Screen) mountArticle(spec ArticleSpec) {
	a := &s.art
	a.shown = true
	a.stopOnUnmount = spec.StopOnUnmount
	a.hasClose = spec.OnClose != nil
	a.onLocate = spec.OnLocate
	if spec.StopOnUnmount {
		// 渲染层 useEffect(() => () => stop(), [session.id])：换了会话也停掉在途的回答。
		if a.sessionKey != "" && a.sessionKey != spec.SessionID {
			s.Store.Article().Stop()
		}
		a.sessionKey = spec.SessionID
	}
	if a.mounted {
		return
	}
	a.mounted = true
	a.in.focusNext = spec.AutoFocus
	a.toEnd = true
}

// openArticle 在目标或会话变了时发起 Open（后台）：渲染层 useEffect(() => open(target, sessionId), [target, sessionId])。
func (s *Screen) openArticle(spec ArticleSpec) {
	a := &s.art
	version := ""
	if spec.Target.VersionID != nil {
		version = *spec.Target.VersionID
	}
	key := fmt.Sprintf("%s|%s|%s|%s", spec.Target.ItemID, spec.SessionID, spec.Target.View, version)
	if a.openKey == key {
		return
	}
	a.openKey = key
	s.spawn(func() { s.Store.Article().Open(spec.Target, spec.SessionID) })
}

// takePrompt 处理选段提问：读取完成后，PromptKey 每变一次，目标带选段就把提示语写进草稿、把选段挂成引用。
func (s *Screen) takePrompt(spec ArticleSpec, v ArticleView, key string) {
	a := &s.art
	if v.Loading || v.ItemID != spec.Target.ItemID || a.consumedSet && a.consumed == spec.PromptKey {
		return
	}
	a.consumedSet, a.consumed = true, spec.PromptKey
	if spec.Target.Selection != nil {
		s.Store.Article().AcceptSelection(key, spec.Target, spec.Prompt)
	}
}

// ArticlePanel 声明围绕本文的对话面板：section flex h-full min-h-0 flex-col bg-background，是带名字的区域。
// 从上到下：头部、（展开时的）本文历史、保存失败提示、消息区、底部（选段引用、联网开关、联网缺失提示、输入区）。
func (s *Screen) ArticlePanel(c ui.C, spec ArticleSpec) {
	l := s.Look
	t := l.T
	a := &s.art
	art := s.Store.Article()
	s.mountArticle(spec)
	if a.closed.Swap(false) && spec.OnClose != nil {
		spec.OnClose()
	}
	if a.closeHistory.Swap(false) {
		a.history = false
	}
	s.openArticle(spec)
	v := art.View()
	key := DraftKey(spec.Target.ItemID, spec.SessionID)
	s.takePrompt(spec, v, key)
	art.SyncSource(key, spec.Target)
	if viewKey := v.SessionID + "|" + fmt.Sprint(v.Loading); viewKey != a.viewKey {
		a.viewKey = viewKey
		a.toEnd = true
	}
	s.pruneArticleCards(v.Messages)

	// 面板不声明自己的焦点作用域：它可能画在表格视图的详情浮层里，浮层的焦点陷阱只放行自己作用域里的命中区，
	// 嵌套的作用域会让整块面板点不动。「焦点在不在面板里」改看焦点控件的位置（ArticleEscape）。
	c = c.Key("article-ask" + spec.ScopeKey)
	ui.Column(c).FillWidth().FillHeight().MinHeight(0).
		Draw(func(d *ui.DrawContext) {
			a.rect = d.Rect
			l.PaintSurface(d, kit.Surface{Bg: t.Background})
			d.Canvas.ContainerWithOptions(d.Rect, gfx.RoleGroup, locales.ArticleAskTitle(), gfx.HitOptions{})
		}).
		Children(func() {
			s.articleHeader(c, spec, v, key)
			if a.history {
				s.articleHistory(c, spec.Target.ItemID, func(id string) { s.openArticleSession(spec, key, id) })
			}
			if v.SaveError != "" {
				s.articleSaveError(c, v.SaveError)
			}
			s.articleBody(c, spec, v)
			s.articleFooter(c, spec, v, key)
		})
}

// articleHeader 是头部：flex shrink-0 items-center gap-2 border-b border-border p-3。左边标题与文章标题（单行截断），
// 右边「新对话」「历史」两枚 text-xs 的文字钮，以及有 OnClose 时的 ×。
func (s *Screen) articleHeader(c ui.C, spec ArticleSpec, v ArticleView, key string) {
	l := s.Look
	t := l.T
	a := &s.art
	ui.Row(c.Key("head")).FillWidth().Shrink(0).Items(ui.Center).Gap(l.Space(2)).
		PadSides(l.Space(3), l.Space(3), l.Space(3)+1, l.Space(3)).
		Draw(func(d *ui.DrawContext) { l.PaintEdge(d, kit.EdgeBottom, 1, t.Border) }).
		Children(func() {
			ui.Column(c.Key("titles")).Grow(1).Shrink(1).Basis(0).MinWidth(0).Children(func() {
				// 窄面板里英文标题会折成两行（渲染层 h2 不截断），所以允许折行。
				l.Heading(c.Key("title"), 2, locales.ArticleAskTitle(), kit.TextSM, kit.WeightSemibold, t.Foreground, kit.Wrapped())
				l.Text(c.Key("item"), v.Title, kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Ellipsis())
			})
			base := textBtn{Style: kit.TextXS, Weight: kit.WeightNormal, Fg: t.Foreground}
			newBtn := base
			newBtn.Disabled = v.Loading
			if s.textButton(c.Key("new"), locales.ArticleAskNew(), newBtn) {
				s.newArticleConversation(spec, key)
			}
			open := a.history
			hist := base
			hist.Expanded = &open
			if s.textButton(c.Key("history"), locales.ArticleAskHistory(), hist) {
				a.history = !a.history
			}
			if spec.OnClose != nil {
				name := locales.ArticleAskClose()
				b := l.Button(c.Key("close"), kit.ButtonSpec{Label: "\u00d7", Name: name}, kit.ButtonStyle{
					PadX: l.Space(2), PadY: l.Space(1), Radius: l.Rem(0.25), Text: kit.TextBase, Weight: kit.WeightNormal,
					Fg: t.Foreground,
				}).Shrink(0)
				if b.Clicked() {
					s.closeArticle()
				}
			}
		})
}

// newArticleConversation 是「新对话」：清掉草稿与选段，保存当前对话后开一个新的，成功再通知调用方换了会话。
func (s *Screen) newArticleConversation(spec ArticleSpec, key string) {
	s.clearDraftInput(&s.art.in, key)
	target := spec.Target
	s.spawn(func() {
		if id := s.Store.Article().NewConversation(key, target); id != "" && spec.OnSessionChange != nil {
			spec.OnSessionChange(id)
		}
	})
}

// openArticleSession 从本文历史里打开一个会话：成功后清空草稿与选段、通知换了会话、收起历史。
func (s *Screen) openArticleSession(spec ArticleSpec, key, id string) {
	a := &s.art
	target := spec.Target
	s.spawn(func() {
		art := s.Store.Article()
		if !art.OpenFromHistory(target, id) {
			return
		}
		art.SetDraft(key, "")
		art.SetQuote(key, nil)
		if spec.OnSessionChange != nil {
			spec.OnSessionChange(id)
		}
		a.closeHistory.Store(true)
		s.Store.wake()
	})
}

// closeArticle 是「收起」：停止进行中的回答并保存，保存成功后（下一帧）调用 OnClose。
func (s *Screen) closeArticle() {
	a := &s.art
	s.spawn(func() {
		if s.Store.Article().Close() {
			a.closed.Store(true)
			s.Store.wake()
		}
	})
}

// articleSaveError 是保存失败的提示：p-3 text-xs text-destructive，role="alert"，带下划线的「重试保存」。
func (s *Screen) articleSaveError(c ui.C, message string) {
	l := s.Look
	t := l.T
	ui.Flow(c.Key("save-error")).FillWidth().Shrink(0).Items(ui.Center).Gap(l.Space(1)).LineGap(0).Pad(l.Space(3)).
		Draw(func(d *ui.DrawContext) { markAlert(d) }).
		Children(func() {
			l.Text(c.Key("text"), message, kit.TextXS, kit.WeightNormal, t.Destructive, kit.Wrapped()).Shrink(1).MinWidth(0)
			if s.textButton(c.Key("retry"), locales.ArticleAskRetrySave(), textBtn{
				Style: kit.TextXS, Weight: kit.WeightNormal, Fg: t.Destructive, Underline: true,
			}) {
				s.spawn(func() { s.Store.Article().Persist() })
			}
		})
}

// articleBody 是消息区：min-h-0 flex-1 space-y-5 overflow-auto p-4。四种内容：加载中、读取失败（带重试）、消息列表、空态说明。
func (s *Screen) articleBody(c ui.C, spec ArticleSpec, v ArticleView) {
	l := s.Look
	t := l.T
	a := &s.art
	pad := l.Space(4)
	l.ScrollAreaView(c.Key("scroll"), [4]float32{pad, pad, pad, pad}, func(view kit.ScrollView) {
		if a.toEnd {
			view.State.ScrollToEnd()
			a.toEnd = false
		}
		switch {
		case v.Loading:
			ui.Column(c.Key("loading")).FillWidth().Draw(func(d *ui.DrawContext) { markStatus(d) }).Children(func() {
				l.Text(c.Key("text"), locales.CommonLoading(), kit.TextBase, kit.WeightNormal, t.Foreground)
			})
		case v.LoadError != "":
			if l.LoadErrorState(c.Key("load-error"), v.LoadError) {
				s.spawn(func() { s.Store.Article().Open(spec.Target, spec.SessionID) })
			}
		case len(v.Messages) > 0:
			ui.Column(c.Key("messages")).FillWidth().Gap(l.Space(5)).Children(func() {
				for _, m := range v.Messages {
					s.articleCard(c, m, v.Running)
				}
			})
		default:
			// text-sm leading-7 text-muted-foreground
			l.TextPx(c.Key("empty"), locales.ArticleAskEmpty(), 14, 28, kit.WeightNormal, t.MutedForeground, kit.Wrapped())
		}
	}).FillWidth().Grow(1).MinHeight(0).Follow(true)
}

// articleFooter 是底部：shrink-0 space-y-2 border-t border-border p-3。
func (s *Screen) articleFooter(c ui.C, spec ArticleSpec, v ArticleView, key string) {
	l := s.Look
	t := l.T
	a := &s.art
	art := s.Store.Article()
	draft := art.Draft(key)
	ui.Column(c.Key("footer")).FillWidth().Shrink(0).Gap(l.Space(2)).PadSides(l.Space(3)+1, l.Space(3), l.Space(3), l.Space(3)).
		Draw(func(d *ui.DrawContext) { l.PaintEdge(d, kit.EdgeTop, 1, t.Border) }).
		Children(func() {
			if q := draft.Quote; q != nil && q.Selection != nil {
				s.articleQuote(c, key, q)
			}
			webTop, webBottom := float32(6), float32(2)
			if s.Look.RootPx < 15 || s.Look.RootPx > 17 {
				webTop, webBottom = 5, 3
			}
			label := "\u2610 "
			if v.WebEnabled {
				label = "\u2611 "
			}
			// 开关按钮是行内元素，只占自己的宽度（外层 Row 吃掉 Column 的拉伸）；勾选符与文字是相邻的两个文本节点。
			// 它落在父级 16px/24px 行高的 strut 行盒里：行盒 24 高，按钮（16 高）中档从第 6 px 起（上 +6、下 +2）；小、大档像素取整后实测各上移 1px（5 / 3）。
			ui.Row(c.Key("web-wrap")).PadSides(webTop, 0, webBottom, 0).Children(func() {
				if s.textButton(c.Key("web"), label+locales.ArticleAskWeb(), textBtn{
					Style: kit.TextXS, Weight: kit.WeightNormal, Fg: t.MutedForeground,
					Role: gfx.RoleSwitch, Checked: v.WebEnabled, Disabled: v.Running,
					Runs: []string{label, locales.ArticleAskWeb()},
				}) {
					art.SetWeb(!v.WebEnabled)
				}
			})
			if art.MissingSearch() {
				s.articleMissingSearch(c, spec, key)
			}
			s.chatComposer(c, &a.in, composerSpec{
				Key: key, Running: v.Running, Disabled: art.Submitting() || v.Loading || v.LoadError != "",
				OnSend: func() { s.sendArticle(spec, key, false) },
				OnStop: art.Stop,
			})
		})
}

// articleQuote 是引用的选段：rounded border border-border p-2 text-xs，上面是「阅读视图 · 楼层」，选段最高 6rem 可滚动、保留换行，
// 下面「移除选段」。
func (s *Screen) articleQuote(c ui.C, key string, q *aiflow.ArticleTarget) {
	l := s.Look
	t := l.T
	head := articleViewLabel(q.View)
	if q.Floor != nil {
		head += fmt.Sprintf(" \u00b7 %d", *q.Floor)
	}
	selection := *q.Selection
	ui.Column(c.Key("quote")).FillWidth().Pad(l.Space(2) + 1).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Border: t.Border, BorderW: 1, Radius: l.Rem(0.25)})
		}).
		Children(func() {
			l.Text(c.Key("head"), head, kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Wrapped()).MarginBottom(l.Space(1))
			l.ScrollArea(c.Key("scroll"), [4]float32{}, func() {
				l.Text(c.Key("text"), selection, kit.TextXS, kit.WeightNormal, t.Foreground, kit.Wrapped())
			}).FillWidth().MaxHeight(l.Rem(6))
			ui.Row(c.Key("remove-wrap")).MarginTop(l.Space(1)).Children(func() {
				if s.textButton(c.Key("remove"), locales.ArticleAskRemoveQuote(), textBtn{
					Style: kit.TextXS, Weight: kit.WeightNormal, Fg: t.Primary,
				}) {
					s.Store.Article().SetQuote(key, nil)
				}
			})
		})
}

// articleViewLabel 是阅读视图的名字；不认识的视图原样显示。
func articleViewLabel(view string) string {
	switch view {
	case "body":
		return locales.ArticleAskViewBody()
	case "summary":
		return locales.ArticleAskViewSummary()
	case "transcript":
		return locales.ArticleAskViewTranscript()
	case "recognized":
		return locales.ArticleAskViewRecognized()
	case "replies":
		return locales.ArticleAskViewReplies()
	case "snapshot":
		return locales.ArticleAskViewSnapshot()
	case "themed":
		return locales.ArticleAskViewThemed()
	}
	return view
}

// articleMissingSearch 是「尚未配置联网搜索，问题已保留」：role="alert" space-y-2 rounded border border-border p-2 text-xs，
// 「前往配置」去设置的 AI 分区，「关闭联网后发送」关掉联网开关再发。
func (s *Screen) articleMissingSearch(c ui.C, spec ArticleSpec, key string) {
	l := s.Look
	t := l.T
	ui.Column(c.Key("no-search")).FillWidth().Gap(l.Space(2)).Pad(l.Space(2) + 1).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Border: t.Border, BorderW: 1, Radius: l.Rem(0.25)})
			markAlert(d)
		}).
		Children(func() {
			l.Text(c.Key("text"), locales.ArticleAskNoSearch(), kit.TextXS, kit.WeightNormal, t.Foreground, kit.Wrapped())
			ui.Flow(c.Key("buttons")).FillWidth().LineGap(0).Items(ui.Center).Children(func() {
				base := textBtn{Style: kit.TextXS, Weight: kit.WeightNormal, Fg: t.Primary}
				if s.textButton(c.Key("configure"), locales.ArticleAskConfigure(), base) && s.Store.OnOpenSettingsAI != nil {
					s.Store.OnOpenSettingsAI()
				}
				ui.Row(c.Key("offline-wrap")).MarginLeft(l.Space(3)).Children(func() {
					if s.textButton(c.Key("offline"), locales.ArticleAskSendOffline(), base) {
						s.sendArticle(spec, key, true)
					}
				})
			})
		})
}

// sendArticle 发出草稿：先把输入框里此刻的文字写回 store，再在后台发送（Send 阻塞到这一轮结束）。
func (s *Screen) sendArticle(spec ArticleSpec, key string, offline bool) {
	a := &s.art
	if a.in.key == key {
		s.flushDraft(&a.in)
	}
	a.toEnd = true
	target := spec.Target
	opts := SendOptions{Offline: offline, ResolveTarget: spec.ResolveTarget, OnSubmitted: spec.OnSubmitted}
	s.spawn(func() { s.Store.Article().Send(key, target, opts) })
}
