package app

// 外壳接线：窗口的显隐、关闭策略与托盘由 main 包的生命周期策略（app_lifecycle.go，与 React 路径共用）决定，
// 这里只定义它挂到 Viva 主窗口上的接口，使 viva/app 不依赖 main 包，也能用无头会话测试。

import (
	vapp "github.com/vivaui/viva/app"
)

// Shell 是外壳（main 包的生命周期策略）接到 Viva 主窗口上的钩子。Options.Shell 为空表示没有外壳接线
// （截图、对照与无头测试：窗口关闭就是关闭，没有托盘）。
type Shell interface {
	// Attach 在主窗口打开之后、事件循环线程上调用一次：外壳据此取得窗口、托盘与界面命令的入口。
	Attach(h *Host)
	// OnCloseRequest 在用户关闭主窗口（标题栏的关闭钮、Alt+F4）时调用（事件循环线程）。
	// 返回 true 表示外壳接管了这次关闭（隐藏到托盘，或让界面弹出关闭对话框）；false 表示照常关闭。
	OnCloseRequest() (handled bool)
	// CloseDialogResult 与 CloseDialogCancel 是关闭对话框的结果：选了动作（可勾「记住我的选择」），或只是关掉对话框。
	// 在界面线程上调用；action 取 "minimize" 或 "exit"。
	CloseDialogResult(action string, remember bool)
	CloseDialogCancel()
}

// Command 是外壳推给界面的命令（对应渲染层的 app:command 与 shortcut:triggered）。
type Command string

const (
	// CommandNewItem 是托盘「新建」：先回到主页，再打开快速采集（渲染层 DesktopAppCommandBridge 的 item:new）。
	CommandNewItem Command = "item:new"
	// CommandShortcutNewItem 是 newItem 快捷键：只请求打开快速采集，不切换页面（渲染层的 shortcut:newItem 事件）。
	CommandShortcutNewItem Command = "shortcut:newItem"
	// CommandOpenSettings 打开设置页。
	CommandOpenSettings Command = "settings:open"
	// CommandOpenUpdater 打开更新对话框。
	CommandOpenUpdater Command = "updater:open"
	// CommandFocusSearch 聚焦顶栏搜索框（search 快捷键；渲染层的 shortcut:search 事件）。
	CommandFocusSearch Command = "shortcut:search"
)

// hostWindow 是 Host 需要的窗口能力（*vapp.Window 满足；测试用假实现）。
type hostWindow interface {
	Show()
	Hide()
	Close()
	Hidden() bool
	Minimized() bool
}

// hostApp 是 Host 需要的应用能力（*vapp.Application 满足；测试用假实现）。
type hostApp interface {
	Quit()
	AddStatusItem(config vapp.StatusItemConfig) (*vapp.StatusItem, error)
}

// Host 是外壳对主窗口与应用能做的事。窗口方法都可在任意 goroutine 调用（Viva 把它们转到事件循环线程）；
// AddStatusItem 要在事件循环线程上调用——从别的线程用 RunOnUI 转进来。
type Host struct {
	app  hostApp
	win  hostWindow
	root *Root
}

// Show 把窗口带到前台：最小化先还原，再显示并聚焦（Viva 的 Show 就是「显示或提升」）。
// 显示之后补做隐藏期间被跳过的自动更新检查（渲染层 window:visibility-changed）。
func (h *Host) Show() {
	h.win.Show()
	if h.root != nil {
		h.root.windowShown()
	}
}

// Hide 把窗口收到托盘：窗口与内容保留，应用继续运行。收起前先销毁岛（WebView 子进程只在可见时存在）。
func (h *Host) Hide() {
	h.root.closeIslands()
	h.win.Hide()
}

// Close 请求关闭窗口（会再经过 OnCloseRequest）。
func (h *Host) Close() { h.win.Close() }

// Quit 关闭所有窗口并结束事件循环，不经过关闭请求。
func (h *Host) Quit() { h.app.Quit() }

// Visible 报告窗口是否在屏幕上（没被 Hide 收起；最小化的窗口也算在屏幕上，由 Minimized 区分）。
func (h *Host) Visible() bool { return !h.win.Hidden() }

// Minimized 报告窗口是否被最小化。
func (h *Host) Minimized() bool { return h.win.Minimized() }

// AddStatusItem 在系统托盘区加一个图标。事件循环线程。
func (h *Host) AddStatusItem(config vapp.StatusItemConfig) (*vapp.StatusItem, error) {
	return h.app.AddStatusItem(config)
}

// ShowCloseDialog 让界面弹出关闭对话框（对应渲染层的 window:showCloseDialog）。任意 goroutine。
func (h *Host) ShowCloseDialog() { h.root.showCloseDialog() }

// Dispatch 把命令交给界面执行（窗口由外壳先带到前台）。任意 goroutine。
func (h *Host) Dispatch(cmd Command) { h.root.dispatch(cmd) }

// RunOnUI 把 fn 排到界面线程上、在下一帧开头执行（并请求重绘）。任意 goroutine。
func (h *Host) RunOnUI(fn func()) { h.root.runOnUI(fn) }
