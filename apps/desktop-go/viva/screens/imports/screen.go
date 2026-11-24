package imports

import (
	"context"
	"sync"
	"time"

	"github.com/vivaui/viva/ui"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/viva/kit"
)

// Hooks 是采集模块需要外壳代办的事：跨模块跳转、快速采集面板、快捷键文案。
// 都可以为空（对照与单测里没有外壳）；为空时对应的入口什么都不做。
type Hooks struct {
	// QuickCapture 打开快速采集（渲染层 window.dispatchEvent("shortcut:newItem")）。
	QuickCapture func()
	// OpenItem 打开一个知识条目：切到知识库、范围回到「全部」并选中它。
	OpenItem func(itemID string)
	// CaptureShortcut 返回「新建条目」快捷键当前的显示文案（如 Ctrl+N），空引导页的提示用。
	CaptureShortcut func() string
	// OpenSettingsSection 打开设置页的某一节（渲染层 requestSettingsSection）。
	OpenSettingsSection func(section string)
	// WebSolverEnabled 报告网页验证自动求解（FlareSolverr）是否开启：开启时重试按钮写「自动验证并采集」。
	WebSolverEnabled func() bool
	// RetryAuthenticated 用内置登录窗口的登录态重试一个任务（渲染层 retryAuthenticated：查平台状态、必要时登录、再带登录态重试）。
	// 在后台调用，可以阻塞（等用户登录）；返回登录态重试是否已发起（详情弹窗据此关闭），失败由钩子自己带详情提示（「登录态重试未开始」）。
	RetryAuthenticated func(task services.ImportTask, platform string) bool
	// AskAboutItem 围绕新入库的内容开始提问（渲染层 askAboutItem）：新建会话（不污染刚浏览的历史会话）、
	// 把 draft 预填进输入框（只预填不发送）、切到问答。
	AskAboutItem func(draft string)
	// AppInfo 返回应用版本与运行平台（复制诊断信息用）；取不到的返回空串，诊断文本就少一行环境信息。
	AppInfo func() (version, platform string)
	// CaptureBrowser 返回登录态采集用的浏览器（名称加版本，诊断信息用）；没有信息返回空串。
	CaptureBrowser func(platform string) string
	// VerifyNodeseek 打开 NodeSeek 的验证窗口，等用户通过验证后返回（渲染层 platformCapture.login("nodeseek", …)）。
	// 在后台调用，会阻塞；取消或窗口被关闭时返回错误。随后由界面带登录态重试任务。
	VerifyNodeseek func(ctx context.Context, task services.ImportTask) error
	// CancelNodeseek 取消正在等待的 NodeSeek 验证（渲染层 platformCapture.cancelLogin("nodeseek")）。
	CancelNodeseek func() error
}

// Screen 是「采集」模块的界面（渲染层 components/imports/*）：二级侧栏里的筛选，工作区里的任务队列。
//
// 状态都在 Store 里；Screen 只保存界面自己的东西：搜索框的输入、详情弹窗与清理确认的开合、行悬停的分组登记。
// 所有方法都在界面线程里调用。
type Screen struct {
	Look  *kit.Look
	Store *Store
	// Ctx 是后台请求用的上下文（应用生命周期）。
	Ctx context.Context
	// Guard 是变更类操作的失败通道（与 Store 里的是同一个）；Guard.Notify 同时是提示条的入口。
	Guard kit.Guard
	Hooks Hooks
	// DocSite 是「采集文档站」整页的状态（渲染层 DocumentSitePanel）；为空（装配不全的自动化实例、旧单测）时
	// 点「采集文档站」仍显示「尚未迁移」的占位页。
	DocSite *DocSiteStore

	// Now 返回当前时间；对照与测试注入固定时钟。空表示取系统时钟。
	Now func() time.Time
	// Loc 与 Locale 决定时间的写法（今天写时分，其余写日期）；空表示本地时区与中文。
	Loc    *time.Location
	Locale kit.DateLocale

	search       ui.Field
	queryPushed  string // 最近一次与 Store 对齐过的搜索词：输入框与 Store 谁先变，靠它分辨
	searchFocus  bool   // 上一帧搜索框是否有焦点（边框高亮跟它走，晚一帧不可见）
	queryStop    func()
	mounted      bool
	documents    bool // 工作区显示「采集文档站」而不是任务队列（渲染层 ImportsWorkspace 的 view）
	docUI        docSiteUI
	detailTaskID string // 详情弹窗打开的任务 id；空表示没开
	detail       detailState
	clear        *clearConfirm
	confirm      kit.ConfirmState
	rowHover     kit.HoverGroups
	loading      kit.Delayed
	window       kit.RowWindow // 任务行的窗口化声明（台账 B-028）：只声明看得见的行

	mu           sync.Mutex    // 保护 pendingClear 与 detail 里由后台写入的字段：界面线程在帧首取走或读出
	pendingClear *clearConfirm // 预览完成、等界面线程打开的确认框
}

// clearConfirm 是「清理采集任务历史」确认框的内容：范围与会删除的条数在点按钮时预览好，确认框只负责呈现与执行。
type clearConfirm struct {
	query services.ImportClearQuery
	label string
	count int
}

// nowTime 是此刻（对照里是固定时钟）。
func (s *Screen) nowTime() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// loc 是时间的时区。
func (s *Screen) loc() *time.Location {
	if s.Loc != nil {
		return s.Loc
	}
	return time.Local
}

// importFiles 弹出文件选择框并把所选文件入队：系统对话框会阻塞，所以放到后台做。
func (s *Screen) importFiles() {
	s.Store.Spawn(func() { s.Store.ImportFiles(s.Ctx) })
}
