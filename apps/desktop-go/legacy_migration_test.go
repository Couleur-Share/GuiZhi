package main

// 旧库迁移差分测试：golden.legacyMigration 由真实 TS（migration.ipc.ts + legacy-migration.ts）生成。

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/sqlite"
)

type legacyGolden struct {
	LegacyMigration struct {
		DumpSQL      map[string]string `json:"dumpSql"`
		LegacySchema []string          `json:"legacySchema"`
		WikiSchema   []string          `json:"wikiSchema"`
		FullStmts    []string          `json:"fullStmts"`
		Cases        []struct {
			Name   string `json:"name"`
			Legacy *struct {
				Stmts  []string `json:"stmts"`
				Wiki   bool     `json:"wiki"`
				Header *string  `json:"header"`
			} `json:"legacy"`
			Extra          *string                    `json:"extra"`
			TargetNotEmpty bool                       `json:"targetNotEmpty"`
			WalResidue     bool                       `json:"walResidue"`
			Ops            []map[string]any           `json:"ops"`
			Results        []map[string]any           `json:"results"`
			Dump           map[string][][]interface{} `json:"dump"`
			TmpLeftovers   int                        `json:"tmpLeftovers"`
		} `json:"cases"`
	} `json:"legacyMigration"`
}

func writeLegacyDB(t *testing.T, file string, schema, wikiSchema, stmts []string, wiki bool, header *string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	os.Remove(file)
	ctx := context.Background()
	db, err := sqlite.Open(ctx, file, sqlite.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	exec := func(list []string) {
		for _, s := range list {
			if _, err := db.Execute(ctx, s); err != nil {
				t.Fatalf("旧库造数失败：%v\n%s", err, s)
			}
		}
	}
	exec(schema)
	if wiki {
		exec(wikiSchema)
	}
	exec(stmts)
	db.Close()
	if header != nil && *header == "wal" {
		data, _ := os.ReadFile(file)
		data[18], data[19] = 2, 2
		os.WriteFile(file, data, 0o644)
	}
}

var legacyPathNorm = regexp.MustCompile(`\\`)

func TestLegacyMigrationParity(t *testing.T) {
	dir := os.Getenv("GUIZHI_GOLDEN_DIR")
	if dir == "" {
		t.Skip("未设置 GUIZHI_GOLDEN_DIR")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	var g legacyGolden
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	lg := g.LegacyMigration
	if len(lg.Cases) == 0 {
		t.Fatal("golden 里没有旧库迁移场景")
	}
	for _, c := range lg.Cases {
		t.Run(c.Name, func(t *testing.T) {
			ud := t.TempDir()
			legacyFile := filepath.Join(ud, "data", "guizhi.db")
			extraDir := filepath.Join(ud, "legacy-extra")
			explicitFile := filepath.Join(extraDir, "other-legacy.db")
			notLegacyFile := filepath.Join(extraDir, "not-legacy.db")
			norm := func(s string) string {
				s = strings.ReplaceAll(s, legacyFile, "<LEGACY>")
				s = strings.ReplaceAll(s, explicitFile, "<EXPLICIT>")
				s = strings.ReplaceAll(s, notLegacyFile, "<NOTLEGACY>")
				s = strings.ReplaceAll(s, extraDir, "<EXTRA>")
				s = strings.ReplaceAll(s, ud, "<UD>")
				return legacyPathNorm.ReplaceAllString(s, "/")
			}
			if c.Legacy != nil {
				writeLegacyDB(t, legacyFile, lg.LegacySchema, lg.WikiSchema, c.Legacy.Stmts, c.Legacy.Wiki, c.Legacy.Header)
			}
			if c.Extra != nil && *c.Extra == "explicit" {
				writeLegacyDB(t, explicitFile, lg.LegacySchema, lg.WikiSchema, lg.FullStmts, true, nil)
			}
			if c.Extra != nil && *c.Extra == "notlegacy" {
				os.MkdirAll(extraDir, 0o755)
				db, err := sqlite.Open(context.Background(), notLegacyFile, sqlite.OpenOptions{})
				if err != nil {
					t.Fatal(err)
				}
				db.Execute(context.Background(), "CREATE TABLE Other (x)")
				db.Close()
			}
			if c.WalResidue {
				os.WriteFile(legacyFile+"-wal", []byte{1, 2, 3}, 0o644)
			}

			work := filepath.Join(ud, "target.db")
			base, err := os.ReadFile(filepath.Join(dir, "empty-schema.db"))
			if err != nil {
				t.Fatal(err)
			}
			os.WriteFile(work, base, 0o644)
			ctx := context.Background()
			db, err := sqlite.Open(ctx, work, sqlite.OpenOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if c.TargetNotEmpty {
				if _, err := db.Execute(ctx, "INSERT INTO knowledge_items (id, title, content, item_type, status, created_at, updated_at) VALUES ('pre','p','c','note','active',1,1)"); err != nil {
					t.Fatal(err)
				}
			}
			l := &Library{db: db, paths: Paths{UserData: ud}}
			b := &Bridge{handlers: map[string]handler{}}
			l.registerLegacyMigration(b)

			runStart := time.Now().UnixMilli()
			for i, op := range c.Ops {
				var got map[string]any
				var args []json.RawMessage
				ch := chMigrationDetectLegacy
				if op["op"] == "run" {
					ch = chMigrationRunLegacy
					switch op["path"] {
					case "explicit":
						args = append(args, jsonStringRaw(explicitFile))
					case "missing":
						args = append(args, jsonStringRaw(filepath.Join(extraDir, "missing.db")))
					case "empty":
						args = append(args, jsonStringRaw(""))
					case "other-nolegacy":
						args = append(args, jsonStringRaw(notLegacyFile))
					}
				}
				v, err := b.handlers[ch](ctx, args)
				if err != nil {
					got = map[string]any{"error": norm(err.Error())}
				} else {
					got = map[string]any{"value": normalizeLegacyValue(t, v, norm)}
				}
				want := c.Results[i]
				if !legacyResultEqual(want, got) {
					t.Errorf("op %d %v：\n  TS: %v\n  Go: %v", i, op["op"], want, got)
				}
			}

			for name, sql := range lg.DumpSQL {
				rows, err := db.Query(ctx, sql)
				if err != nil {
					t.Fatalf("%s: %v", name, err)
				}
				var gotRows [][]any
				for _, r := range rows.Values {
					var row []any
					for _, v := range r {
						row = append(row, nowish(roundTripNum(v), runStart))
					}
					gotRows = append(gotRows, row)
				}
				var wantRows [][]any
				for _, r := range c.Dump[name] {
					wantRows = append(wantRows, r)
				}
				if len(gotRows) == 0 && len(wantRows) == 0 {
					continue
				}
				if !reflect.DeepEqual(wantRows, gotRows) {
					wb, _ := json.Marshal(wantRows)
					gb, _ := json.Marshal(gotRows)
					t.Errorf("表 %s 不一致\nTS: %.1500s\nGo: %.1500s", name, wb, gb)
				}
			}
			leftovers, _ := filepath.Glob(filepath.Join(os.TempDir(), "guizhi-legacy-migration-*.db"))
			if len(leftovers) > 0 {
				t.Errorf("临时拷贝没有清理：%v", leftovers)
			}
		})
	}
}

func jsonStringRaw(s string) json.RawMessage {
	b, _ := json.Marshal(s)
	return b
}

func roundTripNum(v any) any {
	switch x := v.(type) {
	case int64:
		return float64(x)
	case []byte:
		return string(x)
	}
	return v
}

// nowish 把「迁移时刻」写入的时间戳（旧库时间缺失/损坏时回落到当前时间）统一成 NOW。
func nowish(v any, runStart int64) any {
	if f, ok := v.(float64); ok && f >= float64(runStart-5000) && f <= float64(time.Now().UnixMilli()+5000) {
		return "NOW"
	}
	return v
}

func normalizeLegacyValue(t *testing.T, v any, norm func(string) string) any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	var walk func(any) any
	walk = func(x any) any {
		switch y := x.(type) {
		case string:
			return norm(y)
		case map[string]any:
			for k, e := range y {
				y[k] = walk(e)
			}
		}
		return x
	}
	return walk(out)
}

// 自定义的中文报错逐字比对；驱动层的报错（约束冲突等）只比「是否报错」。
func legacyResultEqual(want, got map[string]any) bool {
	if we, ok := want["error"].(string); ok {
		ge, gok := got["error"].(string)
		if !gok {
			return false
		}
		if regexp.MustCompile(`[\p{Han}]`).MatchString(we) {
			return we == ge
		}
		return true
	}
	return reflect.DeepEqual(want, got)
}
