package ask

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"time"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// View 是全局问答界面每帧读取的快照：切片都是写时复制的，拿到之后不会被后台改动。
type View struct {
	// Sessions 是历史会话（最近在前）；ArticleSession 非 nil 表示当前活跃会话是「围绕本文」的会话，Messages 此时为空，
	// 对话内容在 Store.Article() 的状态里。
	Sessions        []services.AskSessionMeta
	ActiveSessionID string
	Messages        []services.AskMessage
	ArticleSession  *services.AskSessionMeta
	IsRunning       bool

	// HasLoaded 为首次加载是否已结束（无论成败）。界面不能拿 len(Messages)==0 直接判空态：读会话是异步的，
	// 有历史会话的用户会先看到一整屏空态引导，再被消息列表整块换掉。
	HasLoaded bool
	// LoadError 非空时界面必须渲染成错误态（带重试），不能当作空态。
	LoadError string
	// SaveError 非空表示最近一次保存失败：对话还在内存里，界面提示「尚未保存」并给出重试入口（Store.Persist）。
	SaveError string
}

// Store 是全局问答的状态（ask.store.ts）：会话列表与活跃会话、流式提问、停止、重答、删除单轮、切换与删除会话。
// 它同时是问答模块的根：持有本文问答（Article）与共享的证据清除记录。
//
// 方法都是同步的、可从任意 goroutine 调用；界面线程只读 View。变化后调用 invalidate 请求重绘。
type Store struct {
	svc        services.Ask
	guard      kit.Guard
	invalidate func()

	// Prefs 记住上次活跃会话（nil 时只在内存里）。
	Prefs services.Prefs
	// Ctx 是发给服务的请求的根上下文；nil 取 context.Background()。
	Ctx context.Context
	// Spawn 在后台执行一个动作（「触发就不管」的保存）；默认开 goroutine，测试注入同步执行以保持确定性。
	Spawn func(fn func())
	// NewID 生成消息与会话 ID（前缀 ask / session）；Now 给本文问答的存档节流用。测试注入固定值。
	NewID func(prefix string) string
	Now   func() time.Time

	// 以下钩子由 Root 接线，全部可空。
	// OnSaved 在会话保存或删除之后调用：历史列表与侧栏要刷新（渲染层的 "article-ask-saved" 事件）。
	OnSaved func()
	// OnOpenArticle 打开文章：切到知识库模块并选中它（GlobalArticleConversation 的「返回文章」）。
	OnOpenArticle func(itemID string)
	// OnOpenSettingsAI 打开设置的 AI 分区（模型未配置时的引导）。
	OnOpenSettingsAI func()

	article *ArticleStore
	ev      *Evidence

	mu   sync.Mutex
	view View
	// run 是进行中的提问，nil 表示没有（abortController）。以指针相等判断「这轮是否已被替换或停止」。
	run *askRun
	// generation 是会话切换代次：快速连点切换时，旧一轮的结果不能覆盖新一轮（sessionGeneration）。
	generation int
	// initialized 是首次加载是否已发起（失败后复位，允许重试）。
	initialized bool
	// failedSessionID 是上次切换失败的会话，重试加载时再切一次。
	failedSessionID string
	// persisted 是各会话最近一次读入或落盘的消息快照：内容没变就不再保存，否则切换会话会刷新旧会话的更新时间。
	persisted map[string]string
	// rev 在消息列表每次变化时加一，给「这期间有没有别的改动」的判断用（JS 里是比较数组引用）。
	rev int
}

type askRun struct {
	cancel context.CancelFunc
	ctx    context.Context
}

// NewStore 创建全局问答状态。svc 不能为空。
func NewStore(svc services.Ask, guard kit.Guard, invalidate func()) *Store {
	s := &Store{
		svc: svc, guard: guard, invalidate: invalidate,
		Spawn:     func(fn func()) { go fn() },
		NewID:     defaultID,
		Now:       time.Now,
		ev:        &Evidence{},
		persisted: map[string]string{},
	}
	s.article = newArticleStore(s)
	s.ev.OnCleared(s.onEvidenceCleared)
	s.ev.OnCleared(s.article.onEvidenceCleared)
	return s
}

// Article 返回本文问答的状态。
func (s *Store) Article() *ArticleStore { return s.article }

// View 返回当前快照。
func (s *Store) View() View {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.view
}

func (s *Store) wake() {
	if s.invalidate != nil {
		s.invalidate()
	}
}

func (s *Store) ctx() context.Context {
	if s.Ctx != nil {
		return s.Ctx
	}
	return context.Background()
}

func (s *Store) newID(prefix string) string {
	if s.NewID != nil {
		return s.NewID(prefix)
	}
	return defaultID(prefix)
}

func (s *Store) spawn(fn func()) {
	if s.Spawn != nil {
		s.Spawn(fn)
		return
	}
	go fn()
}

func (s *Store) fireSaved() {
	if s.OnSaved != nil {
		s.OnSaved()
	}
}

// remember 记住活跃会话；偏好写失败只留日志（对应 localStorage 不可用时静默降级）。
func (s *Store) remember(id string) {
	if s.Prefs == nil {
		return
	}
	if err := s.Prefs.Set(PrefActiveSession, id); err != nil {
		s.guard.Background("ask.rememberSession", err)
	}
}

func (s *Store) remembered() string {
	if s.Prefs == nil {
		return ""
	}
	raw, ok := s.Prefs.Get(PrefActiveSession)
	if !ok {
		return ""
	}
	var id string
	if json.Unmarshal(raw, &id) != nil {
		return ""
	}
	return id
}

// sanitizeMessages 按已清除证据的条目给消息脱敏；脱敏失败时记日志并保持原样（输入是自己刚序列化的 JSON，实际不会失败）。
func (s *Store) sanitizeMessages(msgs []services.AskMessage) []services.AskMessage {
	ids := s.ev.IDs()
	if len(ids) == 0 {
		return msgs
	}
	cleaned, err := sanitizeList(s.svc, ids, normalizeAskAll(msgs))
	if err != nil {
		s.guard.Background("ask.sanitize", err)
		return msgs
	}
	return cleaned
}

// ---- 加载 ----

// Initialize 加载会话列表并恢复上次活跃会话（AskWorkspace / 侧栏挂载时调用）。重复调用无副作用；失败后可再调（RetryLoad）。
func (s *Store) Initialize() {
	s.mu.Lock()
	if s.initialized {
		s.mu.Unlock()
		return
	}
	s.initialized = true
	s.view.LoadError = ""
	s.mu.Unlock()
	s.wake()

	err := s.loadInitial()
	s.mu.Lock()
	if err != nil {
		s.initialized = false
		s.view.LoadError = err.Error()
	}
	// 失败也要放行：否则界面会一直停在加载态，用户连空态引导都看不到
	s.view.HasLoaded = true
	s.mu.Unlock()
	if err != nil {
		s.guard.Background("ask.initialize", err)
	}
	s.wake()
}

func (s *Store) loadInitial() error {
	s.mu.Lock()
	gen := s.generation
	s.mu.Unlock()
	page, err := s.svc.QuerySessions(s.ctx(), services.AskSessionQuery{Limit: 50})
	if err != nil {
		return err
	}
	var (
		activeID       string
		messages       []services.AskMessage
		articleSession *services.AskSessionMeta
	)
	if remembered := s.remembered(); remembered != "" {
		record, err := s.svc.GetSession(s.ctx(), remembered)
		if err != nil {
			return err
		}
		if record != nil {
			activeID = record.ID
			if record.IsArticle() {
				meta := record.AskSessionMeta
				articleSession = &meta
			} else {
				if messages, err = s.readMessages(record.MessagesJSON); err != nil {
					return err
				}
				if err := s.rememberPersisted(record.ID, messages); err != nil {
					return err
				}
			}
		}
	}
	fresh := activeID == ""
	if fresh {
		activeID = s.newID("session")
	}
	s.mu.Lock()
	if s.generation != gen || s.view.ActiveSessionID != "" {
		// 读取期间用户已经切到别的会话或新建了会话（例如导入结果里的「就此提问」）：只补上历史列表，
		// 不能把活跃会话改回上次的那个。
		// 光比代次不够：NewSession 与 SwitchSession 先 beginSwitch（代次加一）、后落地活跃会话，两步之间
		// 开始的首次加载拿到的是已加一的代次，却在对方落地之后才返回，会把新建的空会话盖回上次会话。
		// 所以首次加载只在还没有任何活跃会话时才有权落地；已有活跃会话说明用户的选择在前。
		s.view.Sessions = page.Entries
		s.mu.Unlock()
		return nil
	}
	s.view.Sessions, s.view.ActiveSessionID, s.view.Messages, s.view.ArticleSession = page.Entries, activeID, messages, articleSession
	s.rev++
	s.mu.Unlock()
	if fresh {
		s.remember(activeID)
	}
	return nil
}

// readMessages 解析并脱敏一个会话的消息。
func (s *Store) readMessages(messagesJSON string) ([]services.AskMessage, error) {
	msgs, err := parseStoredMessages(messagesJSON, s.newID)
	if err != nil {
		return nil, err
	}
	return s.sanitizeMessages(msgs), nil
}

func (s *Store) rememberPersisted(id string, msgs []services.AskMessage) error {
	body, err := marshalAskMessages(msgs)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.persisted[id] = body
	s.mu.Unlock()
	return nil
}

// RetryLoad 重试加载：上次是切换某个会话失败就再切一次，否则重做首次加载。
func (s *Store) RetryLoad() {
	s.mu.Lock()
	failed := s.failedSessionID
	if failed == "" {
		s.initialized = false
	}
	s.mu.Unlock()
	if failed != "" {
		s.SwitchSession(failed)
		return
	}
	s.Initialize()
}

// RefreshSessions 重读历史会话列表（后台失败只记日志）。
func (s *Store) RefreshSessions() {
	sessions, err := s.svc.ListSessions(s.ctx(), services.AskSessionFilter{})
	if err != nil {
		s.guard.Background("ask.refreshSessions", err)
		return
	}
	s.mu.Lock()
	s.view.Sessions = sessions
	s.mu.Unlock()
	s.wake()
}

// ---- 保存 ----

// Persist 保存活跃会话（有消息才落盘，空会话不进数据库；内容没变不重复保存）。失败时设置 SaveError 并返回 false，
// 界面据此给出「重试保存」入口——再调一次 Persist 即重试。本文会话交给本文问答保存。
func (s *Store) Persist() bool { return s.persistActive() }

func (s *Store) persistActive() bool {
	s.mu.Lock()
	isArticle := s.view.ArticleSession != nil
	id, msgs := s.view.ActiveSessionID, s.view.Messages
	s.mu.Unlock()
	if isArticle {
		return s.article.Persist()
	}
	if id == "" || len(msgs) == 0 {
		return true
	}
	body, err := marshalAskMessages(msgs)
	if err != nil {
		return s.saveFailed(err)
	}
	s.mu.Lock()
	unchanged := s.persisted[id] == body
	s.mu.Unlock()
	if unchanged {
		return true
	}
	input := services.SaveAskSessionInput{ID: id, Title: buildSessionTitle(msgs), MessagesJSON: body}
	var (
		saved   services.AskSessionRecord
		saveErr error
	)
	saves.do(id, func() { saved, saveErr = s.svc.SaveSession(s.ctx(), input) })
	if saveErr != nil {
		return s.saveFailed(saveErr)
	}
	s.mu.Lock()
	s.persisted[id] = body
	rest := slices.DeleteFunc(slices.Clone(s.view.Sessions), func(m services.AskSessionMeta) bool { return m.ID == saved.ID })
	s.view.Sessions = append([]services.AskSessionMeta{saved.AskSessionMeta}, rest...)
	s.view.SaveError = ""
	s.mu.Unlock()
	s.wake()
	s.fireSaved()
	return true
}

func (s *Store) saveFailed(err error) bool {
	s.mu.Lock()
	s.view.SaveError = err.Error()
	s.mu.Unlock()
	s.guard.Background("ask.save", err)
	s.wake()
	return false
}

// ---- 会话 ----

// NewSession 开一个新会话：先停掉进行中的回答并保存当前会话；当前会话为空时直接复用，避免制造一堆空会话。
func (s *Store) NewSession() {
	gen := s.beginSwitch()
	if !s.persistActive() || !s.isCurrentGen(gen) {
		return
	}
	s.mu.Lock()
	if s.view.ArticleSession != nil {
		s.view.ArticleSession, s.view.ActiveSessionID = nil, ""
	}
	if len(s.view.Messages) == 0 && s.view.ActiveSessionID != "" {
		s.mu.Unlock()
		s.wake()
		return
	}
	s.mu.Unlock()
	id := s.newID("session")
	s.remember(id)
	s.mu.Lock()
	s.view.ActiveSessionID, s.view.Messages, s.view.IsRunning, s.view.LoadError = id, nil, false, ""
	s.rev++
	s.mu.Unlock()
	s.wake()
}

// beginSwitch 开始一次会话切换：取新代次，停掉进行中的全局与本文回答。
func (s *Store) beginSwitch() int {
	s.mu.Lock()
	s.generation++
	gen := s.generation
	s.mu.Unlock()
	s.Stop()
	s.article.Stop()
	return gen
}

func (s *Store) isCurrentGen(gen int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return gen == s.generation
}

// SwitchSession 切换到另一个会话：先保存当前会话；读到的记录先完整解析再切换，损坏的记录不会变成可覆盖的空会话。
// 失败时保持原会话并设置 LoadError，RetryLoad 会重试这一次切换。
func (s *Store) SwitchSession(id string) {
	s.mu.Lock()
	if id == s.view.ActiveSessionID {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	gen := s.beginSwitch()
	if !s.persistActive() || !s.isCurrentGen(gen) {
		return
	}
	s.mu.Lock()
	s.view.LoadError = ""
	s.mu.Unlock()
	s.wake()

	fail := func(err error) {
		if !s.isCurrentGen(gen) {
			return
		}
		s.mu.Lock()
		s.failedSessionID = id
		s.view.LoadError = err.Error()
		s.mu.Unlock()
		s.guard.Background("ask.switchSession", err)
		s.wake()
	}
	record, err := s.svc.GetSession(s.ctx(), id)
	if !s.isCurrentGen(gen) {
		return
	}
	if err != nil {
		fail(err)
		return
	}
	if record == nil {
		fail(errors.New("会话不存在或已被删除"))
		return
	}
	var messages []services.AskMessage
	if !record.IsArticle() {
		if messages, err = s.readMessages(record.MessagesJSON); err != nil {
			fail(err)
			return
		}
		if err := s.rememberPersisted(record.ID, messages); err != nil {
			fail(err)
			return
		}
	}
	s.remember(record.ID)
	s.mu.Lock()
	s.failedSessionID = ""
	s.view.ActiveSessionID, s.view.Messages, s.view.IsRunning = record.ID, messages, false
	s.view.ArticleSession = nil
	if record.IsArticle() {
		meta := record.AskSessionMeta
		s.view.ArticleSession = &meta
	}
	s.rev++
	s.mu.Unlock()
	s.wake()
}

// DeleteSession 删除一个会话：若它正是进行中的会话，先停止并把内存里的最新内容保存（保存失败就不删，免得丢内容后又没删掉）。
// 删除经 kit.Guard：失败弹出带原因的提示；成功后若删的是当前会话，换成一个新的空会话。
func (s *Store) DeleteSession(id string) {
	if s.article.SessionID() == id {
		s.article.Stop()
		if !s.article.Persist() {
			return
		}
	}
	if s.View().ActiveSessionID == id {
		s.Stop()
		if !s.persistActive() {
			return
		}
	}
	ok := false
	saves.do(id, func() {
		ok = s.guard.Run(locales.AskDeleteSession(), func() error {
			_, err := s.svc.DeleteSession(s.ctx(), id)
			return err
		})
	})
	if !ok {
		return
	}
	s.article.forget(id)
	s.mu.Lock()
	delete(s.persisted, id)
	s.view.Sessions = slices.DeleteFunc(slices.Clone(s.view.Sessions), func(m services.AskSessionMeta) bool { return m.ID == id })
	isActive := s.view.ActiveSessionID == id
	s.mu.Unlock()
	if isActive {
		next := s.newID("session")
		s.remember(next)
		s.mu.Lock()
		s.view.ActiveSessionID, s.view.Messages, s.view.IsRunning, s.view.ArticleSession = next, nil, false, nil
		s.rev++
		s.mu.Unlock()
	}
	s.wake()
	s.fireSaved()
}

// AdoptArticleSession 把一个「围绕本文」的会话接成当前活跃会话（本文问答里新建会话或换会话之后，让全局这边的当前会话跟上）。
func (s *Store) AdoptArticleSession(session services.AskSessionMeta) {
	s.remember(session.ID)
	s.mu.Lock()
	s.view.ActiveSessionID, s.view.ArticleSession, s.view.Messages, s.view.IsRunning = session.ID, &session, nil, false
	s.rev++
	s.mu.Unlock()
	s.wake()
}

// SyncArticleSession 在本文问答换了会话（新建、从历史打开）之后调用：用本文问答此刻的目标与联网开关更新全局这边的会话信息
// （GlobalArticleConversation 的 onSessionChange）。
func (s *Store) SyncArticleSession(session services.AskSessionMeta, newID string) {
	v := s.article.View()
	next := session
	next.ID = newID
	if v.Target != nil {
		next.Target = v.Target
	}
	web := v.WebEnabled
	next.WebEnabled = &web
	s.AdoptArticleSession(next)
}

// OpenArticle 是 GlobalArticleConversation 的「返回文章」：文章不存在或在回收站里时弹出带原因的提示，成功则交给 OnOpenArticle。
func (s *Store) OpenArticle(itemID string) bool {
	item, found, err := s.svc.ItemInfo(s.ctx(), itemID)
	if err == nil && (!found || item.Deleted) {
		err = errors.New("文章不存在或已删除，请先恢复文章")
	}
	if err != nil {
		return s.guard.Fail(locales.ArticleAskOpenFailed(), "ask.openArticle", err)
	}
	if s.OnOpenArticle != nil {
		s.OnOpenArticle(itemID)
	}
	return true
}

// ---- 证据清除 ----

// ClearEvidenceForItems 登记一批「证据已被清除」的条目（对应渲染层 clearCachedConversationEvidence）：
// 全局与本文问答里已展开的片段与迟到的流都按清除标记重新脱敏，不会再次暴露旧证据。
func (s *Store) ClearEvidenceForItems(ids []string) {
	if len(ids) == 0 {
		return
	}
	s.ev.Clear(ids)
}

// onEvidenceCleared 在证据清除登记之后重新脱敏内存里的消息。脱敏走服务调用，不在锁内做：
// 做完发现这期间消息又变过（流式补丁）就按新内容重来，不覆盖掉别人的改动。
func (s *Store) onEvidenceCleared() {
	for {
		s.mu.Lock()
		msgs, rev := s.view.Messages, s.rev
		s.mu.Unlock()
		cleaned := s.sanitizeMessages(msgs)
		s.mu.Lock()
		if s.rev != rev {
			s.mu.Unlock()
			continue
		}
		s.view.Messages = cleaned
		if t := s.view.ArticleSession; t != nil && t.Target != nil {
			meta := *t
			meta.Target = sanitizeTarget(s.ev, t.Target)
			s.view.ArticleSession = &meta
		}
		s.rev++
		s.mu.Unlock()
		break
	}
	s.wake()
}
