package themed

// 阅读面板（ThemedReadingPane.tsx）里与界面无关的部分：生成途中的静态预览、两个对话框的开关，以及「面板此刻该画什么」的判断。

import (
	"fmt"
	"time"

	"guizhi-mygo-shell/native/services"
)

// ---- 静态预览 ----

// SetPreviewOpen 设置已有页面时是否查看生成途中的新稿（「预览新稿 / 返回当前版」）。
func (s *ReadingStore) SetPreviewOpen(open bool) {
	s.mu.Lock()
	s.view.PreviewOpen = open
	s.mu.Unlock()
	s.wake()
	s.schedulePreview()
}

// TogglePreview 切换「预览新稿」。
func (s *ReadingStore) TogglePreview() {
	s.mu.Lock()
	open := !s.view.PreviewOpen
	s.mu.Unlock()
	s.SetPreviewOpen(open)
}

// schedulePreview 在需要预览时安排一次拉取：任务有了可预览的章节，且（还没有页面，或用户在看新稿）。
// 同一组条件（任务、预览版本号、是否看新稿、有没有页面）只排程一次；新的排程会作废还没触发的旧排程；
// 两次拉取至少间隔 previewMinInterval，晚到的旧版本不会盖掉新版本。
func (s *ReadingStore) schedulePreview() {
	s.mu.Lock()
	t, page := s.view.Task, s.view.Page()
	if t == nil || t.PreviewRevision == 0 || (!s.view.PreviewOpen && page != nil) {
		s.mu.Unlock()
		return
	}
	key := fmt.Sprintf("%s|%d|%t|%t", t.ID, t.PreviewRevision, s.view.PreviewOpen, page != nil)
	if key == s.previewKey {
		s.mu.Unlock()
		return
	}
	s.previewKey = key
	s.previewGen++
	gen, sel, taskID, req := s.previewGen, s.selectGen, t.ID, s.request()
	delay := previewMinInterval - s.now().Sub(s.lastPreviewAt)
	if delay < 0 {
		delay = 0
	}
	s.mu.Unlock()
	after := s.After
	if after == nil {
		after = func(d time.Duration, fn func()) { time.AfterFunc(d, fn) }
	}
	after(delay, func() { s.fetchPreview(req, taskID, gen, sel) })
}

func (s *ReadingStore) fetchPreview(req services.ThemedRequest, taskID string, gen, sel int) {
	s.mu.Lock()
	if gen != s.previewGen || sel != s.selectGen {
		s.mu.Unlock()
		return
	}
	s.lastPreviewAt = s.now()
	s.mu.Unlock()

	p, err := s.svc.Preview(s.ctx(), req, taskID)

	s.mu.Lock()
	if gen != s.previewGen || sel != s.selectGen {
		s.mu.Unlock()
		return
	}
	if err == nil && p != nil && (s.view.Preview == nil || p.Revision >= s.view.Preview.Revision) {
		s.view.Preview = p
	}
	s.mu.Unlock()
	if err != nil {
		s.guard.Background("themedReading.preview", err)
	}
	s.wake()
}

// ---- 对话框 ----

// OpenSetup 打开生成设置对话框。
func (s *ReadingStore) OpenSetup(mode SetupMode) {
	s.mu.Lock()
	s.view.Setup = mode
	s.mu.Unlock()
	s.wake()
}

// CloseSetup 关闭生成设置对话框。
func (s *ReadingStore) CloseSetup() { s.OpenSetup(SetupNone) }

// NewSetupForm 按当前读取结果为对话框建表单：调整模式沿用当前页面的选项；模型与搜索状态决定默认值与可否提交。
func (s *ReadingStore) NewSetupForm(mode SetupMode) SetupForm {
	s.mu.Lock()
	defer s.mu.Unlock()
	var current *services.ThemedOptions
	if mode == SetupAdjust {
		if p := s.view.Page(); p != nil {
			o := p.Options
			current = &o
		}
	}
	var models services.ThemedModels
	var search *services.ThemedSearchStatus
	if r := s.view.Result; r != nil {
		models, search = r.Models, r.Search
	}
	return NewSetupForm(current, models, search)
}

// SubmitSetup 提交生成设置：不满足前置条件（模型、搜索）时什么也不做；生成启动成功才关闭对话框。
func (s *ReadingStore) SubmitSetup(form SetupForm) bool {
	if !form.Configured() {
		return false
	}
	if !s.Generate(form.Options()) {
		return false
	}
	s.CloseSetup()
	return true
}

// RequestDelete 打开「删除 AI 阅读页」确认。
func (s *ReadingStore) RequestDelete() {
	s.mu.Lock()
	s.view.DeleteConfirm = true
	s.mu.Unlock()
	s.wake()
}

// CancelDelete 关闭删除确认。
func (s *ReadingStore) CancelDelete() {
	s.mu.Lock()
	s.view.DeleteConfirm = false
	s.mu.Unlock()
	s.wake()
}

// ConfirmDelete 确认删除：成功才关闭确认并通知界面退回原文；失败保留确认框，错误提示已弹出。
func (s *ReadingStore) ConfirmDelete() bool {
	if !s.Remove() {
		return false
	}
	s.CancelDelete()
	if s.OnPageRemoved != nil {
		s.OnPageRemoved()
	}
	return true
}

// ---- 面板此刻该画什么 ----

// PaneBody 是面板主体区域的内容。
type PaneBody int

const (
	BodyNone        PaneBody = iota // 什么也不画（读取中或读取失败，由 LoadError / Loading 负责）
	BodyOriginal                    // 只显示原文（页面不显示且界面给了原文视图）
	BodyNativePage                  // 当前格式页面（v3）：原生阅读视图
	BodyLegacyFrame                 // 旧格式页面：沙箱 iframe 里的整页文档
	BodyTaskStatus                  // 居中的任务状态卡（还没有可渲染的页面）
	BodyWaitDone                    // 任务已完成，正在载入成品
	BodyEmpty                       // 没有页面也没有任务：引导生成
)

// ToolbarModel 是面板工具栏上各控件是否出现。
type ToolbarModel struct {
	// Show 为 false 时整条工具栏不画（页面不显示，或既没有页面也没有任务）。
	Show bool
	// Tools 是「阅读页设置」菜单（有页面时）。
	Tools bool
	// StartOver 是「重新生成」按钮（没有页面、有已结束的任务）；StartOverDisabled 为不可点。
	StartOver, StartOverDisabled bool
	// PreviewToggle 是「预览新稿 / 返回当前版」；PreviewOpen 决定文案。
	PreviewToggle, PreviewOpen bool
	// SwitchLatest 是「新版已完成 · 切换」。
	SwitchLatest bool
}

// PaneModel 是面板的布局判断：哪些提示、状态条和主体区域该出现。
type PaneModel struct {
	Toolbar ToolbarModel

	// ReadOnlyNote：条目在回收站里，页面仍可查看与导出。
	ReadOnlyNote bool
	// ShowLoadError / LoadError：读取失败（带重试）；ShowLoading：还没读到结果。
	ShowLoadError bool
	LoadError     string
	ShowLoading   bool
	// StatusStrip：页面上方的紧凑状态条（有可渲染的页面且任务状态需要显示）。
	StatusStrip bool
	// StatusExpanded：主体里的任务状态卡用展开样式（没有可渲染的页面时）。
	StatusExpanded bool
	// StaleNote：原文已有更新；LegacyNote：旧格式页面只读。
	StaleNote, LegacyNote bool

	Body PaneBody
}

// Pane 计算面板布局。readOnly 是条目在回收站里；hasOriginal 表示界面给了可退回的原文视图。
func (v ReadingView) Pane(readOnly, hasOriginal bool) PaneModel {
	page := v.Page()
	active := v.Active()
	reading := v.ReadingPage()
	// 任务状态：未完成的任务总要显示；已完成的任务只在当前格式页面上保留（用于查看生成详情）。
	taskStatus := v.Task != nil && (v.Task.State != services.ThemedTaskCompleted || (page != nil && page.FormatVersion == 3))

	m := PaneModel{
		ReadOnlyNote:  readOnly,
		ShowLoadError: v.LoadError != "",
		LoadError:     v.LoadError,
		ShowLoading:   v.LoadError == "" && v.Result == nil,
	}
	m.Toolbar = ToolbarModel{
		Show:              v.ShowPage && (page != nil || v.Task != nil),
		Tools:             page != nil,
		StartOver:         page == nil && v.Task != nil && !active,
		StartOverDisabled: v.Disabled(readOnly) || v.Result == nil,
		PreviewToggle:     page != nil && v.Task != nil && v.Task.PreviewRevision > 0 && v.Task.VersionID != page.ID && v.Latest == nil,
		PreviewOpen:       v.PreviewOpen,
		SwitchLatest:      v.Latest != nil,
	}
	if !v.ShowPage && hasOriginal {
		m.Body = BodyOriginal
		return m
	}
	// 读取失败只显示错误态；其余提示与主体按渲染层的先后顺序判断。
	m.StatusStrip = reading != nil && taskStatus
	m.StatusExpanded = reading == nil
	m.StaleNote = page != nil && v.Result != nil && v.Result.Stale
	m.LegacyNote = page != nil && !v.ShowingPreview() && page.FormatVersion < 3
	healthy := v.Result != nil && v.LoadError == ""
	switch {
	case reading != nil && reading.FormatVersion == 3:
		m.Body = BodyNativePage
	case page != nil && v.Result != nil && v.Result.Document != "":
		m.Body = BodyLegacyFrame
	case healthy && taskStatus:
		m.Body = BodyTaskStatus
	case healthy && v.Task != nil && v.Task.State == services.ThemedTaskCompleted:
		m.Body = BodyWaitDone
	case healthy:
		m.Body = BodyEmpty
	}
	return m
}
