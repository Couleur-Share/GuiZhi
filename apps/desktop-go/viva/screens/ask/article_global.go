package ask

import (
	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/aiflow"
	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// globalArticle 是「围绕本文」会话在全局问答里的页面（渲染层 GlobalArticleConversation）：
// 头部一枚「返回文章」，下面是嵌入的本文对话。flex min-h-0 flex-1 flex-col。
//
// 返回文章与定位来源都会先打开文章（切到知识库并选中它）；文章不存在或在回收站里时弹出带原因的提示。
// 本文对话换了会话（新对话、从历史打开）时，把全局这边的会话信息跟过去（SyncArticleSession）。
func (s *Screen) globalArticle(c ui.C, session services.AskSessionMeta) {
	l := s.Look
	t := l.T
	itemID := session.ItemID
	strutTop, strutBottom := strutInsets(l)
	ui.Column(c.Key("article")).FillWidth().Grow(1).MinHeight(0).Children(func() {
		// shrink-0 border-b border-border p-3：一枚 text-sm text-primary 的文字钮。行内按钮与父级 16px/24px 行高的
		// strut 按基线对齐：行盒 24 高（随字号档缩放），按钮（20 高）落在其中偏下，上下各补 StrutInsets（实测 1280x800 三档：2/2.5、3/2、3/2.5）。
		ui.Row(c.Key("head")).FillWidth().Shrink(0).PadSides(l.Space(3)+strutTop, l.Space(3), l.Space(3)+strutBottom, l.Space(3)).
			Draw(func(d *ui.DrawContext) { l.PaintEdge(d, kit.EdgeBottom, 1, t.Border) }).
			Children(func() {
				if s.textButton(c.Key("back"), locales.ArticleAskBack(), textBtn{Style: kit.TextSM, Weight: kit.WeightNormal, Fg: t.Primary}) {
					s.spawn(func() { s.Store.OpenArticle(itemID) })
				}
			})
		ui.Column(c.Key("conversation")).FillWidth().Grow(1).MinHeight(0).Children(func() {
			if session.Target == nil {
				return
			}
			s.ArticlePanel(c, ArticleSpec{
				Target:    *session.Target,
				SessionID: session.ID,
				OnSessionChange: func(id string) {
					s.Store.SyncArticleSession(session, id)
				},
				OnLocate: func(source aiflow.ArticleSource, done func(found bool)) {
					s.spawn(func() {
						if !s.Store.OpenArticle(itemID) || s.Hooks.LocateArticle == nil {
							done(false)
							return
						}
						s.Hooks.LocateArticle(source, done)
					})
				},
				StopOnUnmount: true,
			})
		})
	})
}

// strutInsets 是行内按钮落在父级行盒（text-base 1.5rem）里的上下偏移。Chromium 把基线和行盒边缘取整到像素，
// 所以偏移不随字号档线性缩放；这里记录 1280x800 三档（根字号 14/16/18）的实测值：
// 小档 2 / 2.5，中档 3 / 2，大档 3 / 2.5。
func strutInsets(l *kit.Look) (top, bottom float32) {
	switch {
	case l.RootPx < 15:
		return 2, 2.5
	case l.RootPx > 17:
		return 3, 2.5
	}
	return 3, 2
}
