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
	ui.Column(c.Key("article")).FillWidth().Grow(1).MinHeight(0).Children(func() {
		// shrink-0 border-b border-border p-3：一枚 text-sm text-primary 的文字钮。
		ui.Row(c.Key("head")).FillWidth().Shrink(0).PadSides(l.Space(3), l.Space(3), l.Space(3)+1, l.Space(3)).
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
