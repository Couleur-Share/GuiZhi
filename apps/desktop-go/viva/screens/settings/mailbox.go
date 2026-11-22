package settings

import "sync"

// mailbox 把后台线程里的结果交回窗口线程：界面状态只能在窗口线程上改，而「测试连接」「检查更新」要走网络，
// 状态推送也来自别的 goroutine。后台只 post，分区在每帧开头 drain 领走并执行；close 之后（分区被切走）的投递一律丢弃，
// 所以切走分区后不会再有回调去改一个已经不在画面上的状态。零值可用。
type mailbox struct {
	mu     sync.Mutex
	fns    []func()
	closed bool
}

// post 投递一件待办并请求重绘（wake 可为 nil）。任意 goroutine 可调用。
func (m *mailbox) post(wake func(), fn func()) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.fns = append(m.fns, fn)
	m.mu.Unlock()
	if wake != nil {
		wake()
	}
}

// drain 领走并执行已投递的待办（按投递顺序）。只在窗口线程调用。
func (m *mailbox) drain() {
	m.mu.Lock()
	fns := m.fns
	m.fns = nil
	m.mu.Unlock()
	for _, fn := range fns {
		fn()
	}
}

// close 丢弃未执行的待办并拒收之后的投递。
func (m *mailbox) close() {
	m.mu.Lock()
	m.closed, m.fns = true, nil
	m.mu.Unlock()
}
