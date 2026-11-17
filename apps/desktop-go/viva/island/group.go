package island

import (
	"sync"

	"github.com/vivaui/viva/app"
)

// Closer 是能被 Group 收拢的东西：*Island 满足；测试可以登记假实现，不必真开 WebView2。
type Closer interface{ Close() }

// Group 跟踪一个窗口开过的岛，窗口收到托盘时一起销毁：WebView2 的浏览器与渲染进程只在窗口可见时存在
// （计划 §4.7、§10.9：收托盘前必须先销毁，托盘态的空闲内存才不含任何 WebView）。
// 岛仍由开它的界面持有；界面在窗口再次显示时按需重开（岛里没有需要跨越隐藏保留的状态——不可信页面不该有）。
type Group struct {
	mu   sync.Mutex
	open map[Closer]struct{}
}

// Open 在窗口线程上打开一个岛并登记；之后 CloseAll 会把它一起销毁。
func (g *Group) Open(host *app.WebHost, cfg Config) (*Island, error) {
	i, err := Open(host, cfg)
	if err != nil {
		return nil, err
	}
	g.Track(i)
	return i, nil
}

// Track 登记一个已开的岛（测试或别处开的岛也走这里）。c 必须是指针之类可作映射键的值。
func (g *Group) Track(c Closer) {
	if c == nil {
		return
	}
	g.mu.Lock()
	if g.open == nil {
		g.open = map[Closer]struct{}{}
	}
	g.open[c] = struct{}{}
	g.mu.Unlock()
}

// Forget 把一个岛从登记里去掉（界面自己关了它之后调用，避免登记表越攒越大）。
func (g *Group) Forget(c Closer) {
	g.mu.Lock()
	delete(g.open, c)
	g.mu.Unlock()
}

// CloseAll 销毁登记里的全部岛并清空登记，返回销毁的个数。窗口线程调用。
func (g *Group) CloseAll() int {
	g.mu.Lock()
	closers := make([]Closer, 0, len(g.open))
	for c := range g.open {
		closers = append(closers, c)
	}
	g.open = nil
	g.mu.Unlock()
	for _, c := range closers {
		c.Close()
	}
	return len(closers)
}

// Len 是登记着的岛数（测试用）。
func (g *Group) Len() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.open)
}
