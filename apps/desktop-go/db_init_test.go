package main

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

// 数据库初始化/迁移与 TS（init.ts + migrations.ts）的整库转储差分；golden 由 spikes/mygo-db/golden-schema.mts 生成。

type schemaDump struct {
	UserVersion int64   `json:"userVersion"`
	Schema      [][]any `json:"schema"`
	Data        map[string]struct {
		Columns []string `json:"columns"`
		Rows    [][]any  `json:"rows"`
	} `json:"data"`
}

func dumpDatabase(t *testing.T, dbPath string) schemaDump {
	t.Helper()
	ctx := context.Background()
	db, err := openReadOnly(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	q := func(query string) ([]string, [][]any) {
		rows, err := db.QueryContext(ctx, query)
		if err != nil {
			return nil, nil
		}
		defer rows.Close()
		cols, _ := rows.Columns()
		out := [][]any{}
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				t.Fatal(err)
			}
			for i, v := range vals {
				if b, ok := v.([]byte); ok {
					vals[i] = "hex:" + hex.EncodeToString(b)
				}
			}
			out = append(out, vals)
		}
		return cols, out
	}
	_, master := q("SELECT type, name, tbl_name, sql FROM sqlite_master ORDER BY type, name")
	shadow := map[string]bool{}
	for _, r := range master {
		if s, ok := r[3].(string); ok && strings.HasPrefix(strings.ToUpper(s), "CREATE VIRTUAL TABLE") {
			for _, suf := range []string{"data", "idx", "content", "docsize", "config"} {
				shadow[fmt.Sprintf("%v_%s", r[1], suf)] = true
			}
		}
	}
	d := schemaDump{Data: map[string]struct {
		Columns []string `json:"columns"`
		Rows    [][]any  `json:"rows"`
	}{}}
	for _, r := range master {
		name := r[1].(string)
		if shadow[name] {
			continue
		}
		d.Schema = append(d.Schema, []any{r[0], r[1], r[2], r[3]})
		if r[0] != "table" {
			continue
		}
		cols, _ := q(`SELECT name FROM pragma_table_info('` + strings.ReplaceAll(name, "'", "''") + `')`)
		_ = cols
		_, infoRows := q(`SELECT name FROM pragma_table_info('` + strings.ReplaceAll(name, "'", "''") + `')`)
		var columns []string
		for _, ir := range infoRows {
			columns = append(columns, ir[0].(string))
		}
		rowsData := [][]any{}
		if name == "schema_migrations" {
			columns = []string{"name"}
			_, rowsData = q("SELECT name FROM schema_migrations ORDER BY name")
		} else {
			if _, rd := q(`SELECT * FROM "` + name + `" ORDER BY rowid`); len(rd) > 0 || tableHasRowid(db, name) {
				rowsData = rd
			} else {
				_, rowsData = q(`SELECT * FROM "` + name + `"`)
			}
		}
		d.Data[name] = struct {
			Columns []string `json:"columns"`
			Rows    [][]any  `json:"rows"`
		}{columns, rowsData}
	}
	var uv int64
	_ = db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&uv)
	d.UserVersion = uv
	return d
}

// normalize 让 Go 与 JSON 解码结果可比：整数/浮点都走一遍 JSON。
func normalizeJSON(t *testing.T, v any) any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestMigrationListMatchesTS(t *testing.T) {
	var names []string
	for _, m := range dbMigrations {
		names = append(names, m.name)
	}
	if !reflect.DeepEqual(names, migrationNames) {
		t.Fatalf("Go 迁移清单与 TS 不一致\n go: %v\n ts: %v", names, migrationNames)
	}
	if supportedSchemaVersion != len(migrationNames) {
		t.Fatalf("supportedSchemaVersion=%d，清单 %d", supportedSchemaVersion, len(migrationNames))
	}
}

func TestDatabaseInitParity(t *testing.T) {
	dir := os.Getenv("GUIZHI_GOLDEN_DIR")
	if dir == "" {
		t.Skip("未设置 GUIZHI_GOLDEN_DIR")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	var g struct {
		Schema struct {
			Cases map[string]struct {
				Pre  *string    `json:"pre"`
				Post schemaDump `json:"post"`
			} `json:"cases"`
			MigrationNames []string `json:"migrationNames"`
		} `json:"schema"`
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	if len(g.Schema.Cases) == 0 {
		t.Fatal("golden 缺少 schema 段")
	}
	if !reflect.DeepEqual(g.Schema.MigrationNames, migrationNames) {
		t.Fatal("golden 的迁移清单与 schema_gen.go 不一致，请重新生成 gen-schema.mts")
	}
	for name, c := range g.Schema.Cases {
		t.Run(name, func(t *testing.T) {
			work := filepath.Join(t.TempDir(), "knowledge.db")
			if c.Pre != nil {
				data, err := os.ReadFile(filepath.Join(dir, "schema-golden", *c.Pre))
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(work, data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			lease, err := initDatabaseFile(context.Background(), work, dbInitOptions{BackupsDir: filepath.Join(filepath.Dir(work), "backups")})
			if err != nil {
				t.Fatal(err)
			}
			lease.release()
			got := dumpDatabase(t, work)
			if got.UserVersion != c.Post.UserVersion {
				t.Errorf("user_version go=%d ts=%d", got.UserVersion, c.Post.UserVersion)
			}
			gs, ws := normalizeJSON(t, got.Schema), normalizeJSON(t, c.Post.Schema)
			if !reflect.DeepEqual(gs, ws) {
				ga, wa := gs.([]any), ws.([]any)
				t.Errorf("结构条数 go=%d ts=%d", len(ga), len(wa))
				seen := map[string]bool{}
				for _, x := range wa {
					seen[fmt.Sprint(x)] = true
				}
				shown := 0
				for _, x := range ga {
					if !seen[fmt.Sprint(x)] && shown < 6 {
						t.Errorf("Go 独有或不同：%v", x)
						shown++
					}
				}
				seen = map[string]bool{}
				for _, x := range ga {
					seen[fmt.Sprint(x)] = true
				}
				shown = 0
				for _, x := range wa {
					if !seen[fmt.Sprint(x)] && shown < 6 {
						t.Errorf("TS 独有或不同：%v", x)
						shown++
					}
				}
			}
			for table, want := range c.Post.Data {
				have, ok := got.Data[table]
				if !ok {
					t.Errorf("缺少表 %s", table)
					continue
				}
				if !reflect.DeepEqual(have.Columns, want.Columns) {
					t.Errorf("%s 列不一致 go=%v ts=%v", table, have.Columns, want.Columns)
					continue
				}
				hr, wr := normalizeJSON(t, have.Rows), normalizeJSON(t, want.Rows)
				if strings.HasPrefix(table, "sqlite_stat") { // ANALYZE 按哈希表顺序写行，行序随构建而异，按内容排序后比较
					hr, wr = sortedRows(hr), sortedRows(wr)
				}
				if !reflect.DeepEqual(hr, wr) {
					t.Errorf("%s 数据不一致\n go: %s\n ts: %s", table, short(have.Rows), short(want.Rows))
				}
			}
			for table := range got.Data {
				if _, ok := c.Post.Data[table]; !ok {
					t.Errorf("Go 多出表 %s", table)
				}
			}
		})
	}
}

func sortedRows(v any) any {
	rows, _ := v.([]any)
	out := append([]any{}, rows...)
	sort.Slice(out, func(i, j int) bool { return fmt.Sprint(out[i]) < fmt.Sprint(out[j]) })
	return out
}

func tableHasRowid(db *sql.DB, name string) bool {
	rows, err := db.Query(`SELECT 1 FROM "` + name + `" ORDER BY rowid LIMIT 0`)
	if err != nil {
		return false
	}
	rows.Close()
	return true
}

func short(v any) string {
	raw, _ := json.Marshal(v)
	s := string(raw)
	if len(s) > 600 {
		s = s[:600] + "…"
	}
	return s
}

// ── 手写测试 ──

func TestMigrationsAreIdempotentAndResumable(t *testing.T) {
	work := filepath.Join(t.TempDir(), "knowledge.db")
	ctx := context.Background()
	lease, err := initDatabaseFile(ctx, work, dbInitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	lease.release()
	// 模拟「执行到一半崩溃」：抹掉后 10 条记录，再初始化应只补这 10 条
	db, err := openReadWrite(work)
	if err != nil {
		t.Fatal(err)
	}
	tail := migrationNames[len(migrationNames)-10:]
	for _, n := range tail {
		if _, err := db.Exec("DELETE FROM schema_migrations WHERE name = ?", n); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	lease, err = initDatabaseFile(ctx, work, dbInitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	lease.release()
	got := dumpDatabase(t, work)
	if got.UserVersion != int64(len(migrationNames)) {
		t.Fatalf("user_version=%d", got.UserVersion)
	}
	if n := len(got.Data["schema_migrations"].Rows); n != len(migrationNames) {
		t.Fatalf("schema_migrations 行数 %d，应为 %d", n, len(migrationNames))
	}
}

func TestInitCreatesPreUpdateBackupOnlyWhenMigrationsPending(t *testing.T) {
	dir := t.TempDir()
	work := filepath.Join(dir, "knowledge.db")
	backups := filepath.Join(dir, "backups")
	ctx := context.Background()
	count := func() int {
		entries, _ := os.ReadDir(backups)
		n := 0
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), "knowledge-pre-update-") {
				n++
			}
		}
		return n
	}
	l, err := initDatabaseFile(ctx, work, dbInitOptions{BackupsDir: backups})
	if err != nil {
		t.Fatal(err)
	}
	l.release()
	if count() != 0 {
		t.Fatal("全新数据库没有需要保护的旧数据，不应产生 pre-update 备份")
	}
	l, err = initDatabaseFile(ctx, work, dbInitOptions{BackupsDir: backups})
	if err != nil {
		t.Fatal(err)
	}
	l.release()
	if count() != 0 {
		t.Fatal("已是最新结构，不应产生备份")
	}
	db, _ := openReadWrite(work)
	_, err = db.Exec("DELETE FROM schema_migrations WHERE name = ?", migrationNames[len(migrationNames)-1])
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	l, err = initDatabaseFile(ctx, work, dbInitOptions{BackupsDir: backups})
	if err != nil {
		t.Fatal(err)
	}
	l.release()
	if count() != 1 {
		t.Fatalf("存在待执行迁移时应留 1 份 pre-update 备份，实际 %d", count())
	}
}

func TestIntegrityMarkerAndRepair(t *testing.T) {
	ctx := context.Background()
	work := filepath.Join(t.TempDir(), "knowledge.db")
	l, err := initDatabaseFile(ctx, work, dbInitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	l.release()

	// 标记新鲜：跳过校验，且标记被消费
	resetIntegrityState(t)
	now := time.Now()
	os.WriteFile(work+".clean", []byte(fmt.Sprint(now.UnixMilli()-1000)), 0o644)
	if err := ensureDatabaseIntegrity(ctx, work, func() time.Time { return now }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(work + ".clean"); err == nil {
		t.Fatal("标记应读完即删")
	}
	if lastIntegrityAtMs != now.UnixMilli()-1000 {
		t.Fatalf("应沿用标记里的时刻，实际 %d", lastIntegrityAtMs)
	}
	if integrityRecheckDue.Load() {
		t.Fatal("标记新鲜时不应要求后台复检")
	}
	// 标记过期（>7 天）：干净退出过，启动不再同步校验（不阻塞），交给后台复检；校验时刻沿用旧值
	stale := now.Add(-8 * 24 * time.Hour).UnixMilli()
	os.WriteFile(work+".clean", []byte(fmt.Sprint(stale)), 0o644)
	if err := ensureDatabaseIntegrity(ctx, work, func() time.Time { return now }); err != nil {
		t.Fatal(err)
	}
	if !integrityRecheckDue.Load() {
		t.Fatal("过期标记应交给后台复检")
	}
	if lastIntegrityAtMs != stale {
		t.Fatalf("同步路径不应更新校验时刻，应沿用标记里的旧值 %d，实际 %d", stale, lastIntegrityAtMs)
	}
	// 无标记：校验；乱码文件：报错而不是静默通过
	bad := filepath.Join(t.TempDir(), "bad.db")
	os.WriteFile(bad, []byte(strings.Repeat("not a database ", 400)), 0o644)
	if err := ensureDatabaseIntegrity(ctx, bad, time.Now); err == nil {
		t.Fatal("损坏文件应报错")
	}
	// 索引条目数不符：可自动 REINDEX
	idxDB := filepath.Join(t.TempDir(), "idx.db")
	db, _ := openReadWrite(idxDB)
	db.Exec("CREATE TABLE t (a INTEGER, b TEXT); CREATE INDEX idx_t_b ON t(b); INSERT INTO t VALUES (1,'x'),(2,'y')")
	db.Close()
	if names := indexOnlyMismatchNames([]string{"wrong # of entries in index idx_t_b", "wrong # of entries in index idx_t_b"}); !reflect.DeepEqual(names, []string{"idx_t_b"}) {
		t.Fatalf("index 诊断解析: %v", names)
	}
	if indexOnlyMismatchNames([]string{"wrong # of entries in index a", "row 1 missing"}) != nil {
		t.Fatal("混入其它诊断时不应当作纯索引问题")
	}
	if !isFreelistOnly([]string{"Freelist: size is 3 but should be 2"}) || isFreelistOnly([]string{"ok"}) || isFreelistOnly(nil) {
		t.Fatal("freelist 诊断判定有误")
	}
}

func TestDatabaseClientLease(t *testing.T) {
	db := filepath.Join(t.TempDir(), "knowledge.db")
	alive := map[int]bool{100: true}
	probe := func(pid int) bool { return alive[pid] }

	a, err := acquireDatabaseClientLease(db, 100, probe)
	if err != nil {
		t.Fatal(err)
	}
	// 另一个实例：看到存活的 100，拒绝
	if _, err := acquireDatabaseClientLease(db, 200, probe); err == nil {
		t.Fatal("存在存活的其他客户端时应拒绝")
	} else if live, ok := err.(*errLiveDatabaseClient); !ok || !reflect.DeepEqual(live.PIDs, []int{100}) {
		t.Fatalf("错误类型/内容不符: %v", err)
	}
	if _, err := os.Stat(filepath.Join(db+".clients", "200.json")); err == nil {
		t.Fatal("被拒绝的一方不应留下自己的租约")
	}
	a.release()
	if _, err := os.Stat(db + ".clients"); err == nil {
		t.Fatal("最后一个租约释放后应清掉目录")
	}
	// 死进程残留与格式不符的文件：清掉后放行
	os.MkdirAll(db+".clients", 0o700)
	os.WriteFile(filepath.Join(db+".clients", "300.json"), []byte(`{"pid":300,"registeredAt":"x"}`), 0o600)
	os.WriteFile(filepath.Join(db+".clients", "stray.json"), []byte(`{"pid":301}`), 0o600)
	os.WriteFile(filepath.Join(db+".clients", "garbage.json"), []byte(`not json`), 0o600)
	b, err := acquireDatabaseClientLease(db, 400, probe)
	if err != nil {
		t.Fatalf("残留应被清理: %v", err)
	}
	entries, _ := os.ReadDir(db + ".clients")
	if len(entries) != 1 || entries[0].Name() != "400.json" {
		t.Fatalf("残留未清干净: %v", entries)
	}
	var rec dbLeaseRecord
	raw, _ := os.ReadFile(filepath.Join(db+".clients", "400.json"))
	if json.Unmarshal(raw, &rec) != nil || rec.PID != 400 || !regexp.MustCompile(`^\d{4}-\d\d-\d\dT[\d:.]+Z$`).MatchString(rec.RegisteredAt) {
		t.Fatalf("租约内容不符: %s", raw)
	}
	b.release()
	b.release() // 重复释放无副作用
}

func TestProcessAliveSelf(t *testing.T) {
	if !processAlive(os.Getpid()) {
		t.Fatal("当前进程应判定为存活")
	}
	if processAlive(0x7ffffff0) {
		t.Fatal("不存在的进程不应判定为存活")
	}
}

var _ = sql.ErrNoRows
