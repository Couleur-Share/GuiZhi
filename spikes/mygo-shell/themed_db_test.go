package main

// 主题阅读页数据层差分：回放 themed-db-cases.mts 的剧本（真实 ThemedReadingDB），逐步对拍返回值与三张表快照。
// 需要 GUIZHI_GOLDEN_DIR（取其中已迁移到最新 schema 的库作模板）与 GUIZHI_THEMED_DB_GOLDEN_DIR。

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var themedConstraintErr = regexp.MustCompile(`(?i)constraint|UNIQUE|FOREIGN|CHECK`)

// themedMask 对应 TS 夹具里的 mask：把大于 1e11 的 updatedAt 抹成 "NOW"。
func themedMask(v any) any {
	switch x := v.(type) {
	case []any:
		for i := range x {
			x[i] = themedMask(x[i])
		}
	case map[string]any:
		for k, e := range x {
			if f, ok := e.(float64); ok && (k == "updatedAt" || k == "updated_at") && f > 1e11 {
				x[k] = "NOW"
				continue
			}
			x[k] = themedMask(e)
		}
	}
	return v
}

func themedDump(t *testing.T, l *Library) any {
	t.Helper()
	ctx := context.Background()
	table := func(sql string, jsonCols ...string) []any {
		rows, err := l.db.Query(ctx, sql)
		if err != nil {
			t.Fatal(err)
		}
		out := []any{}
		for _, r := range rows.Values {
			m := map[string]any{}
			for i, c := range rows.Columns {
				v := r[i]
				if f, ok := v.(int64); ok {
					v = float64(f)
				}
				for _, jc := range jsonCols {
					if c == jc {
						var p any
						if err := json.Unmarshal([]byte(v.(string)), &p); err != nil {
							t.Fatal(err)
						}
						v = p
					}
				}
				m[c] = v
			}
			out = append(out, m)
		}
		return out
	}
	return themedMask(map[string]any{
		"versions": table("SELECT id,item_id,source_kind,role,payload,created_at,updated_at FROM themed_reading_versions ORDER BY id", "payload"),
		"assets":   table("SELECT version_id,file_name,sha256,bytes FROM themed_reading_assets ORDER BY version_id,file_name"),
		"tasks":    table("SELECT id,item_id,source_kind,version_id,state,payload,created_at,updated_at FROM themed_reading_tasks ORDER BY id", "payload"),
	})
}

func themedOptTask(t *testing.T, raw json.RawMessage) *jsObj {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return themedParseObj(t, string(raw))
}

func themedParseObj(t *testing.T, s string) *jsObj {
	t.Helper()
	v, err := jsParse(s)
	if err != nil {
		t.Fatal(err)
	}
	return v.(*jsObj)
}

// themedJSONOut 把 jsObj / 列表转成可与基线比较的通用 JSON，并抹时间戳。
func themedJSONOut(t *testing.T, v any) any {
	t.Helper()
	var s string
	switch x := v.(type) {
	case nil:
		return nil
	case []*jsObj:
		arr := jsArr{}
		for _, o := range x {
			arr = append(arr, o)
		}
		s, _ = jsStringify(arr)
	case *jsObj:
		if x == nil {
			return nil
		}
		s, _ = jsStringify(x)
	default:
		b, _ := json.Marshal(v)
		s = string(b)
	}
	var out any
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		t.Fatal(err)
	}
	return themedMask(out)
}

func TestThemedPagesDifferential(t *testing.T) {
	dir := os.Getenv("GUIZHI_THEMED_DB_GOLDEN_DIR")
	if dir == "" {
		t.Skip("未设置 GUIZHI_THEMED_DB_GOLDEN_DIR")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "golden-themed-db.json"))
	if err != nil {
		t.Fatal(err)
	}
	var g struct {
		Recs  []forumRec `json:"recs"`
		Items []struct {
			ID        string `json:"id"`
			DeletedAt *int64 `json:"deletedAt"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	_, l := loadGolden(t)
	ctx := context.Background()
	for _, tbl := range []string{"themed_reading_versions", "knowledge_items"} {
		if _, err := l.db.Execute(ctx, "DELETE FROM "+tbl); err != nil {
			t.Fatal(err)
		}
	}
	for _, it := range g.Items {
		var del any
		if it.DeletedAt != nil {
			del = *it.DeletedAt
		}
		if _, err := l.db.Execute(ctx, "INSERT INTO knowledge_items (id,title,content,transcript,item_type,deleted_at,created_at,updated_at) VALUES (?,?,?,?,?,?,1,1)", it.ID, "t", "c", nil, "note", del); err != nil {
			t.Fatal(err)
		}
	}
	pages := l.themedPages()
	for _, r := range g.Recs {
		r := r
		var op struct {
			Op      string          `json:"op"`
			Version json.RawMessage `json:"version"`
			Task    json.RawMessage `json:"task"`
			Item    string          `json:"item"`
			Kind    string          `json:"kind"`
			Role    string          `json:"role"`
			ID      string          `json:"id"`
			SQL     string          `json:"sql"`
			Args    []any           `json:"args"`
		}
		mustJSON(t, r.Input, &op)
		var res any
		var runErr error
		switch op.Op {
		case "saveVersion":
			var v *jsObj
			if v, runErr = pages.saveVersion(ctx, themedParseObj(t, string(op.Version)), themedOptTask(t, op.Task)); runErr == nil {
				res = v
			}
		case "saveTask":
			var v *jsObj
			if v, runErr = pages.saveTask(ctx, themedParseObj(t, string(op.Task))); runErr == nil {
				res = v
			}
		case "get":
			var v *jsObj
			if v, runErr = pages.get(ctx, op.Item, op.Kind, op.Role); runErr == nil {
				res = v
			}
		case "getVersion":
			var v *jsObj
			if v, runErr = pages.getVersion(ctx, op.ID); runErr == nil {
				res = v
			}
		case "getTask":
			var v *jsObj
			if v, runErr = pages.getTask(ctx, op.ID); runErr == nil {
				res = v
			}
		case "listTasks":
			var v []*jsObj
			if v, runErr = pages.listTasks(ctx); runErr == nil {
				res = v
				if v == nil {
					res = []*jsObj{}
				}
			}
		case "publish":
			var v *jsObj
			if v, runErr = pages.publish(ctx, op.ID, themedOptTask(t, op.Task)); runErr == nil {
				res = v
			}
		case "restorePrevious":
			var v *jsObj
			if v, runErr = pages.restorePrevious(ctx, op.Item, op.Kind); runErr == nil {
				res = v
			}
		case "remove":
			var v []string
			if v, runErr = pages.remove(ctx, op.Item, op.Kind); runErr == nil {
				res = append([]string{}, v...)
			}
		case "interruptRunning":
			var v []*jsObj
			if v, runErr = pages.interruptRunning(ctx); runErr == nil {
				res = v
				if v == nil {
					res = []*jsObj{}
				}
			}
		case "hasActiveTasks":
			res, runErr = pages.hasActiveTasks(ctx)
		case "listAssetFiles":
			var v []string
			if v, runErr = l.themedReadingAssetFiles(ctx, nil); runErr == nil {
				res = append([]string{}, v...)
			}
		case "sql":
			_, runErr = l.db.Execute(ctx, op.SQL, op.Args...)
		default:
			t.Fatalf("未知操作 %s", op.Op)
		}
		t.Run(r.Name, func(t *testing.T) {
			var result any
			if runErr != nil {
				msg := runErr.Error()
				if themedConstraintErr.MatchString(msg) {
					msg = "SQLERR"
				}
				result = map[string]any{"error": strings.TrimSpace(msg)}
			} else {
				result = map[string]any{"ok": themedJSONOut(t, res)}
			}
			fgCompare(t, fgDecode(t, r.Output), map[string]any{"result": result, "state": themedDump(t, l)})
		})
	}
}
