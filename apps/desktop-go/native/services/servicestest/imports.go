// Package servicestest 提供 native/services 各接口的假实现，供界面测试使用：
// 记录调用、可注入失败、可让响应卡住，并能手动触发变化事件。
//
// Package servicestest holds fakes of the native/services interfaces for UI tests.
package servicestest

import (
	"context"
	"sync"

	"guizhi-mygo-shell/native/services"
)

// Imports 是 services.Imports 的假实现。零值可用。
type Imports struct {
	mu       sync.Mutex
	page     services.ImportPage
	err      error
	gate     chan struct{}
	queries  []services.ImportQuery
	onChange func()
}

var _ services.Imports = (*Imports)(nil)

// Set 设置之后 List 返回的结果。
func (f *Imports) Set(page services.ImportPage, err error) {
	f.mu.Lock()
	f.page, f.err = page, err
	f.mu.Unlock()
}

// Hold 让之后的 List 卡住，直到返回的函数被调用；用来构造「旧响应晚于新响应」的竞态。
func (f *Imports) Hold() (release func()) {
	gate := make(chan struct{})
	f.mu.Lock()
	f.gate = gate
	f.mu.Unlock()
	return func() { close(gate) }
}

// Unhold 取消卡住设置（已卡住的调用不受影响，仍等它的 release）。
func (f *Imports) Unhold() {
	f.mu.Lock()
	f.gate = nil
	f.mu.Unlock()
}

// Queries 返回迄今收到的全部查询。
func (f *Imports) Queries() []services.ImportQuery {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]services.ImportQuery(nil), f.queries...)
}

// Fire 触发一次变化事件；返回是否有订阅者。
func (f *Imports) Fire() bool {
	f.mu.Lock()
	fn := f.onChange
	f.mu.Unlock()
	if fn == nil {
		return false
	}
	fn()
	return true
}

// Subscribed 报告当前是否有订阅者。
func (f *Imports) Subscribed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.onChange != nil
}

func (f *Imports) List(_ context.Context, q services.ImportQuery) (services.ImportPage, error) {
	f.mu.Lock()
	f.queries = append(f.queries, q)
	page, err, gate := f.page, f.err, f.gate
	f.mu.Unlock()
	if gate != nil {
		<-gate
	}
	return page, err
}

func (f *Imports) OnChange(fn func()) func() {
	f.mu.Lock()
	f.onChange = fn
	f.mu.Unlock()
	return func() {
		f.mu.Lock()
		f.onChange = nil
		f.mu.Unlock()
	}
}
