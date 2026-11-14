package main

// 后台完整性复检：干净退出、只是距上次校验超过一周时，不再在启动路径上同步做整库 quick_check
// （1.5 万条 / 379 MB 实测约 5 s，其中约 65% 是全文索引表的一致性检查，期间没有窗口、没有响应）。
// 改为窗口出现、启动期查询跑完之后，在一条独立的原生只读连接上逐表检查。
//
// 取舍（务必知道）：
//   - 逐表的 PRAGMA quick_check(表名) 覆盖该表及其索引的 B 树完整性，但不覆盖整库级别的空闲页链与指针页；
//     这类损坏仍由「崩溃后没有干净关闭标记」的同步整库检查发现并自动修复。
//   - 发现问题不当场修复（修复要独占数据库）：记 integrityProblemFound，退出时不写干净关闭标记，
//     下次启动就按崩溃路径做完整校验与自动修复。
//   - 数据库默认是 WAL 模式（db_wal.go），读写互不阻塞，复检几乎不影响写入。仍保留两道保险以防回落到回滚日志模式
//     （不支持 WAL 的位置）：该模式下读语句持有读锁期间写入的提交要等待，所以逐表检查把单次持锁缩短到最大那张表的耗时，
//     读写连接的 BusyTimeout 另行放宽到 dbWriteBusyTimeout，保证写入是等待而不是失败。
// Background integrity re-check on a dedicated native read-only connection, table by table.

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/plugins/sqlite"
)

const (
	// integrityBgStartDelay 让界面启动期的查询先跑完，再开始占用磁盘。
	integrityBgStartDelay = 5 * time.Second
	// dbWriteBusyTimeout 读写连接等待锁的上限。后台检查期间提交要等当前那张表检查完，
	// 默认 5 s 在大库的全文索引表上可能不够；应用自己的连接之间才有争用（租约已排除其他客户端），放宽是安全的。
	dbWriteBusyTimeout = 30 * time.Second
)

// SQLite 主结果码：SQLITE_CORRUPT(11)、SQLITE_NOTADB(26) 表示文件本身有问题；其余（忙、IO）不能当作"损坏"。
const (
	sqliteCodeCorrupt = 11
	sqliteCodeNotADB  = 26
)

type integrityBackground struct {
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
}

// startBackgroundIntegrityCheck 延迟 delay 后开始复检；返回的句柄用来在退出、换库前停止它。
func startBackgroundIntegrityCheck(parent context.Context, dbPath string, delay time.Duration, now func() time.Time) *integrityBackground {
	ctx, cancel := context.WithCancel(parent)
	bg := &integrityBackground{cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(bg.done)
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return
		}
		runBackgroundIntegrityCheck(ctx, dbPath, now)
	}()
	return bg
}

// Stop 取消复检并等它结束（取消会中断正在执行的语句并关闭它自己的连接）；可重复调用，nil 安全。
func (b *integrityBackground) Stop() {
	if b == nil {
		return
	}
	b.once.Do(b.cancel)
	<-b.done
}

// classifyIntegrityErr 判断一个 SQLite 错误是否表明文件本身损坏。
func classifyIntegrityErr(err error) (corrupt bool) {
	var se *sqlite.Error
	if errors.As(err, &se) {
		return se.Code == sqliteCodeCorrupt || se.Code == sqliteCodeNotADB
	}
	return false
}

// rowsDiagnostics 把 quick_check 的输出拆成逐行诊断（与 quickCheckDiagnostics 同口径：去掉空行与库头）。
func rowsDiagnostics(rows sqlite.Rows) []string {
	var out []string
	for _, row := range rows.Values {
		for _, v := range row {
			s, ok := v.(string)
			if !ok {
				continue
			}
			for _, line := range strings.Split(s, "\n") {
				line = jsTrim(line)
				if line != "" && !quickCheckHeader.MatchString(line) {
					out = append(out, line)
				}
			}
		}
	}
	return out
}

// integrityTablesInOrder 列出要检查的表：业务表在前，全文索引（虚拟表及其影子表）在后，
// 这样业务数据的问题先被发现，最耗时的全文索引表排在最后。
func integrityTablesInOrder(ctx context.Context, db *sqlite.DB) ([]string, error) {
	rows, err := db.Query(ctx, "SELECT name, sql FROM sqlite_master WHERE type = 'table' ORDER BY name")
	if err != nil {
		return nil, err
	}
	type tbl struct {
		name    string
		virtual bool
	}
	var all []tbl
	var virtuals []string
	for _, r := range rows.Values {
		name, _ := r[0].(string)
		sql, _ := r[1].(string)
		v := strings.HasPrefix(strings.ToUpper(strings.TrimSpace(sql)), "CREATE VIRTUAL TABLE")
		all = append(all, tbl{name, v})
		if v {
			virtuals = append(virtuals, name)
		}
	}
	isFts := func(t tbl) bool {
		if t.virtual {
			return true
		}
		for _, v := range virtuals {
			if strings.HasPrefix(t.name, v+"_") {
				return true
			}
		}
		return false
	}
	var first, last []string
	for _, t := range all {
		if isFts(t) {
			last = append(last, t.name)
		} else {
			first = append(first, t.name)
		}
	}
	return append(first, last...), nil
}

// runBackgroundIntegrityCheck 同步执行一次复检（测试直接调用）。结果只通过全局状态体现：
// 全部通过 → 更新 lastIntegrityAtMs；发现损坏 → integrityProblemFound；被取消或结果不确定 → 都不改，下次启动再来。
func runBackgroundIntegrityCheck(ctx context.Context, dbPath string, now func() time.Time) {
	started := now()
	db, err := sqlite.Open(ctx, dbPath, sqlite.OpenOptions{ReadOnly: true})
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("[DB] 后台完整性复检：无法打开数据库（%v），本次不下结论", err)
		}
		return
	}
	defer db.Close()

	var problems []string
	inconclusive := false // 被取消，或遇到「忙 / IO 错误」这类不能当作损坏的失败：不能宣布通过
	// record 记账一次失败：损坏类错误计入 problems 后继续检查其余表；其余失败使结论不确定并停止。
	record := func(what string, err error) (stop bool) {
		switch {
		case ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, sqlite.ErrClosed):
			inconclusive = true // 被取消（退出、换库）
			return true
		case classifyIntegrityErr(err):
			problems = append(problems, fmt.Sprintf("%s：%v", what, err))
			return false
		default:
			log.Printf("[DB] 后台完整性复检：%s 失败（%v），结果不确定", what, err)
			inconclusive = true
			return true
		}
	}

	tables, err := integrityTablesInOrder(ctx, db)
	if err != nil {
		record("读取表清单", err)
	}
	for _, name := range tables {
		if ctx.Err() != nil {
			inconclusive = true
			break
		}
		rows, err := db.Query(ctx, "PRAGMA quick_check("+quoteSqliteIdentifier(name)+")")
		if err != nil {
			if record("检查表 "+name, err) {
				break
			}
			continue
		}
		if diag := rowsDiagnostics(rows); !isHealthyQuickCheck(diag) {
			problems = append(problems, name+"："+strings.Join(diag, "; "))
		}
	}

	// 已发现的损坏总是上报；只有「没发现问题且结论确定」才算通过。
	if len(problems) == 0 && inconclusive {
		return
	}
	if len(problems) > 0 {
		integrityProblemFound.Store(true)
		msg := strings.Join(problems, " | ")
		if r := []rune(msg); len(r) > 500 {
			msg = string(r[:500]) + "…"
		}
		logAppErrorf("db", "后台完整性复检发现问题", "共 %d 处；退出时不写干净关闭标记，下次启动将做完整校验与自动修复：%s", len(problems), msg)
		return
	}
	atomic.StoreInt64(&lastIntegrityAtMs, started.UnixMilli())
	log.Printf("[DB] 后台完整性复检通过：%d 张表，耗时 %dms", len(tables), now().Sub(started).Milliseconds())
}
