package main

// 主题阅读同步频道差分：回放 golden-themed-ipc.mts 的剧本（真实 themed-reading.ipc.ts + 种子库 seed.db），
// 逐个频道对拍返回值、导出文件内容与最终库状态。需要 GUIZHI_THEMED_IPC_GOLDEN_DIR。

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"

	"github.com/egoist/mygo/plugins/sqlite"
)

var reQuotedPath = regexp.MustCompile(`'[^']*'`)
var reUnsafeDefault = regexp.MustCompile(`[\\/:*?"<>|]`)

// themedMaskPaths 把错误文案里的绝对路径抹掉（两边的临时目录不同）。
func themedMaskPaths(v any) any {
	switch x := v.(type) {
	case string:
		return reQuotedPath.ReplaceAllString(x, "'@@path@@'")
	case []any:
		for i := range x {
			x[i] = themedMaskPaths(x[i])
		}
	case map[string]any:
		for k, e := range x {
			x[k] = themedMaskPaths(e)
		}
	}
	return v
}

func newThemedIPCLib(t *testing.T, dir string) (*Library, *Bridge, string) {
	t.Helper()
	user := t.TempDir()
	l := &Library{paths: Paths{UserData: user}, configDir: filepath.Join(user, "config")}
	if err := os.MkdirAll(l.paths.ImagesDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "userdata", "data", "assets", "images")
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		data, _ := os.ReadFile(filepath.Join(src, e.Name()))
		if err := os.WriteFile(filepath.Join(l.paths.ImagesDir(), e.Name()), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(filepath.Join(dir, "seed.db"))
	if err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(user, "knowledge.db")
	if err := os.WriteFile(work, data, 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := sqlite.Open(context.Background(), work, sqlite.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	l.db = db
	exports := filepath.Join(user, "exports")
	_ = os.MkdirAll(exports, 0o755)
	n := 0
	l.saveFileDialog = func(title, defaultPath string, filters []dialogFilter) (string, error) {
		n++
		return filepath.Join(exports, "export-"+itoaThemed(n)+"-"+reUnsafeDefault.ReplaceAllString(defaultPath, "_")), nil
	}
	b := &Bridge{handlers: map[string]handler{}}
	l.registerThemedReading(b)
	return l, b, exports
}

func itoaThemed(n int) string { return strconv.Itoa(n) }

func TestThemedIPCDifferential(t *testing.T) {
	dir := os.Getenv("GUIZHI_THEMED_IPC_GOLDEN_DIR")
	if dir == "" {
		t.Skip("未设置 GUIZHI_THEMED_IPC_GOLDEN_DIR")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "golden-themed-ipc.json"))
	if err != nil {
		t.Fatal(err)
	}
	var g struct {
		Recs []forumRec     `json:"recs"`
		Dump map[string]any `json:"dump"`
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	l, b, _ := newThemedIPCLib(t, dir)
	ctx := context.Background()
	for _, r := range g.Recs {
		r := r
		if r.Kind == "thmArticle" {
			// 本文问答的 AI 阅读页来源：TS articleContext 对拍
			in, perr := jsParse(string(r.Input))
			if perr != nil {
				t.Fatal(perr)
			}
			out, err := l.articleContext(ctx, in)
			t.Run(r.Name, func(t *testing.T) {
				var got any
				if err != nil {
					got = map[string]any{"thrown": err.Error()}
				} else {
					b, _ := json.Marshal(out)
					_ = json.Unmarshal(b, &got)
				}
				fgCompare(t, fgDecode(t, r.Output), got)
			})
			continue
		}
		var call struct {
			Ch   string            `json:"ch"`
			Args []json.RawMessage `json:"args"`
		}
		mustJSON(t, r.Input, &call)
		h := b.handlers[call.Ch]
		if h == nil {
			t.Fatalf("频道 %s 未注册", call.Ch)
		}
		args := call.Args
		if len(args) == 1 && string(args[0]) == `"@@undef@@"` {
			args = nil
		}
		out, err := h(ctx, args)
		t.Run(r.Name, func(t *testing.T) {
			if err != nil {
				t.Fatal(err)
			}
			got := themedJSONOut(t, out)
			m, _ := got.(map[string]any)
			if _, ok := m["search"]; ok {
				m["search"] = "@@present@@"
			}
			if call.Ch == chThemedExport {
				if p, ok := m["path"].(string); ok {
					data, err := os.ReadFile(p)
					if err != nil {
						t.Fatal(err)
					}
					m["fileContent"], m["path"] = string(data), "@@file@@"
				}
			}
			fgCompare(t, themedMaskPaths(fgDecode(t, r.Output)), themedMaskPaths(got))
		})
	}
	t.Run("final-state", func(t *testing.T) {
		rows, _ := l.db.Query(ctx, "SELECT id,item_id,source_kind,role FROM themed_reading_versions ORDER BY id")
		versions := []any{}
		for _, v := range rows.Values {
			versions = append(versions, map[string]any{"id": v[0], "item_id": v[1], "source_kind": v[2], "role": v[3]})
		}
		trows, _ := l.db.Query(ctx, "SELECT id,state FROM themed_reading_tasks ORDER BY id")
		tasks := []any{}
		for _, v := range trows.Values {
			tasks = append(tasks, map[string]any{"id": v[0], "state": v[1]})
		}
		fgCompare(t, themedMaskPaths(g.Dump), themedMaskPaths(map[string]any{"versions": versions, "tasks": tasks}))
	})
}
