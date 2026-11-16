package kit

import (
	"strconv"
	"sync"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
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
	// toastMaxVisible 是同时存在的提示上限：不自动消失就得有上限，否则堆满屏幕（渲染层 MAX_VISIBLE_TOASTS）。
	toastMaxVisible = 5
	// toastUnmountDelay 是提示被关掉之后到真正卸载的时间：渲染层是 MOTION_DURATION.quick + 20 ms（常量 120 + 20，
	// 与动效档位无关），期间提示留在版面里播放退场动画。
	toastUnmountDelay = 140 * time.Millisecond
	// toastCopiedFor 是「已复制」的显示时长（渲染层 ToastDetail 的 setTimeout 2000）。
	toastCopiedFor = 2 * time.Second
)

// toastSpec 是一次 Show 的可选项。
type toastSpec struct {
	detail   string
	action   string
	onAction func()
}

// ToastOption 调整一条提示。
type ToastOption func(*toastSpec)

// WithDetail 给提示附上完整报错原文：折叠在「查看详情」后面，展开能读、能复制去搜。
// 提示语要短才读得进去，但「失败」两个字对排查毫无用处——原因收在这里。
// 批量操作把逐条失败明细放这里最合适。
func WithDetail(detail string) ToastOption { return func(s *toastSpec) { s.detail = detail } }

// WithAction 给提示加一个行内动作按钮（如「撤销」），点击后执行 fn 并立即收起提示。
// fn 在窗口线程执行。带动作的提示用撤销窗口时长（8 秒），与类别无关（渲染层 showUndoToast）。
func WithAction(label string, fn func()) ToastOption {
	return func(s *toastSpec) { s.action, s.onAction = label, fn }
}

// toastOp 是排队等窗口线程执行的一次操作：新增一条，或收起一条。
type toastOp struct {
	close   bool
	id      string
	seq     uint64
	kind    ToastKind
	message string
	spec    toastSpec
}

// toast 是一条提示在窗口线程上的状态。
type toast struct {
	id       string
	seq      uint64
	kind     ToastKind
	message  string
	detail   string
	action   string
	onAction func()

	timeout time.Duration // 自动消失的时长；负数表示留到手动关闭
	born    time.Duration // 出现的帧时间

	leaving bool
	leaveAt time.Duration // 开始退场的帧时间

	// painted：已经画过一帧。入场动画的参数只在第一帧起作用，之后每一帧改交退场的参数——
	// Viva 的退场按「最后一帧画出来时」的参数回放（见 toast_view.go）。
	painted bool
	// size 是上一帧量到的外框尺寸：退场回放期间卡片不再被画，版面里用同样大小的空位占着（渲染层的卡片此时仍在文档流里）。
	size gfx.Point

	expanded    bool            // 详情展开
	copied      bool            // 「已复制」正在显示
	copyExpires []time.Duration // 还没到点的复制定时器（渲染层每次点击各起一个，先到点的先把「已复制」收掉）
}

// Notifier 是应用的提示通道：任何 goroutine 都能 Show，提示在下一帧由 Look.Toasts 画出。
//
// 为什么要排队：提示的状态只能在窗口线程上改（画面是同一个线程画的）；后台任务失败、守卫里的失败要从别的
// goroutine 报上来，所以先入队、请求重绘、再由宿主每帧领走。
type Notifier struct {
	mu    sync.Mutex
	seq   uint64
	queue []toastOp
	wake  func()

	// Clipboard 写系统剪贴板；nil 时用 app.SetClipboardText。测试里换成记录用的函数，不碰真实剪贴板。
	Clipboard func(text string) error

	// 仅窗口线程读写
	toasts []*toast
}

// NewNotifier 创建提示通道。wake 请求窗口重绘（可在任意 goroutine 调用）；nil 时由调用方自己驱动帧。
func NewNotifier(wake func()) *Notifier { return &Notifier{wake: wake} }

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
	n.mu.Lock()
	n.seq++
	seq := n.seq
	id := "gz-toast-" + strconv.FormatUint(seq, 10)
	n.queue = append(n.queue, toastOp{id: id, seq: seq, kind: kind, message: message, spec: spec})
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

// Len 返回窗口线程上现有的提示条数（含正在退场、尚未卸载的），测试用。
func (n *Notifier) Len() int { return len(n.toasts) }

// copyText 把文字写进剪贴板。
func (n *Notifier) copyText(text string) error {
	if n.Clipboard != nil {
		return n.Clipboard(text)
	}
	return app.SetClipboardText(text)
}

// advance 推进到帧时间 now：领走排队的操作，处理到点的自动消失、卸载与「已复制」，
// 返回下一个需要重绘的时刻与它是否存在（供宿主 RequestFrameIn）。只在窗口线程调用。
func (n *Notifier) advance(now time.Duration) (next time.Duration, ok bool) {
	n.mu.Lock()
	ops := n.queue
	n.queue = nil
	n.mu.Unlock()
	for _, op := range ops {
		if op.close {
			if t := n.find(op.id); t != nil {
				t.startLeave(now)
			}
			continue
		}
		n.add(op, now)
	}

	kept := n.toasts[:0]
	for _, t := range n.toasts {
		if !t.leaving && t.timeout >= 0 && now >= t.born+t.timeout {
			t.startLeave(now)
		}
		if t.leaving && now >= t.leaveAt+toastUnmountDelay {
			continue // 卸载
		}
		live := t.copyExpires[:0]
		for _, at := range t.copyExpires {
			if now >= at {
				t.copied = false // 先到点的定时器先把「已复制」收掉，即使后面还有没到点的
			} else {
				live = append(live, at)
			}
		}
		t.copyExpires = live
		kept = append(kept, t)
	}
	clear(n.toasts[len(kept):])
	n.toasts = kept

	consider := func(at time.Duration) {
		if !ok || at < next {
			next, ok = at, true
		}
	}
	for _, t := range n.toasts {
		switch {
		case t.leaving:
			consider(t.leaveAt + toastUnmountDelay)
		case t.timeout >= 0:
			consider(t.born + t.timeout)
		}
		for _, at := range t.copyExpires {
			consider(at)
		}
	}
	return next, ok
}

// add 加入一条新提示，并按渲染层 trimToasts 的规则把总数压回上限：优先挤掉最老的「会自己消失」的那条，
// 一律砍最老的话，批量导入 10 个文件全失败时，前 5 条错误会在用户读到之前就被后来的挤没——而它们恰恰是不该消失的那类。
func (n *Notifier) add(op toastOp, now time.Duration) {
	t := &toast{
		id: op.id, seq: op.seq, kind: op.kind, message: op.message,
		detail: op.spec.detail, action: op.spec.action, onAction: op.spec.onAction,
		born: now,
	}
	switch {
	case op.spec.action != "":
		t.timeout = toastUndoWindow
	case op.kind.persistent():
		t.timeout = -1
	default:
		t.timeout = toastAutoDismiss
	}
	n.toasts = append(n.toasts, t)
	if len(n.toasts) <= toastMaxVisible {
		return
	}
	drop := 0
	for i, old := range n.toasts {
		if !old.kind.persistent() {
			drop = i
			break
		}
	}
	n.toasts = append(n.toasts[:drop], n.toasts[drop+1:]...)
}

func (n *Notifier) find(id string) *toast {
	for _, t := range n.toasts {
		if t.id == id {
			return t
		}
	}
	return nil
}

// startLeave 让提示开始退场；已经在退场的不重来（渲染层 removeToast 同样不重置退场定时器，避免动画抖动）。
func (t *toast) startLeave(now time.Duration) {
	if t.leaving {
		return
	}
	t.leaving, t.leaveAt = true, now
}
