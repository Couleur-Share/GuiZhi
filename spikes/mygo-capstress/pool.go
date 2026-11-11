package main

import (
	"fmt"
	"sync"
	"time"
)

// 采集页池：采集进程里预先建好的隐藏 MyGo 窗口。它们的 window.open 由宿主层拒绝（见 child 里的处理器），
// 而 CDP 的 Target.createTarget 新建的页面不经过这个处理器。窗口出问题（卡死、渲染进程崩溃）时整个换掉。

type pool struct {
	b      *browser
	ch     chan string
	mu     sync.Mutex
	size   int
	recycl int // 已回收（关闭并重建）的窗口数
	uses   map[string]int
	maxUse int // 每个窗口最多服务多少个任务，0 表示不限
}

// newWindow 让采集进程新建一个隐藏窗口，并返回它的 target。
func (b *browser) newWindow() (string, error) {
	b.winMu.Lock() // 并发新建会让“比较前后 target 差异”认错窗口，必须串行
	defer b.winMu.Unlock()
	before, err := b.targets()
	if err != nil {
		return "", err
	}
	known := map[string]bool{}
	for _, t := range before {
		known[t.TargetID] = true
	}
	if _, err := fmt.Fprintln(b.stdin, "NEW"); err != nil {
		return "", err
	}
	select {
	case <-b.winCh:
	case <-time.After(15 * time.Second):
		return "", fmt.Errorf("新建窗口超时")
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		ts, err := b.targets()
		if err != nil {
			return "", err
		}
		for _, t := range ts {
			if t.Type == "page" && !known[t.TargetID] {
				return t.TargetID, nil
			}
		}
		time.Sleep(30 * time.Millisecond)
	}
	return "", fmt.Errorf("没有出现新窗口的 target")
}

func (b *browser) makePool(n int) error {
	p := &pool{b: b, ch: make(chan string, n+8), size: n, uses: map[string]int{}, maxUse: poolMaxUse}
	for i := 0; i < n; i++ {
		tid, err := b.newWindow()
		if err != nil {
			return err
		}
		p.ch <- tid
	}
	b.pool = p
	return nil
}

func (p *pool) acquire() string { return <-p.ch }

// release 归还窗口；unhealthy 时关闭它并换一个新的。
var poolMaxUse int

func (p *pool) release(tid string, unhealthy bool) {
	p.mu.Lock()
	p.uses[tid]++
	worn := p.maxUse > 0 && p.uses[tid] >= p.maxUse
	p.mu.Unlock()
	if !unhealthy && !worn {
		p.ch <- tid
		return
	}
	p.b.d.callT("", "Target.closeTarget", map[string]any{"targetId": tid}, 5*time.Second)
	nt, err := p.b.newWindow()
	p.mu.Lock()
	p.recycl++
	p.mu.Unlock()
	if err != nil {
		fmt.Println("       警告：重建窗口失败：", err)
		return
	}
	p.ch <- nt
}
