package settings

import (
	"sync"

	"guizhi-mygo-shell/native/settings"
)

// MemStore 是内存里的设置存取：对照工具与无头会话测试用，从给定的初始值开始，写入立即生效。
// FailNext 非空时下一次 Update 返回该错误且不改值，用来测试写入失败时界面的反应。
type MemStore struct {
	mu       sync.Mutex
	v        settings.Values
	FailNext error
	Updates  int // 成功写入的次数
}

// NewMemStore 以 v 为初始值创建存取。
func NewMemStore(v settings.Values) *MemStore { return &MemStore{v: v} }

// Values 返回当前值的快照。
func (m *MemStore) Values() settings.Values {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.v
}

// Update 应用修改。
func (m *MemStore) Update(mutate func(*settings.Values)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.FailNext; err != nil {
		m.FailNext = nil
		return err
	}
	mutate(&m.v)
	m.Updates++
	return nil
}
