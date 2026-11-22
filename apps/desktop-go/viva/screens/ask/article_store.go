package ask

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"time"

	"guizhi-mygo-shell/aiflow"
	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/locales"
)

// ArticleView 是「围绕本文」对话每帧读取的快照（写时复制）。
type ArticleView struct {
	SessionID  string
	ItemID     string
	Title      string
	Target     *aiflow.ArticleTarget
	Sessions   []services.AskSessionMeta
	Messages   []aiflow.ArticleMessage
	WebEnabled bool
	// Loading 为 true 时界面显示加载态、不接受提问。
	Loading bool
	Running bool
	// LoadError 非空时界面必须渲染成错误态（带重试 Open），不能当成空对话。
	LoadError string
	// SaveError 非空表示最近一次保存失败：对话还在内存里，界面提示并给出「重试保存」入口（Persist）。
	SaveError string
}

// ArticleStore 是围绕本文提问的状态（article-ask.store.ts）：打开、新对话、联网开关、提问、重答、停止与保存。
type ArticleStore struct {
	p *Store

	// NewUUID 生成消息与会话 ID（对应 crypto.randomUUID）；测试注入固定值。
	NewUUID func() string
	// CheckpointEvery 是流式回答期间落盘的最短间隔（默认 500ms）：长回答中途崩溃也不至于整轮丢失。
	CheckpointEvery time.Duration

	mu   sync.Mutex
	view ArticleView
	run  *askRun
	// generation 是打开 / 新建会话的代次：快速切换时，旧一次加载的结果不能覆盖新一次。
	generation int
	rev        int
	// signatures 是各本文会话最近一次读入或落盘的内容签名：没变就不再保存，否则打开文章会刷新会话的更新时间。
	signatures map[string]string

	// 提问框的草稿与选段（按 `<条目>:<会话或 reader>` 键保存，切走再回来还在）、联网搜索缺失提示与提交中状态。
	drafts        map[string]Draft
	sources       map[string]string
	missingSearch bool
	submitting    bool
}

func newArticleStore(p *Store) *ArticleStore {
	return &ArticleStore{
		p: p, NewUUID: defaultUUID, CheckpointEvery: 500 * time.Millisecond,
		signatures: map[string]string{}, drafts: map[string]Draft{}, sources: map[string]string{},
	}
}

// View 返回当前快照。
func (a *ArticleStore) View() ArticleView {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.view
}

// SessionID 是当前本文会话 ID（没有时为空串）。
func (a *ArticleStore) SessionID() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.view.SessionID
}

func (a *ArticleStore) newUUID() string {
	if a.NewUUID != nil {
		return a.NewUUID()
	}
	return defaultUUID()
}

func (a *ArticleStore) wake() { a.p.wake() }

// sanitizeMessages 按已清除证据的条目给消息脱敏。
func (a *ArticleStore) sanitizeMessages(msgs []aiflow.ArticleMessage) []aiflow.ArticleMessage {
	ids := a.p.ev.IDs()
	if len(ids) == 0 {
		return msgs
	}
	cleaned, err := sanitizeList(a.p.svc, ids, normalizeArticleAll(msgs))
	if err != nil {
		a.p.guard.Background("articleAsk.sanitize", err)
		return msgs
	}
	return cleaned
}

// signature 是保存内容的签名（消息、目标、联网开关与文章标题四项合一）。
func signature(messagesJSON string, target *aiflow.ArticleTarget, web bool, title string) string {
	raw, _ := json.Marshal([]any{messagesJSON, target, web, title})
	return string(raw)
}

// ---- 保存 ----

// Persist 保存当前本文会话（没有消息或没有目标时直接成功；内容没变不重复保存）。
// 失败时经 kit.Guard 弹出「保存本文问答失败」并设置 SaveError，返回 false；再调一次即重试。
func (a *ArticleStore) Persist() bool {
	a.mu.Lock()
	v := a.view
	a.mu.Unlock()
	if v.SessionID == "" || v.Target == nil || len(v.Messages) == 0 {
		return true
	}
	body, err := marshalArticleMessages(v.Messages)
	if err != nil {
		return a.saveFailed(v.SessionID, err)
	}
	in := services.SaveAskSessionInput{
		ID: v.SessionID, Scope: services.AskScopeArticle, ItemID: v.ItemID, ArticleTitle: v.Title,
		Title: buildArticleTitle(v.Messages), MessagesJSON: body, Target: v.Target, WebEnabled: &v.WebEnabled,
	}
	sig := signature(body, v.Target, v.WebEnabled, v.Title)
	a.mu.Lock()
	unchanged := a.signatures[in.ID] == sig
	a.mu.Unlock()
	if unchanged {
		return true
	}
	ok := false
	saves.do(in.ID, func() {
		ok = a.p.guard.Run(locales.ArticleAskSave(), func() error {
			saved, err := a.p.svc.SaveSession(a.p.ctx(), in)
			if err != nil {
				return err
			}
			a.mu.Lock()
			a.signatures[in.ID] = sig
			if a.view.SessionID == in.ID {
				rest := slices.DeleteFunc(slices.Clone(a.view.Sessions), func(m services.AskSessionMeta) bool { return m.ID == saved.ID })
				a.view.Sessions = append([]services.AskSessionMeta{saved.AskSessionMeta}, rest...)
				a.view.SaveError = ""
			}
			a.mu.Unlock()
			return nil
		})
	})
	if !ok {
		a.mu.Lock()
		if a.view.SessionID == in.ID {
			a.view.SaveError = locales.ArticleAskNotSaved()
		}
		a.mu.Unlock()
		a.wake()
		return false
	}
	a.wake()
	a.p.fireSaved()
	return true
}

// saveFailed 记录序列化之类的内部失败（不经服务调用，不会弹提示）。
func (a *ArticleStore) saveFailed(sessionID string, err error) bool {
	a.p.guard.Background("articleAsk.save", err)
	a.mu.Lock()
	if a.view.SessionID == sessionID {
		a.view.SaveError = locales.ArticleAskNotSaved()
	}
	a.mu.Unlock()
	a.wake()
	return false
}

// ---- 打开 / 新建 ----

// Open 打开本文的会话：sessionID 为空取该文章最近一次会话，没有就开一个空会话。
// 已经打开同一篇文章（同一会话）且没出错时什么也不做。失败设置 LoadError，再调一次 Open 即重试。
func (a *ArticleStore) Open(target aiflow.ArticleTarget, sessionID string) {
	a.mu.Lock()
	v := a.view
	if v.ItemID == target.ItemID && v.SessionID != "" && (sessionID == "" || sessionID == v.SessionID) && v.LoadError == "" {
		a.mu.Unlock()
		return
	}
	a.generation++
	gen := a.generation
	a.mu.Unlock()

	a.Stop()
	if !a.Persist() || !a.isGen(gen) {
		return
	}
	a.mu.Lock()
	a.view.Loading, a.view.LoadError = true, ""
	a.mu.Unlock()
	a.wake()

	if err := a.load(target, sessionID, gen); err != nil && a.isGen(gen) {
		a.mu.Lock()
		a.view.Loading, a.view.LoadError = false, err.Error()
		a.mu.Unlock()
		a.p.guard.Background("articleAsk.load", err)
		a.wake()
	}
}

func (a *ArticleStore) isGen(gen int) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return gen == a.generation
}

func (a *ArticleStore) load(target aiflow.ArticleTarget, sessionID string, gen int) error {
	ctx := a.p.ctx()
	sessions, err := a.p.svc.ListSessions(ctx, services.AskSessionFilter{Scope: services.AskScopeArticle, ItemID: target.ItemID})
	if err != nil {
		return err
	}
	id := sessionID
	if id == "" && len(sessions) > 0 {
		id = sessions[0].ID
	}
	var record *services.AskSessionRecord
	if id != "" {
		if record, err = a.p.svc.GetSession(ctx, id); err != nil {
			return err
		}
		if record == nil || !record.IsArticle() || record.ItemID != target.ItemID {
			return errors.New("本文会话不存在或关联不一致")
		}
	}
	item, found, err := a.p.svc.ItemInfo(ctx, target.ItemID)
	if err != nil {
		return err
	}
	if !a.isGen(gen) {
		return nil
	}
	var (
		messages []aiflow.ArticleMessage
		next     = cloneTarget(target)
		web      = true // record?.webEnabled !== false：没有会话记录时按开启处理
		title    string
	)
	if record != nil {
		if messages, err = parseArticleMessages(record.MessagesJSON); err != nil {
			return err
		}
		messages = a.sanitizeMessages(messages)
		if record.Target != nil {
			next = record.Target
		}
		web = record.WebEnabledOrDefault()
	}
	switch {
	case found:
		title = item.Title
	case record != nil && record.ArticleTitle != "":
		title = record.ArticleTitle
	default:
		title = locales.ArticleAskArticleDeleted()
	}
	newID := a.newUUID()
	if record != nil {
		newID = record.ID
	}
	a.mu.Lock()
	if gen != a.generation {
		a.mu.Unlock()
		return nil
	}
	a.view = ArticleView{
		SessionID: newID, ItemID: target.ItemID, Target: sanitizeTarget(a.p.ev, next), Title: title, Sessions: sessions,
		Messages: messages, WebEnabled: web,
	}
	a.rev++
	if record != nil && len(messages) > 0 {
		if body, err := marshalArticleMessages(messages); err == nil {
			articleTitle := record.ArticleTitle
			if articleTitle == "" {
				articleTitle = title
			}
			a.signatures[newID] = signature(body, a.view.Target, web, articleTitle)
		}
	}
	a.mu.Unlock()
	a.wake()
	return nil
}

// NewSession 在同一篇文章上开一个新对话：先停止并保存当前对话；联网开关默认关闭。
func (a *ArticleStore) NewSession(target aiflow.ArticleTarget) {
	a.Stop()
	if !a.Persist() {
		return
	}
	a.mu.Lock()
	a.generation++
	v := a.view
	a.view = ArticleView{
		SessionID: a.newUUID(), ItemID: target.ItemID, Target: cloneTarget(target), Title: v.Title,
		Sessions: v.Sessions,
	}
	a.rev++
	a.mu.Unlock()
	a.wake()
}

// SetWeb 切换联网查证开关，并在后台保存（开关属于会话内容）。
func (a *ArticleStore) SetWeb(enabled bool) {
	a.mu.Lock()
	a.view.WebEnabled = enabled
	a.mu.Unlock()
	a.wake()
	a.p.spawn(func() { a.Persist() })
}

// ---- 提问 ----

// Stop 停止进行中的回答：取消请求，把 running 的轮次标成「已停止」，并在后台保存。
func (a *ArticleStore) Stop() {
	a.mu.Lock()
	run := a.run
	if run == nil {
		a.mu.Unlock()
		return
	}
	a.run = nil
	run.cancel()
	a.view.Running = false
	msgs := slices.Clone(a.view.Messages)
	for i, m := range msgs {
		if m.Status == services.AskStatusRunning {
			stopped := locales.AskStopped()
			m.Status, m.Error, m.Step = services.AskStatusError, &stopped, nil
			msgs[i] = m
		}
	}
	a.view.Messages = msgs
	a.rev++
	a.mu.Unlock()
	a.wake()
	a.p.spawn(func() { a.Persist() })
}

// applyPatch 把补丁里被设置的字段写进消息（ArticlePatch 对应 Partial<ArticleMessage>）。
func applyPatch(m aiflow.ArticleMessage, p aiflow.ArticlePatch) aiflow.ArticleMessage {
	if p.Step != nil {
		m.Step = copyOf(p.Step)
	} else if p.ClearStep {
		m.Step = nil
	}
	if p.Context != nil {
		m.Context = p.Context
	}
	if p.Sources != nil {
		m.Sources = p.Sources
	}
	if p.Warnings != nil {
		m.Warnings = p.Warnings
	}
	if p.WebStatus != nil {
		m.WebStatus = *p.WebStatus
	}
	if p.Answer != nil {
		m.Answer = *p.Answer
	}
	if p.Status != nil {
		m.Status = *p.Status
	}
	if p.Model != nil {
		m.Model = copyOf(p.Model)
	}
	if p.Truncated != nil {
		m.Truncated = copyOf(p.Truncated)
	}
	return m
}

// sanitizePatch 按证据清除记录给补丁里的来源与上下文脱敏（只处理携带证据的字段；回答文本原样）。
func (a *ArticleStore) sanitizePatch(p aiflow.ArticlePatch) aiflow.ArticlePatch {
	ids := a.p.ev.IDs()
	if len(ids) == 0 || (p.Sources == nil && p.Context == nil) {
		return p
	}
	probe := aiflow.ArticleMessage{Context: p.Context, Sources: p.Sources}
	cleaned, err := sanitizeList(a.p.svc, ids, normalizeArticleAll([]aiflow.ArticleMessage{probe}))
	if err != nil {
		a.p.guard.Background("articleAsk.sanitize", err)
		return p
	}
	if p.Sources != nil {
		p.Sources = cleaned[0].Sources
	}
	if p.Context != nil {
		p.Context = cleaned[0].Context
	}
	return p
}

// patchMessage 把补丁写进消息 id；这一轮已失效（停止、换会话）时丢弃，返回是否写入。
func (a *ArticleStore) patchMessage(run *askRun, sessionID, id string, p aiflow.ArticlePatch) bool {
	p = a.sanitizePatch(p)
	return a.update(run, sessionID, id, func(m aiflow.ArticleMessage) aiflow.ArticleMessage { return applyPatch(m, p) })
}

// update 改写消息 id；这一轮已失效（停止、换会话）时丢弃，返回是否写入。
func (a *ArticleStore) update(run *askRun, sessionID, id string, mutate func(m aiflow.ArticleMessage) aiflow.ArticleMessage) bool {
	a.mu.Lock()
	if a.run != run || a.view.SessionID != sessionID || run.ctx.Err() != nil {
		a.mu.Unlock()
		return false
	}
	at := slices.IndexFunc(a.view.Messages, func(m aiflow.ArticleMessage) bool { return m.ID == id })
	if at < 0 {
		a.mu.Unlock()
		return false
	}
	msgs := slices.Clone(a.view.Messages)
	msgs[at] = mutate(msgs[at])
	a.view.Messages = msgs
	a.rev++
	a.mu.Unlock()
	a.wake()
	return true
}

// Ask 围绕本文提一个问题并阻塞到这一轮结束，返回这一轮是否成功。
// 正在回答、正在加载、加载出错、问题为空或目标不是当前文章时直接返回 false。saved 非 nil 表示重试：沿用保存的原始上下文。
func (a *ArticleStore) Ask(question string, target aiflow.ArticleTarget, saved *aiflow.ArticleContext) bool {
	trimmed := jsTrim(question)
	a.mu.Lock()
	v := a.view
	if v.Running || v.Loading || v.LoadError != "" || trimmed == "" || v.ItemID != target.ItemID {
		a.mu.Unlock()
		return false
	}
	sessionID, id := v.SessionID, a.newUUID()
	runCtx, cancel := context.WithCancel(a.p.ctx())
	defer cancel()
	run := &askRun{cancel: cancel, ctx: runCtx}
	a.run = run
	history := slices.Clone(v.Messages)
	message := aiflow.ArticleMessage{
		ID: id, Question: trimmed, Status: services.AskStatusRunning, Sources: []aiflow.ArticleSource{}, Warnings: []string{},
		WebEnabled: v.WebEnabled, WebStatus: "off",
	}
	if v.WebEnabled {
		message.WebStatus = "searching"
	}
	a.view.Target = sanitizeTarget(a.p.ev, cloneTarget(target))
	a.view.Messages = append(slices.Clone(history), message)
	a.view.Running = true
	web := v.WebEnabled
	a.rev++
	a.mu.Unlock()
	a.wake()

	if !a.Persist() {
		a.Stop()
		return false
	}

	// 流式期间的存档节流：拿到上下文必存，回答文本每 CheckpointEvery 存一次。
	var (
		cpMu           sync.Mutex
		lastCheckpoint time.Time
	)
	patch := func(p aiflow.ArticlePatch) {
		if !a.patchMessage(run, sessionID, id, p) {
			return
		}
		due := p.Context != nil
		if p.Answer != nil {
			cpMu.Lock()
			if time.Since(lastCheckpoint) >= a.CheckpointEvery {
				lastCheckpoint, due = time.Now(), true
			}
			cpMu.Unlock()
		}
		if due {
			a.p.spawn(func() { a.Persist() })
		}
	}
	req := services.ArticleAskRequest{Target: target, Question: trimmed, History: history, WebEnabled: web, RequestID: id, Context: saved}
	err := a.p.svc.AskArticle(runCtx, req, patch)
	ok := err == nil
	if err != nil {
		stopped := runCtx.Err() != nil || errors.Is(err, context.Canceled)
		text := locales.AskStopped()
		if !stopped {
			text = err.Error()
			a.p.guard.Background("articleAsk.answer", err)
		}
		// 停止时 Stop 已经写过「已停止」，这里因 run 失效被丢弃
		a.update(run, sessionID, id, func(m aiflow.ArticleMessage) aiflow.ArticleMessage {
			m.Status, m.Error, m.Step = services.AskStatusError, &text, nil
			return m
		})
	}

	a.mu.Lock()
	still := a.run == run && a.view.SessionID == sessionID
	if still {
		a.run = nil
		a.view.Running = false
	}
	a.mu.Unlock()
	if still {
		a.wake()
		a.Persist()
	}
	return ok
}

// Retry 重新回答某一轮：丢弃它及其之后的消息，沿用当时的上下文与联网开关重跑。
func (a *ArticleStore) Retry(id string) bool {
	a.mu.Lock()
	v := a.view
	if v.Running {
		a.mu.Unlock()
		return false
	}
	at := slices.IndexFunc(v.Messages, func(m aiflow.ArticleMessage) bool { return m.ID == id })
	if at < 0 || v.Target == nil {
		a.mu.Unlock()
		return false
	}
	message := v.Messages[at]
	a.view.Messages = slices.Clone(v.Messages[:at])
	a.view.WebEnabled = message.WebEnabled
	a.rev++
	target := *v.Target
	if message.Context != nil {
		target = message.Context.Target
	}
	a.mu.Unlock()
	a.wake()
	return a.Ask(message.Question, target, message.Context)
}

// forget 在会话被删除后清掉对应的内存状态。
func (a *ArticleStore) forget(id string) {
	a.mu.Lock()
	delete(a.signatures, id)
	if a.view.SessionID == id {
		a.view.SessionID, a.view.ItemID, a.view.Messages = "", "", nil
		a.rev++
	}
	a.mu.Unlock()
}

// onEvidenceCleared 在证据清除登记之后重新脱敏内存里的消息与目标（同全局问答，脱敏不在锁内做）。
func (a *ArticleStore) onEvidenceCleared() {
	for {
		a.mu.Lock()
		msgs, rev := a.view.Messages, a.rev
		a.mu.Unlock()
		cleaned := a.sanitizeMessages(msgs)
		a.mu.Lock()
		if a.rev != rev {
			a.mu.Unlock()
			continue
		}
		a.view.Messages = cleaned
		a.view.Target = sanitizeTarget(a.p.ev, a.view.Target)
		a.rev++
		a.mu.Unlock()
		break
	}
	a.wake()
}