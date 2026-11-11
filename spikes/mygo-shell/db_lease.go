package main

// 数据库客户端租约，对应 packages/db/src/database-client-lock.ts。
//
// 每个访问数据库的进程在 <db>.clients/<pid>.json 登记自己；启动时扫描他人的租约：
// 进程已不存在的清掉（崩溃残留），仍存活的视为「另一个归知实例正在使用」。
// TS 版只用租约决定要不要回收 wasm 驱动遗留的 <db>.lock 目录；Go 壳层用原生 SQLite，
// 与旧版 wasm 驱动的锁机制互不兼容（见迁移文档「已知阻塞与风险」第 3 条），所以这里更严格：
// 发现存活的其他客户端就拒绝启动，避免新旧版本同时写同一个库。

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

type dbLeaseRecord struct {
	PID          int    `json:"pid"`
	RegisteredAt string `json:"registeredAt"`
}

// dbLease 是一份已登记的租约。
type dbLease struct {
	leasePath, clientsDir string
	released              bool
}

// errLiveDatabaseClient 表示另一个存活的归知进程正在使用该数据库。
type errLiveDatabaseClient struct {
	PIDs []int
}

func (e *errLiveDatabaseClient) Error() string {
	return fmt.Sprintf("数据库正被另一个归知进程使用（pid %v）。请先退出旧版归知或其它实例后再启动", e.PIDs)
}

func readLeasePID(path string) (int, bool) {
	st, err := os.Lstat(path)
	if err != nil || !st.Mode().IsRegular() {
		return 0, false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	var rec struct {
		PID any `json:"pid"`
	}
	if json.Unmarshal(raw, &rec) != nil {
		return 0, false
	}
	f, ok := rec.PID.(float64)
	if !ok || f != float64(int(f)) || f <= 0 {
		return 0, false
	}
	return int(f), true
}

// acquireDatabaseClientLease 登记本进程的租约，并清理死进程残留；有存活的其他客户端时登记后立即撤销并返回 *errLiveDatabaseClient。
func acquireDatabaseClientLease(dbPath string, pid int, alive func(int) bool) (*dbLease, error) {
	if alive == nil {
		alive = processAlive
	}
	clientsDir := dbPath + ".clients"
	if err := os.MkdirAll(clientsDir, 0o700); err != nil {
		return nil, err
	}
	st, err := os.Lstat(clientsDir)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("Invalid database clients directory: %s", clientsDir)
	}
	leasePath := filepath.Join(clientsDir, strconv.Itoa(pid)+".json")
	_ = os.Remove(leasePath)
	rec, _ := json.Marshal(dbLeaseRecord{PID: pid, RegisteredAt: time.Now().UTC().Format("2006-01-02T15:04:05.000Z")})
	f, err := os.OpenFile(leasePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	_, werr := f.Write(rec)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	lease := &dbLease{leasePath: leasePath, clientsDir: clientsDir}
	if werr != nil {
		lease.release()
		return nil, werr
	}

	var live []int
	entries, err := os.ReadDir(clientsDir)
	if err != nil {
		lease.release()
		return nil, err
	}
	for _, e := range entries {
		p := filepath.Join(clientsDir, e.Name())
		other, ok := 0, false
		if e.Type().IsRegular() {
			other, ok = readLeasePID(p)
		}
		if ok && e.Name() != strconv.Itoa(other)+".json" { // 文件名与内容不符：残留，清掉
			_ = os.Remove(p)
			continue
		}
		if ok && other == pid {
			continue
		}
		if !ok || !alive(other) {
			_ = os.Remove(p)
			continue
		}
		live = append(live, other)
	}
	if len(live) > 0 {
		lease.release()
		return nil, &errLiveDatabaseClient{PIDs: live}
	}
	return lease, nil
}

func (l *dbLease) release() {
	if l == nil || l.released {
		return
	}
	l.released = true
	_ = os.Remove(l.leasePath)
	_ = os.Remove(l.clientsDir) // 仍有其他条目时删不掉，正好
}
