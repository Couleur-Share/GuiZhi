package servicestest

import (
	"context"
	"sync"

	"guizhi-mygo-shell/native/services"
)

// Migration 是 services.Migration 的假实现：按注入的探测结果与迁移结果返回，记录每次调用，可注入失败、可卡住。
// 零值可用（没有旧库）。
//
// 与真实处理器一致的一点：迁移成功后当前库不再是空的，之后的探测不再发现旧库（Detects 仍计数）。
type Migration struct {
	mu        sync.Mutex
	det       services.LegacyDetection
	found     bool
	stats     services.LegacyMigrationStats
	detectErr error
	runErr    error
	detects   int
	runs      []string

	// DetectGate 非 nil 时 DetectLegacy 先等它关闭再返回；RunGate 同理，停在「迁移中」。
	DetectGate chan struct{}
	RunGate    chan struct{}
}

var _ services.Migration = (*Migration)(nil)

// SetDetection 设置之后的探测结果：found 为假表示没有旧库。
func (f *Migration) SetDetection(det services.LegacyDetection, found bool) {
	f.mu.Lock()
	f.det, f.found = det, found
	f.mu.Unlock()
}

// SetStats 设置迁移成功时返回的统计。
func (f *Migration) SetStats(s services.LegacyMigrationStats) {
	f.mu.Lock()
	f.stats = s
	f.mu.Unlock()
}

// FailDetect、FailRun 注入对应动作的错误（nil 取消）。失败的迁移不改变探测结果。
func (f *Migration) FailDetect(err error) { f.mu.Lock(); f.detectErr = err; f.mu.Unlock() }
func (f *Migration) FailRun(err error)    { f.mu.Lock(); f.runErr = err; f.mu.Unlock() }

// Detects 是探测次数；Runs 是每次迁移收到的源路径（按顺序）。
func (f *Migration) Detects() int { f.mu.Lock(); defer f.mu.Unlock(); return f.detects }
func (f *Migration) Runs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.runs...)
}

func (f *Migration) DetectLegacy(ctx context.Context) (services.LegacyDetection, bool, error) {
	f.mu.Lock()
	f.detects++
	det, found, err, gate := f.det, f.found, f.detectErr, f.DetectGate
	f.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return services.LegacyDetection{}, false, ctx.Err()
		}
	}
	if err != nil {
		return services.LegacyDetection{}, false, err
	}
	return det, found, nil
}

func (f *Migration) RunLegacy(ctx context.Context, sourcePath string) (services.LegacyMigrationStats, error) {
	f.mu.Lock()
	f.runs = append(f.runs, sourcePath)
	stats, err, gate := f.stats, f.runErr, f.RunGate
	f.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return services.LegacyMigrationStats{}, ctx.Err()
		}
	}
	if err != nil {
		return services.LegacyMigrationStats{}, err
	}
	f.mu.Lock()
	f.found = false // 当前库已非空：不再发现旧库
	f.mu.Unlock()
	return stats, nil
}
