package main

// WebSourceDB（check / adopt）与 TS 的差分测试：同一份起始库（websource-base.db），重放同一操作序列，
// 逐步比较返回值/错误与全部条目的正文、基线、版本、向量、Wiki 摄入记录。

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/plugins/sqlite"
)

type webSourceGolden struct {
	WebSource struct {
		IDs []string `json:"ids"`
		Ops []struct {
			Op struct {
				Fn   string            `json:"fn"`
				Args []json.RawMessage `json:"args"`
			} `json:"op"`
			Value json.RawMessage `json:"value"`
			Error *string         `json:"error"`
			State json.RawMessage `json:"state"`
		} `json:"ops"`
	} `json:"webSource"`
}

func openWebSourceBase(t *testing.T, dir string) *Library {
	t.Helper()
	work := filepath.Join(t.TempDir(), "knowledge.db")
	for _, ext := range []string{"", "-wal", "-shm"} {
		data, err := os.ReadFile(filepath.Join(dir, "websource-base.db"+ext))
		if err != nil {
			if ext == "" {
				t.Fatal(err)
			}
			continue
		}
		if err := os.WriteFile(work+ext, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	db, err := sqlite.Open(context.Background(), work, sqlite.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return &Library{db: db}
}

// webSourceState 与 golden-websource.mts 的 state() 同形。
func webSourceState(t *testing.T, l *Library, ids []string) any {
	t.Helper()
	ctx := context.Background()
	out := make([]any, 0, len(ids))
	for _, id := range ids {
		item, err := l.db.Query(ctx, "SELECT title, content, summary, deleted_at FROM knowledge_items WHERE id=?", id)
		if err != nil {
			t.Fatal(err)
		}
		base, err := l.db.Query(ctx, "SELECT version_id, content_hash, title, checked_at, remote_hash, summary_stale FROM web_source_baselines WHERE item_id=?", id)
		if err != nil {
			t.Fatal(err)
		}
		versions, err := l.webSourceVersions(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		vs := make([]any, 0, len(versions))
		for _, v := range versions {
			vs = append(vs, jsonAny(t, []byte(v.Raw)))
		}
		count := func(table string) int64 {
			r, err := l.db.Query(ctx, "SELECT COUNT(*) FROM "+table+" WHERE item_id=?", id)
			if err != nil {
				t.Fatal(err)
			}
			return asInt(r.Values[0][0])
		}
		var baseline any
		if len(base.Values) > 0 {
			baseline = goAny(t, goRows(base.Values)[0])
		}
		out = append(out, map[string]any{
			"item":       goAny(t, goRows(item.Values)[0]),
			"baseline":   baseline,
			"versions":   vs,
			"embeddings": count("knowledge_embeddings"),
			"wiki":       count("wiki_ingestions"),
		})
	}
	return goAny(t, out)
}

func TestWebSourceMatchesTS(t *testing.T) {
	dir := os.Getenv("GUIZHI_GOLDEN_DIR")
	if dir == "" {
		t.Skip("未设置 GUIZHI_GOLDEN_DIR")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	var g webSourceGolden
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	if len(g.WebSource.Ops) == 0 {
		t.Fatal("golden 缺少 webSource 段")
	}
	l := openWebSourceBase(t, dir)
	ctx := context.Background()
	ids := g.WebSource.IDs

	var resolve func(v any, n int) any
	resolve = func(v any, n int) any {
		switch x := v.(type) {
		case []any:
			out := make([]any, len(x))
			for i := range x {
				out[i] = resolve(x[i], n)
			}
			return out
		case map[string]any:
			if i, ok := x["$item"]; ok {
				return ids[int(i.(float64))]
			}
			if p, ok := x["$ver"]; ok {
				pair := p.([]any)
				vs, err := l.webSourceVersions(ctx, ids[int(pair[0].(float64))])
				if err != nil {
					t.Fatal(err)
				}
				if k := int(pair[1].(float64)); k < len(vs) {
					return vs[k].ID
				}
				return "missing-version"
			}
			if i, ok := x["$hash"]; ok {
				return webContentHash(rawItemField(t, l, ids[int(i.(float64))], "content"))
			}
			if i, ok := x["$title"]; ok {
				return rawItemField(t, l, ids[int(i.(float64))], "title")
			}
			if c, ok := x["$cap"]; ok {
				spec := c.(map[string]any)
				r := webCaptureResult{
					TaskID: "t", EntryURL: "https://a.test/p", FinalURL: "https://a.test/p",
					Title: spec["title"].(string), Markdown: spec["md"].(string),
					CapturedAt: 1_700_000_000_000 + int64(100+n)*1000, EngineVersion: "readability/1-static",
					Complete: true,
				}
				if e, ok := spec["engine"].(string); ok {
					r.EngineVersion = e
				}
				if c, ok := spec["complete"].(bool); ok {
					r.Complete = c
				}
				if tr, ok := spec["truncated"].(bool); ok {
					r.Truncated = tr
				}
				if e, ok := spec["error"].(map[string]any); ok {
					r.Error = &webCaptureError{Code: e["code"].(string), Message: e["message"].(string)}
				}
				return r
			}
			out := map[string]any{}
			for k, e := range x {
				out[k] = resolve(e, n)
			}
			return out
		}
		return v
	}

	for n, op := range g.WebSource.Ops {
		args := make([]any, len(op.Op.Args))
		for i, a := range op.Op.Args {
			args[i] = resolve(jsonAny(t, a), n)
		}
		var value any
		var opErr error
		switch op.Op.Fn {
		case "check":
			r, _ := args[1].(webCaptureResult)
			value, opErr = l.webSourceCheck(ctx, args[0].(string), r)
		case "adopt":
			m := args[0].(map[string]any)
			opErr = l.webSourceAdopt(ctx, webAdoptInput{
				ItemID: m["itemId"].(string), VersionID: m["versionId"].(string),
				ExpectedContentHash: m["expectedContentHash"].(string), ExpectedTitle: m["expectedTitle"].(string),
			})
		case "__update":
			var in UpdateInput
			b, _ := json.Marshal(args[1])
			if err := json.Unmarshal(b, &in); err != nil {
				t.Fatal(err)
			}
			_, opErr = l.knowledgeUpdate(ctx, args[0].(string), in)
		default:
			t.Fatalf("未知操作 %s", op.Op.Fn)
		}
		label := fmt.Sprintf("op#%d %s", n, op.Op.Fn)
		if op.Error != nil {
			if opErr == nil || opErr.Error() != *op.Error {
				t.Errorf("%s: 应报错 %q，实际 %v", label, *op.Error, opErr)
			}
		} else {
			if opErr != nil {
				t.Errorf("%s: 不应报错: %v", label, opErr)
			}
			if want := jsonAny(t, op.Value); op.Op.Fn == "check" && want != value {
				t.Errorf("%s: got %v want %v", label, value, want)
			}
		}
		got := maskCrawlIDs(t, webSourceState(t, l, ids))
		want := maskCrawlIDs(t, jsonAny(t, op.State))
		if got != want {
			_ = os.WriteFile(filepath.Join(os.TempDir(), "ws-got.json"), []byte(got), 0o644)
			_ = os.WriteFile(filepath.Join(os.TempDir(), "ws-want.json"), []byte(want), 0o644)
			t.Fatalf("%s: 状态不一致（已写入临时目录 ws-got.json / ws-want.json）", label)
		}
	}
}

// rawItemField 读条目字段（含已删除条目，对应 TS 的 items.get）。
func rawItemField(t *testing.T, l *Library, id, col string) string {
	t.Helper()
	r, err := l.db.Query(context.Background(), "SELECT "+col+" FROM knowledge_items WHERE id=?", id)
	if err != nil || len(r.Values) == 0 {
		t.Fatalf("条目缺失 %s: %v", id, err)
	}
	s, _ := r.Values[0][0].(string)
	return s
}
