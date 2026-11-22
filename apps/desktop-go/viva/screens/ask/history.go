package ask

import (
	"unicode/utf16"

	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// dayMs 是一天的毫秒数；时间范围筛选按「近 N 天」把 from 设成此刻往前 N 天。
const dayMs = 86400000

// renameMaxUnits 是会话标题的长度上限（渲染层输入框 maxLength={200}，按 UTF-16 码元计）。
const renameMaxUnits = 200

// syncSearch 让历史搜索框的防抖跟上输入：文字每变一次重排计时，停下 250 ms 后若与当前筛选的搜索词不同才发起筛选
// （渲染层 HistoryControls 的 useEffect + setTimeout）。
func (s *Screen) syncSearch() {
	text := s.search.Text()
	if text == s.searchSeen {
		return
	}
	s.searchSeen = text
	if s.searchStop != nil {
		s.searchStop()
	}
	s.searchStop = s.after(searchDebounce, func() {
		s.spawn(func() {
			q := s.History.View().Query
			if text == q.Search {
				return
			}
			q.Search = text
			s.History.Filter(q)
		})
	})
}

// filter 在当前筛选条件上改一项后重新读取（`filter({ ...query, ... })`）。
func (s *Screen) filter(mutate func(q *services.AskSessionQuery)) {
	q := s.History.View().Query
	mutate(&q)
	s.spawn(func() { s.History.Filter(q) })
}

// rangeValue 是时间范围下拉的当前值：没有下限是「全部时间」，距今不到 8 天归「近 7 天」，其余「近 30 天」。
func (s *Screen) rangeValue(q services.AskSessionQuery) string {
	if q.From == nil || *q.From == 0 {
		return ""
	}
	if s.nowTime().UnixMilli()-*q.From < 8*dayMs {
		return "7"
	}
	return "30"
}

// historyControls 是历史会话的筛选控件（渲染层 HistoryControls）：my-2 space-y-2 px-1，
// 搜索框、范围与时间两个并排的下拉，以及有「本文问答」会话时的关联文章下拉。
func (s *Screen) historyControls(c ui.C, h HistoryView) {
	l := s.Look
	q := h.Query
	ui.Column(c.Key("history-controls")).FillWidth().MarginY(l.Space(2)).PadX(l.Space(1)).Gap(l.Space(2)).Children(func() {
		// w-full rounded-lg border border-border bg-background px-3 py-2 text-xs：行高 1rem，加上下内边距与边框。
		s.searchFocus = s.boxedInput(c.Key("search"), &s.search, locales.AskHistorySearch(), locales.AskHistorySearchPlaceholder(),
			l.Rem(0.75), l.Rem(1)+l.Space(4)+2, l.Space(3), l.Rem(0.75))

		ui.Row(c.Key("selects")).FillWidth().Gap(l.Space(2)).Children(func() {
			// min-w-0 flex-1：两个下拉平分一行。
			half := func(key string, body func()) {
				ui.Column(c.Key(key)).Grow(1).Shrink(1).Basis(0).MinWidth(0).Children(body)
			}
			half("scope", func() {
				label := locales.AskHistoryScope()
				picked, changed := l.Select(c.Key("scope-select"), &s.scope, kit.SelectSpec{
					Label: label, Value: q.Scope, Width: -1,
					Options: []kit.SelectOption{
						{Value: "", Label: locales.AskHistoryScopeAll()},
						{Value: services.AskScopeKnowledge, Label: locales.AskHistoryScopeKnowledge()},
						{Value: services.AskScopeArticle, Label: locales.ArticleAskHistoryBadge()},
					},
				})
				if changed {
					s.filter(func(q *services.AskSessionQuery) { q.Scope, q.ItemID = picked, "" })
				}
			})
			half("range", func() {
				label := locales.AskHistoryRange()
				picked, changed := l.Select(c.Key("range-select"), &s.rng, kit.SelectSpec{
					Label: label, Value: s.rangeValue(q), Width: -1,
					Options: []kit.SelectOption{
						{Value: "", Label: locales.AskHistoryRangeAll()},
						{Value: "7", Label: locales.AskHistoryRange7d()},
						{Value: "30", Label: locales.AskHistoryRange30d()},
					},
				})
				if changed {
					s.filter(func(q *services.AskSessionQuery) {
						if picked == "" {
							q.From = nil
							return
						}
						days := int64(7)
						if picked == "30" {
							days = 30
						}
						from := s.nowTime().UnixMilli() - days*dayMs
						q.From = &from
					})
				}
			})
		})

		if len(h.Articles) > 0 || q.ItemID != "" {
			options := []kit.SelectOption{{Value: "", Label: locales.AskHistoryArticleAll()}}
			for _, a := range h.Articles {
				title := a.Title
				if title == "" {
					title = locales.AskHistoryArticleUntitled()
				}
				options = append(options, kit.SelectOption{Value: a.ID, Label: title})
			}
			picked, changed := l.Select(c.Key("article-select"), &s.article, kit.SelectSpec{
				Label: locales.AskHistoryArticle(), Value: q.ItemID, Width: -1, Options: options,
			})
			if changed {
				s.filter(func(q *services.AskSessionQuery) { q.ItemID = picked })
			}
		}
	})
}

// utf16Len 是字符串按 UTF-16 码元计的长度（与渲染层 maxLength 同口径）。
func utf16Len(s string) int { return len(utf16.Encode([]rune(s))) }

// renameModal 是重命名对话框（渲染层 HistoryRename）：一个标题输入框与「保存」钮；标题为空（只有空白）或正在保存时钮禁用，
// 保存成功才关闭，失败时对话框保持打开、原因由 HistoryStore 弹出。
func (s *Screen) renameModal(c ui.C) {
	s.renameDialog(c, "rename", &s.rename, s.History)
}

// renameDialog 是重命名对话框的版面，侧栏的历史会话与围绕本文的「本文历史」共用：状态在 r，保存走 hist。key 区分两处的对话框身份。
func (s *Screen) renameDialog(c ui.C, key string, r *renameState, hist *HistoryStore) {
	l := s.Look
	t := l.T
	if r.saved.Swap(false) {
		r.session = nil
	}
	if r.session != nil {
		if text := r.field.Text(); utf16Len(text) > renameMaxUnits {
			r.field.SetText(utf16Prefix(text, renameMaxUnits))
		}
	}
	closed := l.Modal(c.Key(key), &r.modal, kit.ModalSpec{
		Open: r.session != nil, Title: locales.AskRenameSession(), CloseLabel: locales.CommonClose(),
		Body: func(c ui.C) {
			// mb-3 w-full rounded border border-border bg-background p-2：文字 1rem、行高 1.5。
			ui.Column(c.Key("field")).FillWidth().MarginBottom(l.Space(3)).Children(func() {
				s.boxedInput(c.Key("title"), &r.field, locales.AskSessionTitle(), "",
					l.Rem(1), l.Rem(1.5)+l.Space(4)+2, l.Space(2), l.Rem(0.25))
			})
			// Button md primary：h-10 px-5 text-sm font-semibold rounded-xl，shadow-sm shadow-primary/25。
			busy := r.busy.Load()
			disabled := busy || jsTrim(r.field.Text()) == ""
			b := l.Button(c.Key("save"), kit.ButtonSpec{Label: locales.CommonSave(), Disabled: disabled, Center: true}, kit.ButtonStyle{
				Height: l.Rem(2.5), PadX: l.Space(5), Radius: l.Rem(0.75), Gap: l.Space(2), Text: kit.TextSM, Weight: kit.WeightSemibold,
				Fg: t.PrimaryForeground, Bg: t.Primary, Transition: kit.DurationBase, DisabledOpacity: 0.5, Surface: t.Popover,
				Shadows: []tokens.Shadow{{Y: 1, Blur: 2, Color: t.Primary.WithAlpha(0.25)}},
			})
			if b.Clicked() {
				s.saveRename(r, hist)
			}
		},
	})
	if closed {
		r.session = nil
	}
}

// saveRename 在后台保存新标题；成功后下一帧关闭对话框。
func (s *Screen) saveRename(r *renameState, hist *HistoryStore) {
	session := r.session
	if session == nil || r.busy.Load() {
		return
	}
	id, title := session.ID, r.field.Text()
	r.busy.Store(true)
	s.spawn(func() {
		ok := hist.Update(id, services.AskSessionMetaPatch{Title: &title})
		r.busy.Store(false)
		if ok {
			r.saved.Store(true)
		}
		s.Store.wake()
	})
}
