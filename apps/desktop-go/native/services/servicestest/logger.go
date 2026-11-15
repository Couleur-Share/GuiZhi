package servicestest

import (
	"sync"

	"guizhi-mygo-shell/native/services"
)

// LogEntry 是 Logger 记下的一条错误日志。
type LogEntry struct{ Scope, Action, Message string }

// Logger 是 services.Logger 的假实现：按顺序记下所有调用。零值可用。
type Logger struct {
	mu      sync.Mutex
	entries []LogEntry
}

var _ services.Logger = (*Logger)(nil)

// AppError 记下一条。
func (l *Logger) AppError(scope, action, message string) {
	l.mu.Lock()
	l.entries = append(l.entries, LogEntry{scope, action, message})
	l.mu.Unlock()
}

// Entries 返回迄今记下的全部日志（副本）。
func (l *Logger) Entries() []LogEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]LogEntry(nil), l.entries...)
}
