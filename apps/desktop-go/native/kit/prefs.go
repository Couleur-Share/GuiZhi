package kit

import (
	"encoding/json"
	"sync"
)

// PrefStore 是界面偏好的键值存取，值以 JSON 保存。
//
// 窗格宽度、表格列这类持久化件只依赖这个窄接口：真实实现由 Go 侧设置（settings-go）提供，
// 测试与开发用 MemStore。键是点分命名（如 "ui.itemListPaneWidth"），由使用方决定。
// 实现必须可在任意 goroutine 调用：持久化件会在去抖定时器的 goroutine 里写入。
//
// PrefStore is a JSON key-value store for UI preferences.
type PrefStore interface {
	// Get 返回键保存的 JSON；没保存过返回 false。
	Get(key string) (json.RawMessage, bool)
	// Set 把 value 编码成 JSON 保存。
	Set(key string, value any) error
}

// MemStore 是并发安全的内存实现，供测试与开发用。
type MemStore struct {
	mu     sync.Mutex
	m      map[string]json.RawMessage
	writes int
	// FailSet 非 nil 时，Set 不保存并返回它：用来测保存失败的路径。
	FailSet error
}

// Get 实现 PrefStore。
func (s *MemStore) Get(key string) (json.RawMessage, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[key]
	return v, ok
}

// Set 实现 PrefStore。
func (s *MemStore) Set(key string, value any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.FailSet != nil {
		return s.FailSet
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if s.m == nil {
		s.m = map[string]json.RawMessage{}
	}
	s.m[key] = raw
	s.writes++
	return nil
}

// Put 直接放入一段原始 JSON（含不合规的内容），用来模拟旧版本写下的脏数据。
func (s *MemStore) Put(key, raw string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = map[string]json.RawMessage{}
	}
	s.m[key] = json.RawMessage(raw)
}

// Writes 返回 Set 成功保存的次数。
func (s *MemStore) Writes() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writes
}
