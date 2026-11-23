package overlays

// 旧版（.NET）归知数据迁移对话框（MigrationDialog.tsx 与 App.tsx 里的探测）的状态层。
//
// 启动时探测一次：新库为空且发现旧库就弹出确认；「暂不迁移」记住选择、之后不再自动弹出；
// 迁移只读旧库、原文件不动，成功后给出各类数据的数量，失败可原地重试。

import (
	"errors"
	"sync"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/settings"
	"guizhi-mygo-shell/viva/kit"
	"guizhi-mygo-shell/viva/locales"
)

// MigrationPhase 是迁移对话框所处的阶段。
type MigrationPhase uint8

const (
	// MigrationConfirm 是探测到旧库后的确认页（立即迁移 / 暂不迁移）。
	MigrationConfirm MigrationPhase = iota
	MigrationRunning
	MigrationDone
	MigrationFailed
)

// MigrationView 是迁移对话框的快照。
type MigrationView struct {
	Open  bool
	Phase MigrationPhase
	// ItemCount 与 Path 是探测到的旧库的条目数与文件路径。
	ItemCount int
	Path      string
	// SourcePath 是用户另选的旧库路径；空表示用探测到的默认路径。
	SourcePath string
	// Stats 在完成页是各类数据的迁入数量；Error 在失败页是原因原文。
	Stats services.LegacyMigrationStats
	Error string
}

// Migration 管理旧版数据迁移对话框。方法可从任意 goroutine 调用。
type Migration struct {
	Env
	Service services.Migration
	Prefs   services.Prefs
	Guard   kit.Guard
	// OnMigrated 在迁移成功后调用（迁入的数据要在各视图里刷新：知识库、合集、标签）；在后台线程调用，调用方负责切换线程。
	OnMigrated func()
	// OnClose 在对话框关闭后调用（设置清单据此补上被推迟的弹出）。
	OnClose func()
	// OnDetected 在 Detect 有了结论之后调用（找到了旧库并已打开确认页、没找到、探测失败、用户选过暂不迁移都算）：
	// 设置清单据此排在迁移确认之后再评估，不会两个对话框同时弹出。后台线程调用。
	OnDetected func()

	invalidate func()

	mu   sync.Mutex
	view MigrationView
}

// NewMigration 创建迁移状态层。
func NewMigration(guard kit.Guard, invalidate func()) *Migration {
	return &Migration{Guard: guard, invalidate: invalidate}
}

func (m *Migration) wake() { wake(m.invalidate) }

// View 返回当前快照。
func (m *Migration) View() MigrationView {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.view
}

// IsOpen 报告对话框是否打开着（设置清单自动弹出据此错开）。
func (m *Migration) IsOpen() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.view.Open
}

// Detect 在启动时探测旧库：用户选过「暂不迁移」就跳过；探测失败只留日志（渲染层同样不打扰用户，
// 但旧库在、新库空的用户据此无从得知迁移入口，所以日志必须留）；找到就打开确认页。
func (m *Migration) Detect() {
	if m.Service == nil || prefTrue(m.Prefs, settings.PrefMigrationDismissed) {
		m.detected()
		return
	}
	m.spawn(func() {
		defer m.detected()
		det, found, err := m.Service.DetectLegacy(m.context())
		if err != nil {
			m.Guard.Background("migration.detect", err)
			return
		}
		if !found {
			return
		}
		m.mu.Lock()
		if m.view.Open { // 已经在迁移（或另一次探测先到）：不打断
			m.mu.Unlock()
			return
		}
		m.view = MigrationView{Open: true, Phase: MigrationConfirm, ItemCount: det.ItemCount, Path: det.Path}
		m.mu.Unlock()
		m.wake()
	})
}

func (m *Migration) detected() {
	if m.OnDetected != nil {
		m.OnDetected()
	}
}

// SetSourcePath 另选旧库路径（空串回到默认路径）；只在确认页与失败页可改。
func (m *Migration) SetSourcePath(path string) {
	m.mu.Lock()
	ok := m.view.Open && (m.view.Phase == MigrationConfirm || m.view.Phase == MigrationFailed)
	if ok {
		m.view.SourcePath = path
	}
	m.mu.Unlock()
	if ok {
		m.wake()
	}
}

var errMigrationUnavailable = errors.New("migration service is not available")

// Run 是「立即迁移」与失败后的「重试」：进入迁移中、后台只读迁移；成功进完成页并通知刷新视图，失败进失败页显示原因。
// 迁移进行中重复调用无效。
func (m *Migration) Run() {
	m.mu.Lock()
	if !m.view.Open || (m.view.Phase != MigrationConfirm && m.view.Phase != MigrationFailed) {
		m.mu.Unlock()
		return
	}
	m.view.Phase, m.view.Error = MigrationRunning, ""
	source := m.view.SourcePath
	m.mu.Unlock()
	m.wake()

	m.spawn(func() {
		var (
			stats services.LegacyMigrationStats
			err   = errMigrationUnavailable
		)
		if m.Service != nil {
			stats, err = m.Service.RunLegacy(m.context(), source)
		}
		m.mu.Lock()
		if err != nil {
			m.view.Phase, m.view.Error = MigrationFailed, errorOrUnknown(err.Error())
		} else {
			m.view.Phase, m.view.Stats = MigrationDone, stats
		}
		m.mu.Unlock()
		if err != nil {
			m.Guard.Background("migration.run", err)
		}
		m.wake()
		if err == nil && m.OnMigrated != nil {
			m.OnMigrated()
		}
	})
}

// Close 关闭对话框（Esc、「暂不迁移」、「关闭」、「开始使用」）。迁移进行中忽略；在确认页关闭等于「暂不迁移」，
// 记住选择、之后不再自动弹出（记录写不进去时仍然关闭，但如实提示，下次启动会再问）。
func (m *Migration) Close() {
	m.mu.Lock()
	if !m.view.Open || m.view.Phase == MigrationRunning {
		m.mu.Unlock()
		return
	}
	skip := m.view.Phase == MigrationConfirm
	m.view = MigrationView{}
	m.mu.Unlock()
	m.wake()
	if skip {
		if err := setPrefTrue(m.Prefs, settings.PrefMigrationDismissed); err != nil {
			m.Guard.Report(locales.MigrationSkip(), err)
		}
	}
	if m.OnClose != nil {
		m.OnClose()
	}
}
