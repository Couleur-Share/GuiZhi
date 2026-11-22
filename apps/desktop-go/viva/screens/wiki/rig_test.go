package wiki

import (
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

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

func (r *recLog) AppError(_, action, message string) {
	r.mu.Lock()
	r.logs = append(r.logs, action+"|"+message)
	r.mu.Unlock()
}

func (r *recLog) entries() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.logs)
}

// rig 把假服务、提示通道与日志接在一个 Store 上。Spawn 默认同步，保持确定性；并发用例用 useGoroutines 改回真正的 goroutine。
type rig struct {
	t      *testing.T
	svc    *servicestest.WikiFake
	store  *Store
	notify *kit.Notifier
	log    *recLog
	wakes  atomic.Int32

	mu      sync.Mutex
	sources []string
}

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{t: t, svc: &servicestest.WikiFake{}, notify: kit.NewNotifier(nil), log: &recLog{}}
	r.store = NewStore(r.svc, kit.Guard{Notify: r.notify, Log: r.log}, func() { r.wakes.Add(1) })
	r.store.Spawn = func(fn func()) { fn() }
	r.store.OnOpenSource = func(id string) {
		r.mu.Lock()
		r.sources = append(r.sources, id)
		r.mu.Unlock()
	}
	return r
}

func (r *rig) useGoroutines() { r.store.Spawn = func(fn func()) { go fn() } }

func (r *rig) view() View                { return r.store.View() }
func (r *rig) compile() *CompileStore    { return r.store.Compile() }
func (r *rig) cview() CompileView        { return r.store.Compile().View() }
func (r *rig) sview() SuggestionView     { return r.store.Suggestion().View() }
func (r *rig) toasts() []kit.QueuedToast { return r.notify.Queued() }
func (r *rig) openedSources() (out []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.sources)
}

func (r *rig) lastToast() kit.QueuedToast {
	r.t.Helper()
	all := r.toasts()
	if len(all) == 0 {
		r.t.Fatal("期望有一条提示，实际没有")
	}
	return all[len(all)-1]
}

func (r *rig) wantNoToast() {
	r.t.Helper()
	if all := r.toasts(); len(all) != 0 {
		r.t.Fatalf("不该有提示：%+v", all)
	}
}

func (r *rig) wantToast(kind kit.ToastKind, message, detail string) {
	r.t.Helper()
	got := r.lastToast()
	if got.Kind != kind || got.Message != message || got.Detail != detail {
		r.t.Fatalf("提示不对：\n got  [%s] %q (detail %q)\n want [%s] %q (detail %q)", got.Kind, got.Message, got.Detail, kind, message, detail)
	}
}

// page 摆一页并返回它的 ID（标题取 ID，正文取 "body of <id>"）。
func (r *rig) page(id string, mod ...func(*services.WikiPage)) string {
	p := services.WikiPage{ID: id, Title: id, Body: "body of " + id}
	for _, m := range mod {
		m(&p)
	}
	r.svc.PutPage(p)
	return id
}

// ready 初始化并断言没有失败。
func (r *rig) ready() {
	r.t.Helper()
	r.store.Initialize()
	if v := r.view(); v.LoadError != "" || !v.HasLoaded {
		r.t.Fatalf("初始化应成功：%+v", v)
	}
}

// seed 摆三页：Alpha、Beta（手动编辑过）、Gamma（entity），Alpha→Beta 一条链接。
func (r *rig) seed() {
	r.t.Helper()
	r.page("alpha", func(p *services.WikiPage) { p.Title = "Alpha"; p.Summary = "first page" })
	now := int64(5)
	r.page("beta", func(p *services.WikiPage) { p.Title = "Beta"; p.ManualEditedAt = &now })
	r.page("gamma", func(p *services.WikiPage) { p.Title = "Gamma"; p.Kind = services.WikiKindEntity })
	r.svc.PutLink("alpha", "beta")
}

func ids(entries []services.WikiCatalogEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.ID
	}
	return out
}

func wantIDs(t *testing.T, got []services.WikiCatalogEntry, want ...string) {
	t.Helper()
	if g := ids(got); !slices.Equal(g, want) {
		t.Fatalf("页面顺序不对：got %v want %v", g, want)
	}
}

// candidate 构造一条预览里的候选资料。
func candidate(id, state string, blocks, reusable int) services.WikiCompileCandidate {
	return services.WikiCompileCandidate{ID: id, Title: "item " + id, Fingerprint: "fp-" + id, State: state, Blocks: blocks, Reusable: reusable}
}

func previewOf(entries ...services.WikiCompileCandidate) services.WikiCompilePreview {
	p := services.WikiCompilePreview{Entries: entries}
	for _, e := range entries {
		switch e.State {
		case services.WikiCandidateReady:
			p.Counts.Ready++
		case services.WikiCandidateReview:
			p.Counts.Review++
		case services.WikiCandidateWaiting:
			p.Counts.Waiting++
		case services.WikiCandidateUpgrade:
			p.Counts.Upgrade++
		case services.WikiCandidateCurrent:
			p.Counts.Current++
		case services.WikiCandidateExcluded:
			p.Counts.Excluded++
		}
	}
	return p
}

func manyReady(n int) []services.WikiCompileCandidate {
	out := make([]services.WikiCompileCandidate, n)
	for i := range out {
		out[i] = candidate(fmt.Sprintf("i%03d", i), services.WikiCandidateReady, 2, 0)
	}
	return out
}
