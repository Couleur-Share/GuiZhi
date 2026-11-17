package app

import (
	"sync"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/settings"
)

// settingsStore 把 services.Settings 接成设置页的 Store（settingsscreen.Store）。
//
// 界面每帧都读设置，所以值缓存在内存里，不能每帧碰盘；写入经服务落盘，成功后更新缓存；
// 别处（导入、另一入口）改了设置时，经订阅刷新缓存并请求重绘。
// 服务为 nil（测试里不关心设置）时只在内存里读写。
type settingsStore struct {
	svc        services.Settings
	invalidate func()

	mu      sync.Mutex
	cur     settings.Values
	loadErr error // 首次读取失败：界面据此显示加载失败，而不是把默认值当成真实设置
	cancel  func()
}

// newSettingsStore 读一次设置并开始订阅变化。invalidate 在设置被别处改动后调用（可为空）。
func newSettingsStore(svc services.Settings, invalidate func()) *settingsStore {
	s := &settingsStore{svc: svc, invalidate: invalidate, cur: settings.Default()}
	if svc == nil {
		return s
	}
	if v, err := svc.Snapshot(); err != nil {
		s.loadErr = err
	} else {
		s.cur = v
	}
	s.cancel = svc.Subscribe(func(v settings.Values) {
		s.mu.Lock()
		s.cur, s.loadErr = v, nil
		s.mu.Unlock()
		if s.invalidate != nil {
			s.invalidate()
		}
	})
	return s
}

// Values 返回设置的快照（缓存）。
func (s *settingsStore) Values() settings.Values {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cur
}

// LoadError 返回首次读取设置时的错误；读取成功（或之后订阅到了新值）后为 nil。
func (s *settingsStore) LoadError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadErr
}

// Reload 重新读取设置（加载失败界面的「重试」）：成功后缓存换成真实值并清除错误、请求重绘；
// 失败时保留错误（原因随之更新），界面继续停在加载失败状态。服务为 nil 时没有可读的东西，视为成功。
func (s *settingsStore) Reload() error {
	if s.svc == nil {
		return nil
	}
	v, err := s.svc.Snapshot()
	s.mu.Lock()
	if err != nil {
		s.loadErr = err
	} else {
		s.cur, s.loadErr = v, nil
	}
	s.mu.Unlock()
	if err == nil && s.invalidate != nil {
		s.invalidate()
	}
	return err
}

// Update 应用一次修改：经服务落盘，成功后更新缓存；失败时设置保持不变并返回错误。
func (s *settingsStore) Update(mutate func(*settings.Values)) error {
	if s.svc == nil {
		s.mu.Lock()
		mutate(&s.cur)
		s.mu.Unlock()
		return nil
	}
	next, err := s.svc.Update(mutate)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.cur, s.loadErr = next, nil
	s.mu.Unlock()
	return nil
}

// Close 取消订阅。
func (s *settingsStore) Close() {
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
}
