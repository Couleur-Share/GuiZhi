package library

import (
	"strconv"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/icons"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
	"guizhi-mygo-shell/viva/tokens"
)

// 来源评论卡片（渲染层 SourceCommentsCard 与它的提供者 SourceCommentsContext）：抖音与小红书条目可以采集来源评论，
// 卡片折叠在工具侧栏的内容处理里；头部「更多」菜单的「采集评论 / 查看来源评论」把它展开并打开侧栏。
// 评论的读取、重新采集与失败原因在 Store（store_tools.go），这里只管画与开合。

// commentsUI 是评论卡片的界面状态：展开与否（提供者的 open）、采集数量档位（limit，0 取默认的 20）与档位下拉。
type commentsUI struct {
	open     bool
	limit    int
	limitSel kit.SelectState
}

// limitValue 是当前的采集数量档位。
func (u *commentsUI) limitValue() int {
	if u.limit == 0 {
		return services.LibraryCommentLimitDefault
	}
	return u.limit
}

// commentsCard 声明评论卡片：w-full overflow-hidden rounded-xl border border-border bg-background/55。
// 不支持来源评论的条目没有它；支持时，展开着、已有评论或读取失败才摆出来（否则整块隐藏，入口只有头部菜单）。
func (s *Screen) commentsCard(c ui.C, v View, item services.LibraryItem) {
	l := s.Look
	t := l.T
	cm := v.Detail.Tools.Comments
	cu := &s.detail.tools.comments
	if v.Detail.Tools.ItemID != item.ID || !cm.Supported {
		return
	}
	if !cu.open && len(cm.Comments) == 0 && cm.LoadError == "" {
		return
	}
	// w-full 在换行的 flex 行里是「基准宽 100%」：卡片独占一行，不与前面的 chip 并排（FillWidth 是占满本行剩余宽度，会挤在 chip 旁边）。
	ui.Column(c).WidthPercent(100).Pad(1).
		Draw(func(d *ui.DrawContext) {
			l.PaintSurface(d, kit.Surface{Bg: t.Background.WithAlpha(0.55), Border: t.Border, BorderW: 1, Radius: l.Rem(0.75)})
		}).
		Children(func() {
			s.commentsHeader(c, cm, cu)
			if cm.LoadError != "" {
				if l.LoadErrorState(c.Key("load-error"), cm.LoadError) {
					s.Store.Spawn(func() { s.Store.RetryComments(s.Ctx) })
				}
			}
			if cu.open && cm.LoadError == "" {
				s.commentsBody(c, cm, cu)
			}
		})
}

// commentsHeader 是卡片头：flex min-h-9 items-center gap-2 px-3 py-2，一枚占满的展开钮（箭头、评论图标、标题、条数）。
func (s *Screen) commentsHeader(c ui.C, cm CommentsView, cu *commentsUI) {
	l := s.Look
	t := l.T
	title := locales.LibrarySourceComments()
	name := title
	if n := len(cm.Comments); n > 0 {
		name += " " + strconv.Itoa(n)
	}
	ui.Row(c.Key("head")).FillWidth().MinHeight(l.Rem(2.25)).Items(ui.Center).Gap(l.Space(2)).PadSides(l.Space(2), l.Space(3), l.Space(2), l.Space(3)).Children(func() {
		toggle := s.inlineButton(c.Key("toggle"), inlineSpec{
			Name: name, Disclosure: true, Open: cu.open, Fg: t.Foreground, Gap: l.Space(2),
		}, func(fg tokens.Color) {
			chevron := icons.ChevronRight
			if cu.open {
				chevron = icons.ChevronDown
			}
			l.Icon(c.Key("chevron"), chevron, l.Rem(0.875), fg)
			l.Icon(c.Key("icon"), icons.MessageCircle, l.Rem(0.875), t.MutedForeground)
			l.Text(c.Key("title"), title, kit.TextXS, kit.WeightMedium, fg)
			if n := len(cm.Comments); n > 0 {
				l.Text(c.Key("count"), strconv.Itoa(n), kit.TextXS, kit.WeightMedium, t.MutedForeground)
			}
		}).Grow(1).Shrink(1).MinWidth(0)
		if toggle.Clicked() {
			cu.open = !cu.open
		}
	})
}

// commentsBody 是展开后的内容：max-h-64 overflow-y-auto border-t border-border/60 px-3 py-2。
// 先是工具行（采集数量、刷新），然后是失败原因、加载中、空状态或评论列表。
func (s *Screen) commentsBody(c ui.C, cm CommentsView, cu *commentsUI) {
	l := s.Look
	t := l.T
	busy := cm.Reading || cm.Loading
	ui.Column(c.Key("body")).FillWidth().PadSides(1, 0, 0, 0).
		Draw(func(d *ui.DrawContext) { l.PaintEdge(d, kit.EdgeTop, 1, t.Border.WithAlpha(0.6)) }).
		Children(func() {
			l.ScrollAreaView(c.Key("scroll"), [4]float32{l.Space(2), l.Space(3), l.Space(2), l.Space(3)}, func(kit.ScrollView) {
				ui.Column(c.Key("content")).FillWidth().Children(func() {
					s.commentsToolbar(c, cm, cu)
					if cm.Error != "" {
						l.Text(c.Key("error"), cm.Error, kit.TextXS, kit.WeightNormal, t.Destructive, kit.Wrapped()).MarginBottom(l.Space(2))
					}
					if busy {
						ui.Row(c.Key("loading")).FillWidth().PadSides(l.Space(2), 0, l.Space(2), 0).Draw(func(d *ui.DrawContext) {
							d.Canvas.ContainerWithOptions(d.Rect, gfx.RoleStatus, "", gfx.HitOptions{Live: gfx.LivePolite})
						}).Children(func() {
							l.Text(c.Key("text"), locales.CommonLoading(), kit.TextXS, kit.WeightNormal, t.MutedForeground)
						})
					}
					if len(cm.Comments) == 0 && !busy && cm.Error == "" {
						empty := locales.LibraryNoSourceComments()
						if cm.Attempted {
							empty = locales.LibraryCommentsLoadedEmpty()
						}
						ui.Row(c.Key("empty")).FillWidth().PadSides(l.Space(4), 0, l.Space(4), 0).Children(func() {
							l.Text(c.Key("text"), empty, kit.TextXS, kit.WeightNormal, t.MutedForeground, kit.Wrapped(), kit.Centered()).Grow(1)
						})
						return
					}
					ui.Column(c.Key("list")).FillWidth().Children(func() {
						for i, comment := range cm.Comments {
							s.commentArticle(c.Key("comment-"+comment.ID), comment, i == 0)
						}
					})
				})
			}).FillWidth().MaxHeight(l.Rem(16) - 1)
		})
}

// commentsToolbar 是工具行：mb-2 flex items-center justify-end gap-2。采集数量下拉（w-20，h-7 rounded-md bg-muted px-2 text-xs）与刷新钮
// （inline-flex h-7 items-center gap-1 rounded-md border border-border px-2 text-[11px]，加载中是转圈，读取中、加载中与读取失败时禁用）。
func (s *Screen) commentsToolbar(c ui.C, cm CommentsView, cu *commentsUI) {
	l := s.Look
	t := l.T
	ui.Row(c.Key("toolbar")).FillWidth().Items(ui.Center).Justify(ui.End).Gap(l.Space(2)).MarginBottom(l.Space(2)).Children(func() {
		options := make([]kit.SelectOption, len(services.LibraryCommentLimits))
		for i, n := range services.LibraryCommentLimits {
			text := strconv.Itoa(n)
			options[i] = kit.SelectOption{Value: text, Label: text}
		}
		trigger := kit.SelectTriggerStyle{
			Height: l.Rem(1.75), MinWidth: l.Space(20), PadX: l.Space(2), Gap: l.Space(1), Radius: l.Rem(0.75) - 2,
			Bg: t.Muted, BgHover: t.Muted.WithAlpha(0.8), FontPx: l.Rem(0.75), LinePx: l.Rem(1), Transition: kit.DurationDefault,
		}
		picked, changed := l.Select(c.Key("limit"), &cu.limitSel, kit.SelectSpec{
			Label: locales.LibraryCommentCaptureLimit(), Value: strconv.Itoa(cu.limitValue()), Options: options,
			MenuMinWidth: 80, Trigger: &trigger,
		})
		if changed {
			if n, err := strconv.Atoi(picked); err == nil {
				cu.limit = n
			}
		}
		label := locales.LibraryCollectSourceComments()
		if len(cm.Comments) > 0 {
			label = locales.LibraryRefreshComments()
		}
		spec := kit.ButtonSpec{Label: label, Disabled: cm.Loading || cm.Reading || cm.LoadError != "", Busy: cm.Loading, BusySize: l.Rem(0.75)}
		if !cm.Loading {
			spec.Icon, spec.IconSize = icons.RefreshCw, l.Rem(0.75)
		}
		refresh := l.Button(c.Key("refresh"), spec, kit.ButtonStyle{
			Height: l.Rem(1.75), PadX: l.Space(2), Radius: l.Rem(0.75) - 2, Gap: l.Space(1), FontPx: 11, LinePx: 16.5, Weight: kit.WeightNormal,
			Fg: t.MutedForeground, FgHover: t.Foreground, BgHover: t.Muted.WithAlpha(0.6),
			Border: t.Border, BorderW: 1, Transition: kit.DurationDefault, DisabledOpacity: 0.5, Surface: t.Background,
		})
		if refresh.Clicked() {
			limit := cu.limitValue()
			s.Store.Spawn(func() { s.Store.RefreshComments(s.Ctx, limit) })
		}
	})
}

// commentArticle 是一条评论：py-2.5，第二条起带 border-border/60 的上边线（divide-y）。
// 第一行是作者（font-medium text-foreground/80，没有名字是「未知用户」）、点赞数、发布日期（有才有）；
// 正文 mt-1 whitespace-pre-wrap break-words text-xs leading-relaxed text-foreground/85。
func (s *Screen) commentArticle(c ui.C, comment services.LibrarySourceComment, first bool) {
	l := s.Look
	t := l.T
	top := l.Space(2.5)
	if !first {
		top++ // divide-y 的上边线占在盒内一像素
	}
	ui.Column(c).FillWidth().PadSides(top, 0, l.Space(2.5), 0).
		Draw(func(d *ui.DrawContext) {
			if !first {
				l.PaintEdge(d, kit.EdgeTop, 1, t.Border.WithAlpha(0.6))
			}
		}).
		Children(func() {
			ui.Row(c.Key("meta")).FillWidth().Items(ui.Center).Gap(l.Space(2)).Children(func() {
				author := comment.AuthorName
				if author == "" {
					author = locales.LibraryUnknownAuthor()
				}
				l.TextPx(c.Key("author"), author, 11, 16.5, kit.WeightMedium, t.Foreground.WithAlpha(0.8))
				l.TextPx(c.Key("likes"), locales.LibraryCommentLikes(comment.LikeCount), 11, 16.5, kit.WeightNormal, t.MutedForeground)
				if comment.PublishedAt != nil && *comment.PublishedAt != 0 {
					l.TextPx(c.Key("time"), kit.FormatDate(*comment.PublishedAt, s.loc(), s.Locale), 11, 16.5, kit.WeightNormal, t.MutedForeground)
				}
			})
			l.TextPx(c.Key("content"), comment.Content, 12, 19.5, kit.WeightNormal, t.Foreground.WithAlpha(0.85), kit.Wrapped()).MarginTop(l.Space(1))
		})
}
