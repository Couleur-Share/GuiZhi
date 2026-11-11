// 阶段 0 验证原型：MyGo 的 SQLite（Zig 原生）能否读写 node-sqlite3-wasm 生成的 knowledge.db，
// 并得到与 TS 侧相同的 FTS5 / bm25 检索结果。
// 用法：go run . <fixture 目录>   （目录里要有 knowledge.db 与 expected.json；会先拷贝到副本，不改原件）
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"

	"github.com/egoist/mygo/plugins/sqlite"
)

type expectedRow struct {
	ID   string  `json:"id"`
	Rank float64 `json:"rank"`
}
type expectedQuery struct {
	Match string        `json:"match"`
	Rows  []expectedRow `json:"rows"`
}
type expectedFile struct {
	Meta     map[string]any           `json:"meta"`
	Expected map[string]expectedQuery `json:"expected"`
}

func ok(name string, detail any)         { fmt.Printf("[ OK ] %-34s %v\n", name, detail) }
func fail(name string, detail any)       { fmt.Printf("[FAIL] %-34s %v\n", name, detail); failed = true }
func note(name string, detail any)       { fmt.Printf("[INFO] %-34s %v\n", name, detail) }

var failed bool

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

func main() {
	if len(os.Args) < 2 {
		fmt.Println("用法: go run . <fixture 目录>")
		os.Exit(2)
	}
	src := os.Args[1]
	work, _ := os.MkdirTemp("", "guizhi-mygo-work-")
	defer os.RemoveAll(work)
	dbPath := filepath.Join(work, "knowledge.db")
	if err := copyFile(filepath.Join(src, "knowledge.db"), dbPath); err != nil {
		fail("拷贝 fixture", err)
		os.Exit(1)
	}
	raw, _ := os.ReadFile(filepath.Join(src, "expected.json"))
	var exp expectedFile
	if err := json.Unmarshal(raw, &exp); err != nil {
		fail("读取 expected.json", err)
		os.Exit(1)
	}

	ctx := context.Background()
	db, err := sqlite.Open(ctx, dbPath, sqlite.OpenOptions{})
	if err != nil {
		fail("sqlite.Open(绝对路径)", err)
		os.Exit(1)
	}
	defer db.Close()
	ok("sqlite.Open(绝对路径)", "打开 wasm 写出的库")

	// 1. 基础元数据与完整性
	r, err := db.Query(ctx, "PRAGMA user_version")
	if err != nil {
		fail("PRAGMA user_version", err)
	} else {
		ok("user_version", fmt.Sprintf("%v（TS 侧 %v）", r.Values[0][0], exp.Meta["userVersion"]))
	}
	r, err = db.Query(ctx, "PRAGMA quick_check")
	if err != nil {
		fail("quick_check", err)
	} else {
		ok("quick_check", r.Values[0][0])
	}
	r, err = db.Query(ctx, "SELECT COUNT(*) FROM knowledge_items")
	if err != nil {
		fail("COUNT knowledge_items", err)
	} else {
		ok("knowledge_items 行数", fmt.Sprintf("%v（TS 侧 %v）", r.Values[0][0], exp.Meta["itemCount"]))
	}

	// 2. FTS5 + bm25：与 TS 侧逐条比对 id 顺序与 bm25 值
	for q, e := range exp.Expected {
		rows, err := db.Query(ctx,
			`SELECT item_id, bm25(knowledge_fts, 0.0, 10.0, 1.0, 5.0) AS r
			 FROM knowledge_fts WHERE knowledge_fts MATCH ? ORDER BY r ASC, item_id ASC`, e.Match)
		if err != nil {
			fail("FTS "+q, err)
			continue
		}
		same := len(rows.Values) == len(e.Rows)
		for i := 0; same && i < len(rows.Values); i++ {
			id, _ := rows.Values[i][0].(string)
			rk, _ := rows.Values[i][1].(float64)
			if id != e.Rows[i].ID || math.Abs(rk-e.Rows[i].Rank) > 1e-9 {
				same = false
			}
		}
		if same {
			ok("FTS "+q, fmt.Sprintf("%d 条，id 与 bm25 一致", len(rows.Values)))
		} else {
			fail("FTS "+q, fmt.Sprintf("Go %d 条 vs TS %d 条，结果不一致", len(rows.Values), len(e.Rows)))
		}
	}

	// 3. Go 侧写入：事务内同时写 items 不是本验证重点，只验证 FTS 写入与可被 TS 读回
	_, err = db.Transaction(ctx, []sqlite.Statement{
		{SQL: `INSERT INTO knowledge_fts (item_id, title, content, tags) VALUES (?, ?, ?, ?)`,
			Args: []any{"go-written-1", "写 入 自 G o", "由 Go 侧事务写入的 全 文 索 引 行", ""}},
	})
	if err != nil {
		fail("Go 事务写 FTS", err)
	} else {
		ok("Go 事务写 FTS", "COMMIT 成功")
	}

	// 4. 归知备份依赖 VACUUM INTO；MyGo 插件文档称会拒绝，实测确认
	_, err = db.Execute(ctx, fmt.Sprintf("VACUUM INTO '%s'", filepath.ToSlash(filepath.Join(work, "backup.db"))))
	if err != nil {
		fail("VACUUM INTO（归知备份使用）", err)
	} else {
		ok("VACUUM INTO（归知备份使用）", "可用")
	}
	_, err = db.Execute(ctx, "ATTACH DATABASE ':memory:' AS x")
	if err != nil {
		note("ATTACH", "被拒绝："+err.Error())
	} else {
		note("ATTACH", "可用")
	}

	// 5. 日志模式：wasm 驱动用 DELETE 日志；原生 SQLite 默认也是 DELETE，确认未被改成 WAL
	r, err = db.Query(ctx, "PRAGMA journal_mode")
	if err == nil {
		note("journal_mode", r.Values[0][0])
	}

	// 留下 Go 写过的库，供 TS 读回校验
	db.Close()
	if err := copyFile(dbPath, filepath.Join(src, "after-go-write.db")); err != nil {
		fail("保存 after-go-write.db", err)
	}
	if failed {
		os.Exit(1)
	}
}
