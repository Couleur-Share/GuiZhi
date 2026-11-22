package ask

import (
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"guizhi-mygo-shell/aiflow"
	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/services/servicestest"
	"guizhi-mygo-shell/viva/kit"
)

// ---- 测试替身 ----

// recLog 记录写进日志的失败（Guard 在弹提示的同时写日志，后台失败只写日志）。
type recLog struct {
	mu   sync.Mutex
	logs []string
}

func (r *recLog) AppError(scope, action, message string) {
	r.mu.Lock()
	r.logs = append(r.logs, action+"|"+message)
	r.mu.Unlock()
}

func (r *recLog) entries() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.logs)
}

// memPrefs 是内存里的界面偏好；failSet 为真时写入失败。
type memPrefs struct {
	mu      sync.Mutex
	data    map[string]json.RawMessage
	failSet bool
}

func (m *memPrefs) Get(key string) (json.RawMessage, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.data[key]
	return v, ok
}

func (m *memPrefs) Set(key string, value any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failSet {
		return errors.New("prefs unavailable")
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if m.data == nil {
		m.data = map[string]json.RawMessage{}
	}
	m.data[key] = raw
	return nil
}

func (m *memPrefs) active() string {
	raw, ok := m.Get(PrefActiveSession)
	if !ok {
		return ""
	}
	var id string
	_ = json.Unmarshal(raw, &id)
	return id
}

// rig 把假服务、提示通道、日志与各个钩子的调用记录接在一个 Store 上。Spawn 默认同步，保持确定性；
// 并发用例用 useGoroutines 改回真正的 goroutine。ID 与时间都是确定的。
type rig struct {
	t      *testing.T
	svc    *servicestest.AskFake
	store  *Store
	notify *kit.Notifier
	log    *recLog
	prefs  *memPrefs
	wakes  atomic.Int32

	mu         sync.Mutex
	saved      int
	opened     []string
	settingsAI int
	idSeq      int
	uuidSeq    int
}

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{t: t, svc: &servicestest.AskFake{}, notify: kit.NewNotifier(nil), log: &recLog{}, prefs: &memPrefs{}}
	r.store = NewStore(r.svc, kit.Guard{Notify: r.notify, Log: r.log}, func() { r.wakes.Add(1) })
	r.store.Prefs = r.prefs
	r.store.Spawn = func(fn func()) { fn() }
	r.store.NewID = func(prefix string) string {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.idSeq++
		return prefix + "-" + strconv.Itoa(r.idSeq)
	}
	r.store.OnSaved = func() { r.locked(func() { r.saved++ }) }
	r.store.OnOpenArticle = func(id string) { r.locked(func() { r.opened = append(r.opened, id) }) }
	r.store.OnOpenSettingsAI = func() { r.locked(func() { r.settingsAI++ }) }
	r.store.Article().NewUUID = func() string {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.uuidSeq++
		return "uuid-" + strconv.Itoa(r.uuidSeq)
	}
	return r
}

func (r *rig) locked(fn func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	fn()
}

// dirty 让活跃会话的落盘快照失效，使下一次 Persist 真的去保存（模拟「内存里有尚未保存的改动」）。
func (r *rig) dirty() {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	delete(r.store.persisted, r.store.view.ActiveSessionID)
}

func (r *rig) useGoroutines() { r.store.Spawn = func(fn func()) { go fn() } }

func (r *rig) view() View                { return r.store.View() }
func (r *rig) article() *ArticleStore    { return r.store.Article() }
func (r *rig) artView() ArticleView      { return r.store.Article().View() }
func (r *rig) toasts() []kit.QueuedToast { return r.notify.Queued() }
func (r *rig) savedEvents() (n int)      { r.locked(func() { n = r.saved }); return n }
func (r *rig) openedArticles() (o []string) {
	r.locked(func() { o = slices.Clone(r.opened) })
	return o
}
func (r *rig) settingsOpened() (n int) { r.locked(func() { n = r.settingsAI }); return n }

func (r *rig) lastToast() kit.QueuedToast {
	r.t.Helper()
	all := r.toasts()
	if len(all) == 0 {
		r.t.Fatal("期望有一条提示，实际没有")
	}
	return all[len(all)-1]
}

func (r *rig) wantToast(title, detail string) {
	r.t.Helper()
	got := r.lastToast()
	if got.Kind != kit.ToastError || got.Message != title || got.Detail != detail {
		r.t.Fatalf("提示不对：\n got  [%s] %q (detail %q)\n want [error] %q (detail %q)", got.Kind, got.Message, got.Detail, title, detail)
	}
}

// ready 初始化并断言没有失败。
func (r *rig) ready() {
	r.t.Helper()
	r.store.Initialize()
	if v := r.view(); v.LoadError != "" || !v.HasLoaded {
		r.t.Fatalf("初始化应成功：%+v", v)
	}
}

// ---- 夹具 ----

func done(id, question, answer string) services.AskMessage {
	return services.AskMessage{ID: id, Question: question, Answer: answer, Status: services.AskStatusDone}
}

// putAsk 摆一条全局会话到库里。
func (r *rig) putAsk(id string, msgs ...services.AskMessage) services.AskSessionRecord {
	r.t.Helper()
	body, err := marshalAskMessages(msgs)
	if err != nil {
		r.t.Fatal(err)
	}
	rec := services.AskSessionRecord{MessagesJSON: body}
	rec.ID, rec.Title = id, buildSessionTitle(msgs)
	return r.svc.PutSession(rec)
}

func target(item string) aiflow.ArticleTarget {
	return aiflow.ArticleTarget{ItemID: item, View: "body"}
}

func str(s string) *string { return &s }

func articleDone(id, question, answer string) aiflow.ArticleMessage {
	return aiflow.ArticleMessage{ID: id, Question: question, Answer: answer, Status: services.AskStatusDone, Sources: []aiflow.ArticleSource{}, Warnings: []string{}, WebStatus: "off"}
}

// putArticle 摆一条本文会话到库里。
func (r *rig) putArticle(id, item, articleTitle string, web *bool, msgs ...aiflow.ArticleMessage) services.AskSessionRecord {
	r.t.Helper()
	body, err := marshalArticleMessages(msgs)
	if err != nil {
		r.t.Fatal(err)
	}
	tg := target(item)
	rec := services.AskSessionRecord{MessagesJSON: body}
	rec.ID, rec.Title, rec.Scope, rec.ItemID, rec.ArticleTitle, rec.WebEnabled, rec.Target = id, buildArticleTitle(msgs), services.AskScopeArticle, item, articleTitle, web, &tg
	return r.svc.PutSession(rec)
}

// source 造一份带证据快照的来源。
func source(ordinal int, refID, fingerprint string) services.AskSource {
	return services.AskSource{Ordinal: ordinal, Kind: "item", RefID: refID, Title: "title-" + refID,
		Evidence: &aiflow.EvidenceSnapshot{Version: 1, Kind: "item", Title: "title-" + refID, Text: "body-" + refID, Fingerprint: fingerprint, ReviewStatus: "clear", ReviewReasons: []string{}}}
}

// ---- 等待 ----

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("等待超时：%s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// goAsync 在 goroutine 里跑 fn，返回等待它结束的函数。
func goAsync(fn func()) (wait func()) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	return func() { <-done }
}

func lastMessage(t *testing.T, v View) services.AskMessage {
	t.Helper()
	if len(v.Messages) == 0 {
		t.Fatal("没有消息")
	}
	return v.Messages[len(v.Messages)-1]
}

var errBoom = errors.New("boom")
