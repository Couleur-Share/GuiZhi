package library

import (
	"strings"
	"sync"

	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// 标签浮层里的「AI 建议标签」一段（渲染层 AiTagSuggest）：点「生成」让模型按条目标题与正文出候选，
// 结果以可点选的虚线小块展示，点一下就加进条目标签；右边的 X 忽略全部建议。
// 生成调用在 Store（SuggestItemTags，同步、会阻塞），这里放到后台跑；忙标记与候选属于浮层，不进 Store。

// tagSuggestUI 是一块标签浮层里 AI 建议的状态：忙标记与候选。
//
// 它被后台协程写、被界面线程读，所以用互斥锁保护，并且总是以指针挂在浮层的界面状态上（syncTools 会整体覆盖 toolsUI，
// 值字段会让在途协程写进被覆盖的内存）。令牌（token）标识「这一次生成」：浮层关闭、重新打开、换条目时 reset 让它加一，
// 在途的旧请求回来发现令牌变了，什么都不写——候选不会漏到下一次打开，忙标记也不会卡住新一轮。
type tagSuggestUI struct {
	mu          sync.Mutex
	token       int
	generating  bool
	suggestions []string
}

// reset 清空忙标记与候选并作废在途的请求；nil 安全，没有状态时不动令牌（关着的浮层每帧都会调它）。
func (u *tagSuggestUI) reset() {
	if u == nil {
		return
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.generating || u.suggestions != nil {
		u.token++
		u.generating, u.suggestions = false, nil
	}
}

// snapshot 取此刻的忙标记与候选（候选是副本）。
func (u *tagSuggestUI) snapshot() (generating bool, suggestions []string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.generating, append([]string(nil), u.suggestions...)
}

// begin 开始一次生成：已经在生成时返回 false（渲染层的 isGenerating 早退）；否则置忙并返回这一次的令牌。
func (u *tagSuggestUI) begin() (token int, ok bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.generating {
		return 0, false
	}
	u.generating = true
	return u.token, true
}

// finish 在请求返回时落结果：令牌已变（浮层关过、换了条目）就什么都不写；否则放下忙标记，产出了建议（ok）才替换候选
// （条目不存在或调用失败时候选保持原样）。返回是否写入了。
func (u *tagSuggestUI) finish(token int, tags []string, ok bool) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	if token != u.token {
		return false
	}
	u.generating = false
	if ok {
		u.suggestions = append([]string{}, tags...)
	}
	return true
}

// suggest 返回标签行的 AI 建议状态（首次用到时创建）。
func (e *tagEditorUI) suggest() *tagSuggestUI {
	if e.ai == nil {
		e.ai = &tagSuggestUI{}
	}
	return e.ai
}

// suggest 返回列表标签选择浮层的 AI 建议状态（首次用到时创建）。
func (p *tagPicker) suggest() *tagSuggestUI {
	if p.ai == nil {
		p.ai = &tagSuggestUI{}
	}
	return p.ai
}

// aiTagSuggest 是「AI 建议标签」一段：mt-3 border-t border-border/60 pt-2.5，标题与操作一行（flex items-center
// justify-between gap-2），有候选时下面一排虚线小块。names 是条目此刻的标签名，已经带着的候选不再显示。
func (s *Screen) aiTagSuggest(c ui.C, names []string, d tagPickerData) {
	l := s.Look
	t := l.T
	generating, suggestions := d.ai.snapshot()
	var pending []string
	for _, name := range suggestions {
		if !containsString(names, name) && !containsString(pending, name) {
			pending = append(pending, name)
		}
	}
	ui.Column(c.Key("ai")).FillWidth().MarginTop(l.Space(3)).PadSides(l.Space(2.5)+1, 0, 0, 0).
		Draw(func(dc *ui.DrawContext) { l.PaintEdge(dc, kit.EdgeTop, 1, t.Border.WithAlpha(0.6)) }).
		Children(func() {
			ui.Row(c.Key("head")).FillWidth().Items(ui.Center).Justify(ui.Between).Gap(l.Space(2)).Children(func() {
				l.TextTracked(c.Key("title"), strings.ToUpper(locales.LibraryAiTagsSuggest()), kit.TextXS, kit.WeightMedium, t.MutedForeground, 0.025)
				ui.Row(c.Key("actions")).Items(ui.Center).Gap(l.Space(0.5)).Children(func() {
					if len(pending) > 0 {
						// inline-flex h-6 w-6 items-center justify-center rounded-md text-muted-foreground/60 transition-colors hover:text-foreground
						dismiss := locales.LibraryAiTagsDismiss()
						x := l.Button(c.Key("dismiss"), kit.ButtonSpec{Icon: icons.X, IconSize: l.Rem(0.875), Center: true, Name: dismiss, Hint: dismiss}, kit.ButtonStyle{
							Height: l.Rem(1.5), Width: l.Rem(1.5), Radius: l.Rem(0.75) - 2,
							Fg: t.MutedForeground.WithAlpha(0.6), FgHover: t.Foreground, Transition: kit.DurationDefault,
						})
						if x.Clicked() {
							d.ai.reset()
						}
					}
					// inline-flex h-6 items-center gap-1 rounded-md px-1.5 text-xs text-primary transition-colors hover:bg-primary/10 disabled:opacity-60
					spec := kit.ButtonSpec{Label: locales.LibraryTagsGenerate(), Disabled: generating, Busy: generating, BusySize: l.Rem(0.875)}
					if !generating {
						spec.Icon, spec.IconSize = icons.Sparkles, l.Rem(0.875)
					}
					gen := l.Button(c.Key("generate"), spec, kit.ButtonStyle{
						Height: l.Rem(1.5), PadX: l.Space(1.5), Radius: l.Rem(0.75) - 2, Gap: l.Space(1), Text: kit.TextXS, Weight: kit.WeightNormal,
						Fg: t.Primary, BgHover: t.Primary.WithAlpha(0.1), Transition: kit.DurationDefault, DisabledOpacity: 0.6,
					})
					if gen.Clicked() {
						s.generateTagSuggestions(d, names)
					}
				})
			})
			if len(pending) == 0 {
				return
			}
			// mt-1.5 flex flex-wrap gap-1
			ui.Flow(c.Key("candidates")).FillWidth().MarginTop(l.Space(1.5)).Gap(l.Space(1)).LineGap(l.Space(1)).Children(func() {
				for _, name := range pending {
					// inline-flex items-center gap-0.5 rounded-full border border-dashed border-primary/40 px-2 py-0.5 text-xs text-primary
					// transition-colors hover:bg-primary/10
					chip := l.Button(c.Key("cand-"+name), kit.ButtonSpec{Label: name, Icon: icons.Plus, IconSize: l.Rem(0.75)}, kit.ButtonStyle{
						PadX: l.Space(2), PadY: l.Space(0.5), Radius: 9999, Gap: l.Space(0.5), Text: kit.TextXS, Weight: kit.WeightNormal,
						Fg: t.Primary, BgHover: t.Primary.WithAlpha(0.1), Border: t.Primary.WithAlpha(0.4), BorderW: 1, Dashed: true,
						Transition: kit.DurationDefault,
					})
					if chip.Clicked() {
						d.apply(append(append([]string(nil), names...), name))
					}
				}
			})
		})
}

// generateTagSuggestions 在后台生成建议（渲染层 AiTagSuggest 的 generate）：在途时什么也不做；回来后令牌还对得上才写入候选，
// 随后唤醒界面重画。「建议的标签都已存在」的判断用点击当时的标签名（渲染层闭包里的 currentNames），空建议也算都已存在；
// 提示不依赖令牌——渲染层里浮层关了之后提示照样会出。
func (s *Screen) generateTagSuggestions(d tagPickerData, names []string) {
	token, ok := d.ai.begin()
	if !ok {
		return
	}
	current := append([]string(nil), names...)
	store, ctx, guard, itemID, ai := s.Store, s.Ctx, s.Guard, d.itemID, d.ai
	store.Spawn(func() {
		tags, produced := store.SuggestItemTags(ctx, itemID)
		ai.finish(token, tags, produced)
		if produced && allIn(tags, current) && guard.Notify != nil {
			guard.Notify.Info(locales.LibraryAiTagsAllExist())
		}
		store.wake()
	})
}

// allIn 报告 list 里的每一项是不是都在 set 里（空列表为真，同 JS 的 every）。
func allIn(list, set []string) bool {
	for _, item := range list {
		if !containsString(set, item) {
			return false
		}
	}
	return true
}
