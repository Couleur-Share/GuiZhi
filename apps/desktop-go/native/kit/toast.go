package kit

import (
	"strconv"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
)

// ToastKind 是提示的类别，对应渲染层 Toast 的 type。
type ToastKind string

const (
	ToastSuccess ToastKind = "success"
	ToastError   ToastKind = "error"
	ToastInfo    ToastKind = "info"
	ToastWarning ToastKind = "warning"
)

// persistent 报告这类提示是否留到用户手动关闭。
//
// 成功/提示几秒后淡出；错误与警告不自动消失：几秒就消失的报错等于没报，用户只会把同一个操作再做一遍
// （渲染层 Toast 的同一条规则）。
func (k ToastKind) persistent() bool { return k == ToastError || k == ToastWarning }

const (
	// toastAutoDismiss 是成功/提示类的显示时长（渲染层 AUTO_DISMISS_MS）。
	toastAutoDismiss = 3 * time.Second
	// toastUndoWindow 是带「撤销」的提示的时长：3 秒不够读完一句话再决定要不要点（渲染层 UNDO_DISMISS_MS）。
	toastUndoWindow = 8 * time.Second
)

// toastSpec 是一次 Show 的可选项。
type toastSpec struct {
	detail     string
	action     string
	onAction   func()
	timeout    time.Duration
	hasTimeout bool
}

// ToastOption 调整一条提示。
type ToastOption func(*toastSpec)

// WithDetail 给提示附上完整报错原文：折叠在「查看详情」后面，展开能读、能复制去搜。
// 提示语要短才读得进去，但「失败」两个字对排查毫无用处——原因收在这里。
// 批量操作把逐条失败明细放这里最合适。
func WithDetail(detail string) ToastOption { return func(s *toastSpec) { s.detail = detail } }

// WithAction 给提示加一个行内动作按钮（如「撤销」），点击后执行 fn 并立即收起提示。
// fn 在 UI 线程执行。带动作的非持久提示用撤销窗口时长，除非 WithTimeout 另有指定。
func WithAction(label string, fn func()) ToastOption {
	return func(s *toastSpec) { s.action, s.onAction = label, fn }
}

// WithTimeout 覆盖默认显示时长；负数表示不自动消失。
func WithTimeout(d time.Duration) ToastOption {
	return func(s *toastSpec) { s.timeout, s.hasTimeout = d, true }
}

// toastOp 是排队等 UI 线程执行的一次操作。
type toastOp struct {
	close bool
	id    string
	toast ui.Toast
}

// toastMeta 随 ui.Toast.Data 带给视图。
type toastMeta struct {
	kind   ToastKind
	detail string
}

// toastView 是一条提示在 UI 线程上的展示状态。
type toastView struct {
	expanded    bool
	justCopied  bool      // 复制成功的结果刚回来，下一帧开始计「已复制」
	copiedUntil time.Time // 「已复制」显示到这个时刻
}

// Notifier 是应用的提示通道：任何 goroutine 都能 Show，提示在下一帧由 ToastViewport 画出。
//
// 为什么要排队：MyGo 的提示属于窗口的 ui.Context，只能在 UI 线程添加；
// 后台任务失败、守卫里的失败要从别的 goroutine 报上来，所以先入队、请求重绘、再由视图每帧领走。
type Notifier struct {
	mu    sync.Mutex
	seq   uint64
	queue []toastOp
	wake  func()

	// 仅 UI 线程读写
	views map[string]*toastView
}

// NewNotifier 创建提示通道。wake 请求窗口重绘（可在任意 goroutine 调用）；nil 时由调用方自己驱动帧。
func NewNotifier(wake func()) *Notifier {
	return &Notifier{wake: wake, views: map[string]*toastView{}}
}

// SetWake 设置请求重绘的函数（窗口在通道之后才创建）。
func (n *Notifier) SetWake(wake func()) {
	n.mu.Lock()
	n.wake = wake
	n.mu.Unlock()
}

// Show 显示一条提示并返回它的 ID（可交给 Dismiss 提前收起）。可在任意 goroutine 调用。
func (n *Notifier) Show(kind ToastKind, message string, opts ...ToastOption) string {
	var spec toastSpec
	for _, o := range opts {
		o(&spec)
	}
	timeout := toastAutoDismiss
	switch {
	case spec.hasTimeout:
		timeout = spec.timeout
	case kind.persistent():
		timeout = -1
	case spec.action != "":
		timeout = toastUndoWindow
	}

	n.mu.Lock()
	n.seq++
	id := "gz-toast-" + strconv.FormatUint(n.seq, 10)
	t := ui.Toast{
		ID: id, Title: message, Type: string(kind), Timeout: timeout,
		Action: spec.action, OnAction: spec.onAction,
		Data: toastMeta{kind: kind, detail: spec.detail},
	}
	n.queue = append(n.queue, toastOp{id: id, toast: t})
	wake := n.wake
	n.mu.Unlock()
	if wake != nil {
		wake()
	}
	return id
}

// Success、Info、Warning、Error 是 Show 的简写。
func (n *Notifier) Success(message string, opts ...ToastOption) string {
	return n.Show(ToastSuccess, message, opts...)
}

func (n *Notifier) Info(message string, opts ...ToastOption) string {
	return n.Show(ToastInfo, message, opts...)
}

func (n *Notifier) Warning(message string, opts ...ToastOption) string {
	return n.Show(ToastWarning, message, opts...)
}

// Error 显示错误提示；detail 非空时折叠在「查看详情」后面。
func (n *Notifier) Error(message, detail string, opts ...ToastOption) string {
	if detail != "" {
		opts = append([]ToastOption{WithDetail(detail)}, opts...)
	}
	return n.Show(ToastError, message, opts...)
}

// Undo 显示带「撤销」按钮的提示（渲染层 showUndoToast）：删除这类动作不该先弹确认框打断操作，
// 但也不能删完毫无反应——事后给一个撤销窗口。label 是按钮文字，由调用方给（locales.CommonUndo()）。
func (n *Notifier) Undo(message, label string, onUndo func()) string {
	return n.Show(ToastInfo, message, WithAction(label, onUndo))
}

// Dismiss 收起一条提示：操作重试时只撤下这个操作已经过时的提示，保留其他错误。
// ID 不存在（已自行消失）时什么都不做。
func (n *Notifier) Dismiss(id string) {
	n.mu.Lock()
	n.queue = append(n.queue, toastOp{close: true, id: id})
	wake := n.wake
	n.mu.Unlock()
	if wake != nil {
		wake()
	}
}

// flush 在 UI 线程上领走排队的操作，交给 ui.Context。
func (n *Notifier) flush(c *ui.Context) {
	n.mu.Lock()
	ops := n.queue
	n.queue = nil
	n.mu.Unlock()
	for _, op := range ops {
		if op.close {
			c.CloseToast(op.id)
			delete(n.views, op.id)
			continue
		}
		t := op.toast
		id := op.id
		t.OnClose = func() { delete(n.views, id) }
		c.AddToast(t)
	}
}

// queueLen 返回排队等 UI 线程领走的操作数（测试用）。
func (n *Notifier) queueLen() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.queue)
}

// view 取一条提示的展示状态（不存在就建）；UI 线程。
func (n *Notifier) view(id string) *toastView {
	v := n.views[id]
	if v == nil {
		v = &toastView{}
		n.views[id] = v
	}
	return v
}
