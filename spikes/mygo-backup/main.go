// 阶段 0 验证原型：MyGo 的 SQLite 插件拒绝 VACUUM INTO / ATTACH，备份怎么做。
// 方案：备份用纯 Go 的 modernc.org/sqlite 只读打开同一个库并执行 VACUUM INTO；
// 业务读写仍走 MyGo 自带的 SQLite。
// 验证：保真（表、行数、建表语句、FTS 完整性）、并发写入时快照的原子性、写入停顿、耗时。
// 用法：go run . <含 knowledge.db 的目录> [padMB]
package main

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/plugins/sqlite"
	_ "modernc.org/sqlite"
)

func report(name string, ok bool, detail string) {
	tag := "[ OK ]"
	if !ok {
		tag = "[FAIL]"
	}
	fmt.Printf("%s %-34s %s\n", tag, name, detail)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// backupTo 用 modernc 只读连接对在线库做 VACUUM INTO。
func backupTo(srcPath, dstPath string) error {
	dsn := "file:" + filepath.ToSlash(srcPath) + "?mode=ro&_pragma=busy_timeout(10000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec("VACUUM INTO ?", dstPath)
	return err
}

func tableCounts(ctx context.Context, db *sqlite.DB) (map[string]int64, error) {
	rows, err := db.Query(ctx, "SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' AND name NOT LIKE 'spike_%' ORDER BY name")
	if err != nil {
		return nil, err
	}
	out := map[string]int64{}
	for _, r := range rows.Values {
		name := r[0].(string)
		c, err := db.Query(ctx, `SELECT count(*) FROM "`+name+`"`)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		out[name] = c.Values[0][0].(int64)
	}
	return out, nil
}

func schemaOf(ctx context.Context, db *sqlite.DB) ([]string, error) {
	rows, err := db.Query(ctx, "SELECT type||'|'||name||'|'||ifnull(sql,'') FROM sqlite_master WHERE name NOT LIKE 'spike_%' ORDER BY type, name")
	if err != nil {
		return nil, err
	}
	var s []string
	for _, r := range rows.Values {
		s = append(s, r[0].(string))
	}
	return s, nil
}

// verify 校验一份备份：完整性、FTS 完整性、与源库的表行数和建表语句一致、pad 表成对。
func verify(ctx context.Context, srcDB *sqlite.DB, dst string, compareWithSrc bool) (string, bool) {
	// modernc 做完整性与 FTS 校验（写入类校验需要可写连接，作用于备份副本）。
	chk, err := sql.Open("sqlite", "file:"+filepath.ToSlash(dst)+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return err.Error(), false
	}
	defer chk.Close()
	var ic string
	if err := chk.QueryRow("PRAGMA integrity_check").Scan(&ic); err != nil || ic != "ok" {
		return fmt.Sprintf("integrity_check=%q err=%v", ic, err), false
	}
	if _, err := chk.Exec("INSERT INTO knowledge_fts(knowledge_fts) VALUES('integrity-check')"); err != nil {
		return "fts integrity-check: " + err.Error(), false
	}
	var a, b int64
	if err := chk.QueryRow("SELECT (SELECT count(*) FROM spike_pad), (SELECT count(*) FROM spike_pad2)").Scan(&a, &b); err != nil {
		return "pad: " + err.Error(), false
	}
	if a != b {
		return fmt.Sprintf("快照不原子：pad=%d pad2=%d", a, b), false
	}
	// MyGo 自己的 SQLite 能打开并读出同样的内容。
	dstDB, err := sqlite.Open(ctx, dst, sqlite.OpenOptions{ReadOnly: true})
	if err != nil {
		return "MyGo 打开备份: " + err.Error(), false
	}
	defer dstDB.Close()
	if compareWithSrc {
		c1, err1 := tableCounts(ctx, srcDB)
		c2, err2 := tableCounts(ctx, dstDB)
		if err1 != nil || err2 != nil {
			return fmt.Sprintf("行数: %v %v", err1, err2), false
		}
		if !reflect.DeepEqual(c1, c2) {
			return fmt.Sprintf("行数不一致 src=%v dst=%v", c1, c2), false
		}
		s1, _ := schemaOf(ctx, srcDB)
		s2, _ := schemaOf(ctx, dstDB)
		if !reflect.DeepEqual(s1, s2) {
			return "建表语句不一致", false
		}
	}
	return fmt.Sprintf("pad=%d", a), true
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("用法：go run . <含 knowledge.db 的目录> [padMB]")
		os.Exit(2)
	}
	padMB := 100
	if len(os.Args) > 2 {
		fmt.Sscan(os.Args[2], &padMB)
	}
	work, _ := os.MkdirTemp("", "guizhi-mygo-backup-")
	defer os.RemoveAll(work)
	live := filepath.Join(work, "live.db")
	if err := copyFile(filepath.Join(os.Args[1], "knowledge.db"), live); err != nil {
		fmt.Println("复制样本库失败：", err)
		os.Exit(2)
	}

	ctx := context.Background()
	db, err := sqlite.Open(ctx, live, sqlite.OpenOptions{BusyTimeout: 30 * time.Second})
	if err != nil {
		fmt.Println("MyGo 打开失败：", err)
		os.Exit(2)
	}
	defer db.Close()

	// 造一份体量：两张成对的 pad 表，用于校验快照原子性和测耗时。
	db.Execute(ctx, "CREATE TABLE spike_pad(id INTEGER PRIMARY KEY, x BLOB)")
	db.Execute(ctx, "CREATE TABLE spike_pad2(id INTEGER PRIMARY KEY, x BLOB)")
	blob := make([]byte, 64*1024)
	for i := range blob {
		blob[i] = byte(i * 7)
	}
	n := padMB * 1024 / 64
	for i := 0; i < n; i += 50 {
		var st []sqlite.Statement
		for j := 0; j < 50 && i+j < n; j++ {
			st = append(st,
				sqlite.Statement{SQL: "INSERT INTO spike_pad(x) VALUES (?)", Args: []any{blob}},
				sqlite.Statement{SQL: "INSERT INTO spike_pad2(x) VALUES (?)", Args: []any{blob}})
		}
		if _, err := db.Transaction(ctx, st); err != nil {
			fmt.Println("造数据失败：", err)
			os.Exit(2)
		}
	}
	fi, _ := os.Stat(live)
	fmt.Printf("样本库 %.0f MB（含两张各 %d MB 的 pad 表）\n", float64(fi.Size())/1e6, padMB)

	// 0) 前提：MyGo 自带 SQLite 确实拒绝 VACUUM INTO。
	_, err = db.Execute(ctx, "VACUUM INTO ?", filepath.Join(work, "denied.db"))
	report("MyGo 内置 VACUUM INTO 被拒绝", err != nil, fmt.Sprint(err))

	// 1) 保真。
	dst1 := filepath.Join(work, "b1.db")
	t0 := time.Now()
	err = backupTo(live, dst1)
	d1 := time.Since(t0)
	if err != nil {
		report("modernc VACUUM INTO", false, err.Error())
		return
	}
	detail, ok := verify(ctx, db, dst1, true)
	st, _ := os.Stat(dst1)
	report("备份保真（完整性/FTS/行数/建表语句）", ok, fmt.Sprintf("%s，耗时 %v，备份 %.0f MB", detail, d1.Round(time.Millisecond), float64(st.Size())/1e6))

	// 2) 并发写入：MyGo 写入成对 pad 行，同时反复备份。
	var stop atomic.Bool
	var wg sync.WaitGroup
	var writes, writeErrs atomic.Int64
	var maxStall atomic.Int64
	wg.Add(1)
	go func() {
		defer wg.Done()
		small := []byte("w")
		for !stop.Load() {
			t := time.Now()
			_, err := db.Transaction(ctx, []sqlite.Statement{
				{SQL: "INSERT INTO spike_pad(x) VALUES (?)", Args: []any{small}},
				{SQL: "INSERT INTO spike_pad2(x) VALUES (?)", Args: []any{small}},
			})
			if err != nil {
				writeErrs.Add(1)
			} else {
				writes.Add(1)
			}
			if d := time.Since(t).Milliseconds(); d > maxStall.Load() {
				maxStall.Store(d)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
	allOK := true
	var firstFail string
	var total time.Duration
	const rounds = 5
	for i := 0; i < rounds; i++ {
		dst := filepath.Join(work, fmt.Sprintf("c%d.db", i))
		t := time.Now()
		if err := backupTo(live, dst); err != nil {
			allOK = false
			firstFail = "backup: " + err.Error()
			break
		}
		total += time.Since(t)
		if d, ok := verify(ctx, db, dst, false); !ok {
			allOK = false
			firstFail = d
			break
		}
		os.Remove(dst)
	}
	stop.Store(true)
	wg.Wait()
	report(fmt.Sprintf("并发写入下连续备份 %d 次", rounds), allOK, fmt.Sprintf("%s 平均 %v；期间写入 %d 次、失败 %d 次、最长一次写入 %d ms", firstFail, (total/rounds).Round(time.Millisecond), writes.Load(), writeErrs.Load(), maxStall.Load()))

	// 3) 失败路径：目标已存在时 VACUUM INTO 应报错，且不破坏已有文件。
	err = backupTo(live, dst1)
	report("目标已存在时报错", err != nil && strings.Contains(strings.ToLower(fmt.Sprint(err)), "already"), fmt.Sprint(err))
}
