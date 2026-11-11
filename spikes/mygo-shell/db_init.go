package main

// 数据库启动初始化，对应 packages/db/src/init.ts 的 initDatabase：
// 租约 → 完整性检查（含自动修复）→ 升级前备份 → 建表 → 迁移 → 建索引 → 查询统计。
// 用 modernc 单连接完成（原因见 db_migrate.go 头注释），完成后关闭，再由 MyGo 插件打开同一个文件。

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	dbBusyTimeoutMs        = 5000
	integrityRecheckPeriod = 7 * 24 * time.Hour // 兜底全量校验的间隔：正常退出也每周查一次，防外部原因的坏块
	statsDriftFactor       = 3                  // 统计信息过期到这个倍数就重新 ANALYZE
)

var (
	quickCheckHeader  = regexp.MustCompile(`^\*{3} in database .+ \*{3}$`)
	freelistMismatch  = regexp.MustCompile(`^Freelist: size is \d+ but should be \d+$`)
	indexEntryWrong   = regexp.MustCompile(`^wrong # of entries in index (.+)$`)
	lastIntegrityAtMs int64 // 最近一次真正跑过 quick_check 的时刻，正常退出时写进标记文件
)

type dbInitOptions struct {
	BackupsDir string
	Now        func() time.Time
	Alive      func(int) bool // 测试注入；nil 用系统判定
	PID        int            // 测试注入；0 用当前进程
}

// openReadWrite 打开单连接读写 modernc 句柄（外键默认开启，busy_timeout 5s）。
func openReadWrite(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+fmt.Sprintf("?_pragma=busy_timeout(%d)&_pragma=foreign_keys(1)", dbBusyTimeoutMs))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

// quickCheckDiagnostics 把 quick_check 的输出拆成逐行诊断（去掉空行与库头）。
func quickCheckDiagnostics(ctx context.Context, r sqlRunner) ([]string, error) {
	rows, err := queryAllStrings(ctx, r, "PRAGMA quick_check")
	if err != nil {
		return nil, err
	}
	var out []string
	for _, row := range rows {
		for _, v := range row {
			for _, line := range strings.Split(v.String, "\n") {
				line = jsTrim(line)
				if line != "" && !quickCheckHeader.MatchString(line) {
					out = append(out, line)
				}
			}
		}
	}
	return out, nil
}

func isHealthyQuickCheck(d []string) bool { return len(d) == 1 && d[0] == "ok" }

func isFreelistOnly(d []string) bool {
	if len(d) == 0 {
		return false
	}
	for _, x := range d {
		if !freelistMismatch.MatchString(x) {
			return false
		}
	}
	return true
}

func indexOnlyMismatchNames(d []string) []string {
	if len(d) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var names []string
	for _, x := range d {
		m := indexEntryWrong.FindStringSubmatch(x)
		if m == nil || jsTrim(m[1]) == "" {
			return nil
		}
		n := jsTrim(m[1])
		if !seen[n] {
			seen[n] = true
			names = append(names, n)
		}
	}
	return names
}

func integrityError(prefix string, d []string) error {
	msg := strings.Join(d, "; ")
	if r := []rune(msg); len(utf16Units(r)) > 500 {
		msg = string(truncateUTF16(r, 500))
	}
	return fmt.Errorf("%s: %s", prefix, msg)
}

func utf16Units(r []rune) []rune { // 仅用来按 UTF-16 长度截断（JS 的 slice(0, 500)）
	out := make([]rune, 0, len(r))
	for _, c := range r {
		out = append(out, c)
		if c > 0xFFFF {
			out = append(out, 0)
		}
	}
	return out
}

func truncateUTF16(r []rune, limit int) []rune {
	n := 0
	for i, c := range r {
		w := 1
		if c > 0xFFFF {
			w = 2
		}
		if n+w > limit {
			return r[:i]
		}
		n += w
	}
	return r
}

func inspectDatabaseIntegrity(ctx context.Context, dbPath string) ([]string, error) {
	db, err := openReadWrite(dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	return quickCheckDiagnostics(ctx, db)
}

func createIntegrityBackup(dbPath string, now time.Time) (string, error) {
	stamp := now.UTC().Format("2006-01-02T15:04:05.000Z")
	stamp = strings.NewReplacer(":", "-", ".", "-").Replace(stamp)
	p := dbPath + ".integrity-backup-" + stamp
	return p, copyFile(dbPath, p)
}

func repairFreelistIntegrity(ctx context.Context, dbPath string, diag []string, now time.Time) error {
	if !isFreelistOnly(diag) {
		return integrityError("Database integrity check failed", diag)
	}
	backup, err := createIntegrityBackup(dbPath, now)
	if err != nil {
		return err
	}
	db, err := openReadWrite(dbPath)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, "VACUUM")
	db.Close()
	if err != nil {
		return err
	}
	after, err := inspectDatabaseIntegrity(ctx, dbPath)
	if err != nil {
		return err
	}
	if !isHealthyQuickCheck(after) {
		return integrityError("Database integrity repair failed", after)
	}
	log.Printf("[DB] Repaired SQLite freelist metadata; backup=%s", filepath.Base(backup))
	return nil
}

func quoteSqliteIdentifier(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }

func repairIndexIntegrity(ctx context.Context, dbPath string, diag []string, now time.Time) error {
	names := indexOnlyMismatchNames(diag)
	if names == nil {
		return integrityError("Database integrity check failed", diag)
	}
	backup, err := createIntegrityBackup(dbPath, now)
	if err != nil {
		return err
	}
	db, err := openReadWrite(dbPath)
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err == nil {
		err = func() error {
			for _, n := range names {
				var got string
				if e := tx.QueryRowContext(ctx, "SELECT name FROM sqlite_master WHERE type = 'index' AND name = ?", n).Scan(&got); e != nil {
					if errors.Is(e, sql.ErrNoRows) {
						return fmt.Errorf("SQLite index does not exist: %s", n)
					}
					return e
				}
				if _, e := tx.ExecContext(ctx, "REINDEX "+quoteSqliteIdentifier(n)); e != nil {
					return e
				}
			}
			d, e := quickCheckDiagnostics(ctx, tx)
			if e != nil {
				return e
			}
			if !isHealthyQuickCheck(d) {
				return integrityError("Database integrity repair failed", d)
			}
			return nil
		}()
		if err != nil {
			_ = tx.Rollback()
		} else {
			err = tx.Commit()
		}
	}
	db.Close()
	if err != nil {
		return err
	}
	after, err := inspectDatabaseIntegrity(ctx, dbPath)
	if err != nil {
		return err
	}
	if !isHealthyQuickCheck(after) {
		return integrityError("Database integrity repair failed", after)
	}
	log.Printf("[DB] Rebuilt SQLite indexes (%s); backup=%s", strings.Join(names, ", "), filepath.Base(backup))
	return nil
}

func cleanShutdownMarkerPath(dbPath string) string { return dbPath + ".clean" }

// parseIntPrefix 对应 Number.parseInt(raw, 10)：忽略前导空白，取可选符号与连续数字，取不到返回 false。
func parseIntPrefix(raw string) (int64, bool) {
	s := strings.TrimLeft(raw, " \t\r\n\f\v\u00a0\ufeff")
	i := 0
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	start := i
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == start {
		return 0, false
	}
	n, err := strconv.ParseInt(s[:i], 10, 64)
	return n, err == nil
}

// consumeCleanShutdownMarker 读完即删：崩溃或断电时没人写标记，下次读不到，自然全量校验。
func consumeCleanShutdownMarker(dbPath string) (int64, bool) {
	p := cleanShutdownMarkerPath(dbPath)
	raw, err := os.ReadFile(p)
	defer os.Remove(p)
	if err != nil {
		return 0, false
	}
	return parseIntPrefix(string(raw))
}

// writeCleanShutdownMarker 正常退出时调用；写不进去只是下次多校验一遍，不拦退出。
func writeCleanShutdownMarker(dbPath string) {
	if dbPath == "" {
		return
	}
	if err := os.WriteFile(cleanShutdownMarkerPath(dbPath), []byte(strconv.FormatInt(lastIntegrityAtMs, 10)), 0o644); err != nil {
		log.Printf("[DB] 未能写入干净关闭标记：%v", err)
	}
}

// ensureDatabaseIntegrity：只在上次没能正常退出（或距上次校验超过一周）时跑 quick_check，
// 因为它耗时与库体积成正比（2 万条 364MB 约 920ms），会拖慢启动。
func ensureDatabaseIntegrity(ctx context.Context, dbPath string, now func() time.Time) error {
	previous, hadMarker := consumeCleanShutdownMarker(dbPath)
	st, err := os.Stat(dbPath)
	if err != nil || st.Size() == 0 {
		return nil
	}
	if hadMarker && now().UnixMilli()-previous < integrityRecheckPeriod.Milliseconds() {
		lastIntegrityAtMs = previous
		return nil
	}
	started := now()
	diag, err := inspectDatabaseIntegrity(ctx, dbPath)
	if err != nil {
		return err
	}
	lastIntegrityAtMs = started.UnixMilli()
	if time.Since(started) > time.Second {
		log.Printf("[DB] 启动完整性检查耗时 %dms（此期间主进程无响应）", time.Since(started).Milliseconds())
	}
	switch {
	case isHealthyQuickCheck(diag):
		return nil
	case isFreelistOnly(diag):
		return repairFreelistIntegrity(ctx, dbPath, diag, now())
	case indexOnlyMismatchNames(diag) != nil:
		return repairIndexIntegrity(ctx, dbPath, diag, now())
	}
	return integrityError("Database integrity check failed", diag)
}

// ensureQueryPlannerStats：没有 sqlite_stat1 时规划器会误选 idx_items_deleted 并对全部匹配行做临时 B 树排序
// （2 万条列表首屏 272ms → 16ms）；没跑过或行数漂移过大才 ANALYZE。
func ensureQueryPlannerStats(ctx context.Context, r sqlRunner) error {
	var name string
	err := r.QueryRowContext(ctx, "SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'sqlite_stat1'").Scan(&name)
	hasStats := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if hasStats {
		var stat sql.NullString
		e := r.QueryRowContext(ctx, "SELECT stat FROM sqlite_stat1 WHERE tbl = 'knowledge_items' LIMIT 1").Scan(&stat)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		if analyzed, ok := parseIntPrefix(stat.String); ok { // 每条 stat 的首个数字是 ANALYZE 当时的表行数
			var actual int64
			if e := r.QueryRowContext(ctx, "SELECT COUNT(*) AS count FROM knowledge_items").Scan(&actual); e != nil {
				return e
			}
			base := analyzed
			if base < 1 {
				base = 1
			}
			drifted := actual > base*statsDriftFactor || actual*statsDriftFactor < analyzed
			if !drifted {
				return nil
			}
		}
	}
	started := time.Now()
	if _, err := r.ExecContext(ctx, "ANALYZE"); err != nil {
		return err
	}
	log.Printf("[DB] 已刷新查询规划统计信息（%dms）", time.Since(started).Milliseconds())
	return nil
}

// backupPendingSchemaUpgrade 对应 backup.ts：存在待执行迁移时先留一份 pre-update 备份；失败必须阻止结构升级。
func backupPendingSchemaUpgrade(ctx context.Context, conn *sql.Conn, dbPath, backupsDir string, now func() time.Time) error {
	def, err := getTableDefinition(ctx, conn, "knowledge_items")
	if err != nil || def == "" {
		return err
	}
	applied := map[string]bool{}
	if tdef, err := getTableDefinition(ctx, conn, "schema_migrations"); err != nil {
		return err
	} else if tdef != "" {
		rows, err := queryAllStrings(ctx, conn, "SELECT name FROM schema_migrations")
		if err != nil {
			return err
		}
		for _, r := range rows {
			applied[r[0].String] = true
		}
	}
	pending := false
	for _, m := range dbMigrations {
		if !applied[m.name] {
			pending = true
			break
		}
	}
	if !pending {
		return nil
	}
	lb := &localBackups{dbPath: dbPath, backupsDir: backupsDir, now: now}
	if _, err := lb.create("pre-update"); err != nil {
		return fmt.Errorf("升级前备份失败，已中止结构升级：%w", err)
	}
	return nil
}

// initDatabaseFile 把数据库文件初始化到当前结构，返回持有的租约（进程生命周期内保持，退出时 release）。
func initDatabaseFile(ctx context.Context, dbPath string, opt dbInitOptions) (lease *dbLease, err error) {
	if opt.Now == nil {
		opt.Now = time.Now
	}
	if opt.PID == 0 {
		opt.PID = os.Getpid()
	}
	if err = os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, err
	}
	started := opt.Now()
	if lease, err = acquireDatabaseClientLease(dbPath, opt.PID, opt.Alive); err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			lease.release()
			lease = nil
		}
	}()
	if err = ensureDatabaseIntegrity(ctx, dbPath, opt.Now); err != nil {
		return
	}
	db, err := openReadWrite(dbPath)
	if err != nil {
		return
	}
	defer db.Close()
	conn, err := db.Conn(ctx)
	if err != nil {
		return
	}
	defer conn.Close()

	if opt.BackupsDir != "" {
		if err = backupPendingSchemaUpgrade(ctx, conn, dbPath, opt.BackupsDir, opt.Now); err != nil {
			return
		}
	}
	// 只建表（索引要等迁移补齐列之后）
	if _, err = conn.ExecContext(ctx, schemaTables); err != nil {
		return
	}
	executed, err := runMigrations(ctx, conn)
	if err != nil {
		return
	}
	if len(executed) > 0 {
		log.Printf("[DB] 已应用 schema 迁移: %s", strings.Join(executed, ", "))
	}
	if strings.TrimSpace(schemaIndexes) != "" {
		if _, err = conn.ExecContext(ctx, schemaIndexes); err != nil {
			return
		}
	}
	if err = ensureQueryPlannerStats(ctx, conn); err != nil {
		return
	}
	log.Printf("Database initialized at: %s (%dms)", dbPath, opt.Now().Sub(started).Milliseconds())
	return lease, nil
}
