package wiki

import (
	"sync"

	"guizhi-mygo-shell/native/services"
)

// SuggestionView 是页面详情里「更新建议」区块每帧读取的快照。
type SuggestionView struct {
	// PageID 是这份状态对应的页面（空表示没有选中页）。
	PageID string
	// Suggestion 为 nil 表示这页没有待审阅的建议（或还在读取）。
	Suggestion *services.WikiSuggestion
	Loading    bool
	// Error 是读取或采用失败的原文（可复制的详情）；界面在它前面加「更新建议读取失败」之类的固定提示。
	Error string
	// Stale 为 true 表示刚才采用时发现建议或页面已经变化（令牌失效）：状态已自动重读，界面提示用户重新核对。
	Stale bool
}

// LegacyCoverage 报告页面是否是旧版编译器生成的（可能只用了来源开头）：界面据此提示并给出「选择资料升级」入口。
func LegacyCoverage(promptVersion string) bool { return promptVersion != services.WikiCompilerVersion }

// SuggestionStore 是当前选中页的更新建议状态（WikiUpdateSuggestion）：手动改过的页面（或仍有旧版来源的页面）在来源更新后
// 不会被直接覆盖，新内容存成建议等用户审阅；采用时后端核对令牌，页面或建议变化过就拒绝。
// 它跟随主状态的选中页：切页、页面更新时间变化时自动重读；没有选中页时清空。
type SuggestionStore struct {
	root *Store

	mu   sync.Mutex
	view SuggestionView
	// key 是上次读取对应的（页面 ID, 页面更新时间），不变就不重复读。
	keyPage string
	keyAt   int64
	gen     int
}

func newSuggestionStore(root *Store) *SuggestionStore { return &SuggestionStore{root: root} }

// View 返回当前快照。
func (g *SuggestionStore) View() SuggestionView {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.view
}

// clear 没有选中页：清空状态并让在途的读取作废。
func (g *SuggestionStore) clear() {
	g.mu.Lock()
	g.gen++
	g.view, g.keyPage, g.keyAt = SuggestionView{}, "", 0
	g.mu.Unlock()
	g.root.wake()
}

// clearIf 页面被删除：只在当前跟随的就是它时清空。
func (g *SuggestionStore) clearIf(pageID string) {
	g.mu.Lock()
	same := g.view.PageID == pageID
	g.mu.Unlock()
	if same {
		g.clear()
	}
}

// follow 让状态跟随一页：页面 ID 或更新时间变了才重读（页面更新意味着建议可能已被采用或失效）。
func (g *SuggestionStore) follow(pageID string, updatedAt int64) {
	g.mu.Lock()
	if g.keyPage == pageID && g.keyAt == updatedAt {
		g.mu.Unlock()
		return
	}
	g.mu.Unlock()
	g.load(pageID, updatedAt)
}

// Reload 重读当前页的建议（一轮编译结束后调用：为手动改过的页面生成建议不会改动页面本身）。没有跟随的页面时忽略。
func (g *SuggestionStore) Reload() {
	g.mu.Lock()
	id, at := g.view.PageID, g.keyAt
	g.mu.Unlock()
	if id != "" {
		g.load(id, at)
	}
}

func (g *SuggestionStore) load(pageID string, updatedAt int64) {
	g.mu.Lock()
	g.gen++
	gen := g.gen
	if g.view.PageID != pageID {
		g.view = SuggestionView{}
	}
	g.view.PageID, g.view.Loading, g.view.Error = pageID, true, ""
	g.keyPage, g.keyAt = pageID, updatedAt
	g.mu.Unlock()
	g.root.wake()

	suggestion, err := g.root.svc.Suggestion(g.root.ctx(), pageID)

	g.mu.Lock()
	if gen != g.gen {
		g.mu.Unlock()
		return // 已被更新的读取取代，或已清空
	}
	g.view.Loading = false
	if err != nil {
		g.view.Suggestion, g.view.Error = nil, err.Error()
		g.keyPage = "" // 失败的读取不算数：下次 follow 会重试
	} else {
		g.view.Suggestion = suggestion
	}
	g.mu.Unlock()
	if err != nil {
		g.root.guard.Background("wiki.suggestion", err)
	}
	g.root.wake()
}

// Accept 采用当前建议（接受后后端会保存当前版本用于恢复）。成功清掉建议并重读这一页与目录，返回 true。
// 令牌失效（建议或页面已变化）不是故障：自动重读建议、置 Stale 让界面提示重新核对，返回 false。
// 其余失败写进 Error（保留建议，可重试）。没有建议或正在读取时忽略。
func (g *SuggestionStore) Accept() bool {
	g.mu.Lock()
	pageID, sugg := g.view.PageID, g.view.Suggestion
	if sugg == nil || g.view.Loading {
		g.mu.Unlock()
		return false
	}
	token := sugg.Token
	g.view.Error, g.view.Stale = "", false
	g.mu.Unlock()
	g.root.wake()

	ok, err := g.root.svc.AcceptSuggestion(g.root.ctx(), pageID, token)
	if err != nil {
		g.mu.Lock()
		if g.view.PageID == pageID {
			g.view.Error = err.Error()
		}
		g.mu.Unlock()
		g.root.guard.Background("wiki.acceptSuggestion", err)
		g.root.wake()
		return false
	}
	if !ok {
		g.Reload()
		g.mu.Lock()
		if g.view.PageID == pageID {
			g.view.Stale = true
		}
		g.mu.Unlock()
		g.root.wake()
		return false
	}
	g.mu.Lock()
	if g.view.PageID == pageID {
		g.view.Suggestion = nil
	}
	g.mu.Unlock()
	g.root.wake()
	g.root.discardDraftFor(pageID)
	g.root.reloadIfSelected(pageID)
	g.root.Refresh()
	return true
}
