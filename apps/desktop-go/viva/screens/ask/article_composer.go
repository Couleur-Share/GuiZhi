package ask

import (
	"errors"
	"slices"

	"guizhi-mygo-shell/aiflow"
	"guizhi-mygo-shell/viva/locales"
)

// 围绕本文的提问框状态（ArticleConversation.tsx 里的草稿、选段、联网缺失提示与发送流程）。
// 草稿与选段按 DraftKey 保存在 ArticleStore 里：切到别的文章或会话再回来，写了一半的问题还在。

// Draft 是提问框里写了一半的内容：问题文本与引用的选段（nil 表示没有引用）。
type Draft struct {
	Text  string
	Quote *aiflow.ArticleTarget
}

// DraftKey 是草稿的键：同一篇文章的阅读器侧栏（sessionID 为空）与每个历史会话各有一份。
func DraftKey(itemID, sessionID string) string {
	if sessionID == "" {
		sessionID = "reader"
	}
	return itemID + ":" + sessionID
}

// Draft 读取草稿。
func (a *ArticleStore) Draft(key string) Draft {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.drafts[key]
}

// SetDraft 更新草稿文本，保留引用的选段。
func (a *ArticleStore) SetDraft(key, text string) {
	a.mu.Lock()
	d := a.drafts[key]
	d.Text = text
	a.drafts[key] = d
	a.mu.Unlock()
	a.wake()
}

// SetQuote 更新引用的选段（nil 清除），保留草稿文本。
func (a *ArticleStore) SetQuote(key string, quote *aiflow.ArticleTarget) {
	a.mu.Lock()
	d := a.drafts[key]
	d.Quote = quote
	a.drafts[key] = d
	a.mu.Unlock()
	a.wake()
}

// AcceptSelection 在阅读器里选中一段并点「提问」时调用：目标带选段就把提示语写进草稿、把选段挂成引用。
func (a *ArticleStore) AcceptSelection(key string, target aiflow.ArticleTarget, prompt string) {
	if target.Selection == nil {
		return
	}
	a.mu.Lock()
	a.drafts[key] = Draft{Text: prompt, Quote: cloneTarget(target)}
	a.mu.Unlock()
	a.wake()
}

// SyncSource 在阅读来源（视图或版本）变化时调用：引用的选段属于旧来源，必须清掉。首次看到某个键只记录。
func (a *ArticleStore) SyncSource(key string, target aiflow.ArticleTarget) {
	version := ""
	if target.VersionID != nil {
		version = *target.VersionID
	}
	now := target.View + ":" + version
	a.mu.Lock()
	prev, seen := a.sources[key]
	a.sources[key] = now
	changed := seen && prev != now
	if changed {
		d := a.drafts[key]
		d.Quote = nil
		a.drafts[key] = d
	}
	a.mu.Unlock()
	if changed {
		a.wake()
	}
}

// MissingSearch 报告是否该显示「尚未配置联网搜索，问题已保留」提示。
func (a *ArticleStore) MissingSearch() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.missingSearch
}

// Submitting 报告是否有一次发送正在进行（输入框要禁用）。
func (a *ArticleStore) Submitting() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.submitting
}

// DismissMissingSearch 收起联网缺失提示。
func (a *ArticleStore) DismissMissingSearch() {
	a.mu.Lock()
	a.missingSearch = false
	a.mu.Unlock()
	a.wake()
}

// SendOptions 是一次发送的附加参数。
type SendOptions struct {
	// Offline 为 true 表示「关闭联网后发送」：先把联网开关关掉再发。
	Offline bool
	// ResolveTarget 非 nil 时在发送前取阅读器此刻的目标（选区、版本可能已变）；返回错误按发送失败处理。
	ResolveTarget func() (aiflow.ArticleTarget, error)
	// OnSubmitted 在问题已被接收、草稿已清空时调用（界面收起选段工具之类）。
	OnSubmitted func()
}

// Send 发送草稿并阻塞到这一轮结束，返回是否成功发出并答完。
//   - 草稿为空或已有一次发送在进行：忽略。
//   - 没有文本模型：提示「尚未配置 AI 服务」并跳到设置的 AI 分区，草稿保留。
//   - 联网开启但搜索没配置：显示 MissingSearch 提示，草稿保留；用户可以去配置或 Offline 重发。
//   - 引用的选段与此刻的阅读来源不一致：失败，提示「阅读来源已切换」。
//   - 提问没有产生任何一轮（未能发出）时把问题放回草稿，不丢用户写的字。
//
// 失败经 kit.Guard 弹出「提问失败」，原因进详情。
func (a *ArticleStore) Send(key string, target aiflow.ArticleTarget, opts SendOptions) bool {
	a.mu.Lock()
	draft := a.drafts[key]
	if jsTrim(draft.Text) == "" || a.submitting {
		a.mu.Unlock()
		return false
	}
	a.submitting = true
	a.mu.Unlock()
	a.wake()
	defer func() {
		a.mu.Lock()
		a.submitting = false
		a.mu.Unlock()
		a.wake()
	}()

	fail := func(err error) bool {
		return a.p.guard.Fail(locales.ArticleAskSendFailed(), "articleAsk.send", err)
	}
	configured, err := a.p.svc.QAConfigured(a.p.ctx())
	if err != nil {
		return fail(err)
	}
	if !configured {
		a.p.guard.Reject(locales.AskNotConfigured())
		if a.p.OnOpenSettingsAI != nil {
			a.p.OnOpenSettingsAI()
		}
		return false
	}
	if a.View().WebEnabled && !opts.Offline {
		searchOK, err := a.p.svc.WebSearchConfigured(a.p.ctx())
		if err != nil {
			return fail(err)
		}
		if !searchOK {
			a.mu.Lock()
			a.missingSearch = true
			a.mu.Unlock()
			a.wake()
			return false
		}
	}
	if opts.Offline {
		a.SetWeb(false)
	}
	current := target
	if opts.ResolveTarget != nil {
		if current, err = opts.ResolveTarget(); err != nil {
			return fail(err)
		}
	}
	questionTarget := *cloneTarget(current)
	if q := draft.Quote; q != nil {
		questionTarget.Selection, questionTarget.Floor = copyOf(q.Selection), copyOf(q.Floor)
		if q.View != current.View || !samePtr(q.VersionID, current.VersionID) {
			return fail(errors.New("阅读来源已切换，请重新选择段落"))
		}
	} else {
		questionTarget.Selection = nil
	}

	question := draft.Text
	a.mu.Lock()
	a.drafts[key] = Draft{}
	a.missingSearch = false
	a.mu.Unlock()
	if opts.OnSubmitted != nil {
		opts.OnSubmitted()
	}
	ok := a.Ask(question, questionTarget, nil)
	if !ok && !slices.ContainsFunc(a.View().Messages, func(m aiflow.ArticleMessage) bool { return m.Question == question }) {
		a.SetDraft(key, question)
	}
	return ok
}

func samePtr(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// ---- 全局这一侧的会话衔接 ----

// OpenFromHistory 从本文历史里打开一个会话：成功才返回 true（调用方据此清空草稿、收起历史、通知全局这边换会话）。
func (a *ArticleStore) OpenFromHistory(target aiflow.ArticleTarget, id string) bool {
	a.Open(target, id)
	v := a.View()
	return v.SaveError == "" && v.LoadError == "" && v.SessionID == id
}

// NewConversation 是面板上的「新对话」：清掉当前草稿与选段，开新会话，返回新会话 ID（调用方据此通知全局那边）。
// 开不成（保存当前对话失败）时返回空串。
func (a *ArticleStore) NewConversation(key string, target aiflow.ArticleTarget) string {
	a.SetDraft(key, "")
	a.SetQuote(key, nil)
	before := a.SessionID()
	a.NewSession(target)
	id := a.SessionID()
	if id == before {
		return ""
	}
	return id
}

// Close 是面板的「收起」：停止进行中的回答并保存，保存成功才算收起（返回 true）。
func (a *ArticleStore) Close() bool {
	a.Stop()
	return a.Persist()
}