package themed

// 阅读模式（use-themed-reading-mode.ts）：知识库详情里「原文 / AI 阅读 / 编辑」三态的选择与默认值。
// 内容版本与编辑状态分开；每次打开一篇内容，有成品时默认阅读 AI 页。

import (
	"context"
	"sync"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
)

// 阅读面板的标签（reading-memory 的 ReadingPanelTab 里与主题页有关的两个）。
const (
	TabBody    = "body"
	TabSummary = "summary"
)

// SourceKindOf 把阅读面板标签映射为主题页来源：讨论总结用 summary，其余一律是正文。
func SourceKindOf(tab string) string {
	if tab == TabSummary {
		return services.ThemedKindSummary
	}
	return services.ThemedKindBody
}

// ModeView 是当前内容的模式快照。
type ModeView struct {
	ItemID     string
	SourceKind string
	// Available 为 true 时这篇内容可以有 AI 阅读（功能启用，且在正文或讨论总结标签上）。
	Available bool
	// Active 是正在阅读 AI 页（编辑中、不可用时一律为 false）；Editing 是正在编辑原文。
	Active, Editing bool
}

// ModeStore 管理阅读模式。方法可从任意 goroutine 调用。
type ModeStore struct {
	svc        services.ThemedReading
	guard      kit.Guard
	invalidate func()

	// Ctx 是发给服务的请求的根上下文；nil 取 context.Background()。
	Ctx context.Context
	// Spawn 在后台执行一个动作；默认开 goroutine，测试注入同步执行。
	Spawn func(fn func())

	mu         sync.Mutex
	itemID     string
	sourceKind string
	available  bool
	active     bool // 用户选择（或默认探测）的「看 AI 页」，不含 Available / Editing 的修饰
	editing    bool
	// effect 是「环境」代次：内容或可用性变了加一，旧的默认探测作废。selection 是用户主动选择的次数。
	effect, selection int
}

// NewModeStore 创建阅读模式。
func NewModeStore(svc services.ThemedReading, guard kit.Guard, invalidate func()) *ModeStore {
	return &ModeStore{svc: svc, guard: guard, invalidate: invalidate, Spawn: func(fn func()) { go fn() }}
}

// View 返回当前快照。
func (m *ModeStore) View() ModeView {
	m.mu.Lock()
	defer m.mu.Unlock()
	return ModeView{
		ItemID: m.itemID, SourceKind: m.sourceKind, Available: m.available,
		Active: m.available && m.active && !m.editing, Editing: m.editing,
	}
}

func (m *ModeStore) wake() {
	if m.invalidate != nil {
		m.invalidate()
	}
}

func (m *ModeStore) ctx() context.Context {
	if m.Ctx != nil {
		return m.Ctx
	}
	return context.Background()
}

// Open 选中内容与标签。内容、来源与可用性都没变时什么也不做（保留用户已做的选择）；否则回到「原文」，
// 并在可用时后台探测：这份内容已有页面就默认进入 AI 阅读——除非探测期间用户已主动选择或开始编辑，那样就尊重用户。
func (m *ModeStore) Open(itemID, tab string, enabled bool) {
	kind := SourceKindOf(tab)
	available := enabled && (tab == TabBody || tab == TabSummary)
	m.mu.Lock()
	if m.itemID == itemID && m.sourceKind == kind && m.available == available {
		m.mu.Unlock()
		return
	}
	m.itemID, m.sourceKind, m.available = itemID, kind, available
	m.active, m.editing = false, false
	m.effect++
	effect, selection := m.effect, m.selection
	m.mu.Unlock()
	m.wake()
	if !available {
		return
	}
	spawn := m.Spawn
	if spawn == nil {
		spawn = func(fn func()) { go fn() }
	}
	spawn(func() { m.probe(itemID, kind, effect, selection) })
}

func (m *ModeStore) probe(itemID, kind string, effect, selection int) {
	st, err := m.svc.State(m.ctx(), services.ThemedRequest{ItemID: itemID, SourceKind: kind})
	m.mu.Lock()
	if effect != m.effect || selection != m.selection || m.editing {
		m.mu.Unlock()
		return
	}
	if err == nil {
		m.active = st.HasPage
	}
	m.mu.Unlock()
	if err != nil {
		// 完整阅读面板负责展示可重试的读取错误；这里只留痕，默认停在原文。
		m.guard.Background("themedReading.state", err)
		return
	}
	m.wake()
}

// Select 主动选择看 AI 页（true）或原文（false），同时结束编辑。
func (m *ModeStore) Select(active bool) {
	m.mu.Lock()
	m.selection++
	m.active, m.editing = active, false
	m.mu.Unlock()
	m.wake()
}

// Toggle 切换原文 / AI 阅读。
func (m *ModeStore) Toggle() {
	m.mu.Lock()
	next := !m.active
	m.mu.Unlock()
	m.Select(next)
}

// StartEditing 开始编辑原文（AI 页退出显示）。
func (m *ModeStore) StartEditing() {
	m.mu.Lock()
	m.selection++
	m.active, m.editing = false, true
	m.mu.Unlock()
	m.wake()
}

// FinishEditing 结束编辑，回到原文。
func (m *ModeStore) FinishEditing() { m.Select(false) }
