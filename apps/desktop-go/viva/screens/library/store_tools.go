package library

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/locales"
)

// 文章工具侧栏背后的异步状态：来源评论（渲染层 SourceCommentsContext 的 useComments）、网页来源版本（WebSourceVersions 的读取与采用）、
// 「复制给 AI」的忙标记（AiHandoffButton 的 isBusy）。
//
// 渲染层里这三块的组件状态都随条目重建（ArticleToolsPanel 与评论 Provider 的 key 含条目 ID），所以这里读到条目之后各自读一次、
// 换条目与清详情时整体重置；toolsGen 是「这一代挂载」的序号：重置会让它加一，旧一代在途的读取与采集回来发现序号变了就整个丢弃，
// 不碰新一代的状态（含「采集中」标记）。展开与选中的版本、评论数量档位这类只属于界面的状态在 Screen 里（detailtools.go）。

// CommentsView 是来源评论的数据状态（useComments 里除 open、limit 之外的部分）。
type CommentsView struct {
	// Supported 是这条能采集来源评论：抖音与小红书的 https 来源，回收站里的不开放。
	Supported bool
	Comments  []services.LibrarySourceComment
	// Reading 是读取已落库的评论在途；Loading 是重新采集在途。
	Reading, Loading bool
	// LoadError 是读取已落库评论失败的原因（卡片里显示带重试的错误态）；Error 是上次重新采集失败的原因（卡片里的红字）。
	LoadError, Error string
	// Attempted 是已经点过采集：空列表的提示据此从「还没有来源评论」换成「未取得评论，可稍后重试」。
	Attempted bool
}

// VersionsView 是网页来源版本的数据状态（WebSourceVersions 的 versions、baseline、error）。
type VersionsView struct {
	// Baseline 是最近一次读到的比较基线（版本之外的字段：此刻的正文、标题、哈希、摘要是否过期、有无待确认版本）；nil 表示还没读到。
	// 它同时带着版本列表（Baseline.Versions），Versions 是同一份的别名，方便界面直接用。
	Baseline *services.LibraryWebVersions
	Versions []services.LibraryWebVersion
	// Error 是读取失败的原因（面板里显示带重试的错误态）；成功读到后清空。失败时保留上一次读到的版本与基线（渲染层同）。
	Error string
}

// ToolsView 是文章工具侧栏背后的状态；ItemID 是它属于哪一条。
type ToolsView struct {
	ItemID string
	// Mount 是这一代挂载的序号：它变了就是渲染层里整个工具侧栏重新挂载（换条目、采用版本后重新读取），界面里随挂载而生的状态要清空。
	Mount    int
	Comments CommentsView
	Versions VersionsView
	// HandoffBusy 是一次「复制给 AI」或「另存为」在途（AiHandoffButton 的 isBusy）：两枚按钮都禁用。
	HandoffBusy bool
	// SummaryGenerating 是 AI 摘要生成（含写回）在途（AiSummaryCard 的 isGenerating）；SummaryExpandSeq 每成功写回一次加一，
	// 卡片据此展开摘要（渲染层 setIsExpanded(true)）：它是「请展开」的事件序号，不是展开状态本身（展开状态属于 Screen）。
	SummaryGenerating bool
	SummaryExpandSeq  int
	// OcrRunning 是图中文字识别（含写回）在途（AiOcrCard 的 isRunning）。
	OcrRunning bool
}

// toolsState 是工具侧栏的内部状态（detailState 里的一份）。
type toolsState struct {
	// gen 是挂载代号；commentsReq 是同一代里评论读取与采集的请求序号（渲染层 request ref，卸载时加一）。
	gen, commentsReq int
	// capturing 是重新采集在途（渲染层 capturing ref）：它不进快照，只用来挡住重复点击。
	capturing bool
	// adopting 是采用版本在途：挡住重复点击（渲染层靠按钮在采用期间整面板重新挂载来挡）。
	adopting bool
	// summarizing、ocrRunning 是摘要生成、图中文字识别在途：挡住重复触发（store_ai.go；渲染层是 isGenerating / isRunning 的早退）。
	summarizing, ocrRunning bool
}

// resetToolsLocked 让上一代作废并清空快照（换条目、清详情）。mu 由调用方持有。
func (s *Store) resetToolsLocked() {
	s.detail.tools = toolsState{gen: s.detail.tools.gen + 1}
	s.view.Detail.Tools = ToolsView{}
}

// describeToolsError 取失败原因的文字：空的换成 fallback。
func describeToolsError(err error, fallback string) string {
	if msg := strings.TrimSpace(err.Error()); msg != "" {
		return msg
	}
	return fallback
}

// initTools 在读到条目之后为它挂载工具侧栏的状态并发起读取：评论只对抖音与小红书、未删除的条目读取（useComments 的 supported），
// 来源版本只对未删除的网页条目读取（侧栏在回收站里不渲染，WebSourceVersions 随之不挂载）。序号变了（又选了别的）就什么也不做。
func (s *Store) initTools(ctx context.Context, id string, seq int) {
	s.mu.Lock()
	item := s.view.Detail.Item
	if seq != s.detail.seq || item == nil || item.ID != id {
		s.mu.Unlock()
		return
	}
	s.resetToolsLocked()
	gen := s.detail.tools.gen
	supported := item.DeletedAt == 0 && services.DetectSourceCommentsPlatform(item.SourceURI) != ""
	web := item.DeletedAt == 0 && item.ItemType == services.LibraryTypeWebpage
	s.view.Detail.Tools = ToolsView{ItemID: id, Mount: gen, Comments: CommentsView{Supported: supported}}
	s.mu.Unlock()
	s.wake()
	if web {
		s.Spawn(func() { s.loadWebVersions(ctx, gen) })
	}
	if supported {
		s.Spawn(func() { s.loadComments(ctx, gen) })
	}
}

// ---- 来源评论 ----

// loadComments 读已落库的评论（useComments 的 load）：开始就把读取中置真、读取失败清空；请求序号让过期的读取不覆盖较新的。
func (s *Store) loadComments(ctx context.Context, gen int) {
	s.mu.Lock()
	t := s.view.Detail.Tools
	if gen != s.detail.tools.gen || !t.Comments.Supported {
		s.mu.Unlock()
		return
	}
	s.detail.tools.commentsReq++
	req := s.detail.tools.commentsReq
	t.Comments.Reading, t.Comments.LoadError = true, ""
	s.view.Detail.Tools = t
	s.mu.Unlock()
	s.wake()

	rows, err := s.svc.SourceComments(ctx, t.ItemID)

	s.mu.Lock()
	if gen != s.detail.tools.gen {
		s.mu.Unlock()
		return
	}
	current := req == s.detail.tools.commentsReq
	if current {
		c := s.view.Detail.Tools
		if err != nil {
			c.Comments.LoadError = err.Error()
		} else {
			c.Comments.Comments = rows
		}
		c.Comments.Reading = false
		s.view.Detail.Tools = c
	}
	s.mu.Unlock()
	if err != nil {
		s.guard.Background("library.sourceComments", fmt.Errorf("读取来源评论失败：%w", err))
	}
	if current {
		s.wake()
	}
}

// RetryComments 重新读已落库的评论（错误态的重试钮）。
func (s *Store) RetryComments(ctx context.Context) {
	s.mu.Lock()
	gen := s.detail.tools.gen
	s.mu.Unlock()
	s.Spawn(func() { s.loadComments(ctx, gen) })
}

// RefreshComments 重新采集评论（useComments 的 refresh）：不支持、采集在途、读取在途、读取失败时什么也不做；
// limit 是评论采集数量档位。开始就清上次的失败原因并记下「试过」；失败给一条带原因的提示并把原因留在卡片里。
func (s *Store) RefreshComments(ctx context.Context, limit int) {
	s.mu.Lock()
	t := s.view.Detail.Tools
	if !t.Comments.Supported || s.detail.tools.capturing || t.Comments.Reading || t.Comments.LoadError != "" {
		s.mu.Unlock()
		return
	}
	gen := s.detail.tools.gen
	s.detail.tools.capturing = true
	s.detail.tools.commentsReq++
	req := s.detail.tools.commentsReq
	t.Comments.Loading, t.Comments.Error, t.Comments.Attempted = true, "", true
	s.view.Detail.Tools = t
	s.mu.Unlock()
	s.wake()

	s.Spawn(func() {
		rows, err := s.svc.RefreshSourceComments(ctx, t.ItemID, limit)

		s.mu.Lock()
		if gen != s.detail.tools.gen {
			s.mu.Unlock()
			return
		}
		current := req == s.detail.tools.commentsReq
		detail := ""
		if current {
			c := s.view.Detail.Tools
			if err != nil {
				detail = err.Error()
				c.Comments.Error = detail
			} else {
				c.Comments.Comments = rows
			}
			c.Comments.Loading = false
			s.view.Detail.Tools = c
		}
		s.detail.tools.capturing = false
		s.mu.Unlock()
		if err != nil && current {
			s.guard.Background("library.refreshComments", fmt.Errorf("采集来源评论失败：%w", err))
			if s.guard.Notify != nil {
				s.guard.Notify.Error(locales.LibraryCommentsCaptureFailed(), detail)
			}
		}
		s.wake()
	})
}

// ---- 网页来源版本 ----

// loadWebVersions 读网页条目的原文版本与比较基线（WebSourceVersions 的 load）：成功清掉错误；失败记下原因、保留已读到的数据。
// 不带请求序号（渲染层也没有）：同一代里后到的结果覆盖先到的。
func (s *Store) loadWebVersions(ctx context.Context, gen int) {
	s.mu.Lock()
	if gen != s.detail.tools.gen {
		s.mu.Unlock()
		return
	}
	id := s.view.Detail.Tools.ItemID
	s.mu.Unlock()

	res, err := s.svc.WebVersions(ctx, id)

	s.mu.Lock()
	if gen != s.detail.tools.gen {
		s.mu.Unlock()
		return
	}
	t := s.view.Detail.Tools
	if err != nil {
		t.Versions.Error = describeToolsError(err, locales.WebCaptureLoadVersionsError())
	} else {
		res.Versions = slices.Clone(res.Versions)
		t.Versions = VersionsView{Baseline: &res, Versions: res.Versions}
	}
	s.view.Detail.Tools = t
	s.mu.Unlock()
	if err != nil {
		s.guard.Background("library.webVersions", fmt.Errorf("读取原文版本失败：%w", err))
	}
	s.wake()
}

// LoadWebVersions 重新读原文版本（展开面板时与错误态的重试钮）。
func (s *Store) LoadWebVersions(ctx context.Context) {
	s.mu.Lock()
	gen := s.detail.tools.gen
	s.mu.Unlock()
	s.Spawn(func() { s.loadWebVersions(ctx, gen) })
}

// AdoptWebVersion 把所选的原文版本采用为正文（WebSourceVersions 的 adopt）：先把当前草稿落盘，落不了就什么也不做
// （渲染层这里直接 return，随后照常重新选中——照原样复刻）；落盘后仍有未保存改动就失败；采用时带上比较时的正文哈希与标题做乐观锁。
// 失败走变更失败通道；成功后重新读取条目（工具侧栏随详情重新挂载，所以不必另读版本）。返回是否成功。
func (s *Store) AdoptWebVersion(ctx context.Context, versionID string) bool {
	s.mu.Lock()
	t := s.view.Detail.Tools
	if s.detail.tools.adopting || t.Versions.Baseline == nil {
		s.mu.Unlock()
		return false
	}
	var version *services.LibraryWebVersion
	for i := range t.Versions.Versions {
		if t.Versions.Versions[i].ID == versionID {
			version = &t.Versions.Versions[i]
			break
		}
	}
	if version == nil {
		s.mu.Unlock()
		return false
	}
	in := services.LibraryAdoptWebVersionInput{
		ItemID: t.ItemID, VersionID: versionID,
		ExpectedContentHash: t.Versions.Baseline.ContentHash, ExpectedTitle: t.Versions.Baseline.Title,
	}
	s.detail.tools.adopting = true
	gen := s.detail.tools.gen
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		if gen == s.detail.tools.gen {
			s.detail.tools.adopting = false
		}
		s.mu.Unlock()
	}()

	ok := s.guard.Run(locales.WebCaptureAdoptVersion(), func() error {
		if !s.FlushPendingSave(ctx) {
			return nil
		}
		if s.View().Detail.Unsaved {
			return errors.New(locales.WebCaptureSaveFirst())
		}
		return s.svc.AdoptWebVersion(ctx, in)
	})
	if ok {
		s.SelectItemWait(ctx, in.ItemID)
	}
	return ok
}

// ---- 交接稿 ----

// RunHandoff 在后台执行一次交接稿任务（复制或另存为），期间 HandoffBusy 为真；已经在途时直接忽略（AiHandoffButton 的 run）。
func (s *Store) RunHandoff(task func()) {
	s.mu.Lock()
	t := s.view.Detail.Tools
	if t.HandoffBusy || t.ItemID == "" {
		s.mu.Unlock()
		return
	}
	gen := s.detail.tools.gen
	t.HandoffBusy = true
	s.view.Detail.Tools = t
	s.mu.Unlock()
	s.wake()
	s.Spawn(func() {
		task()
		s.mu.Lock()
		if gen == s.detail.tools.gen {
			t := s.view.Detail.Tools
			t.HandoffBusy = false
			s.view.Detail.Tools = t
		}
		s.mu.Unlock()
		s.wake()
	})
}
