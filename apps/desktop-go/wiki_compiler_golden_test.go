package main

// wiki:compiler 差分测试：golden-wikicompiler.mts 用真实 TS（wiki-compiler.ipc.ts）录制的
// 纯函数输出与场景脚本，这里在同一份种子库的副本上重放并逐步比对响应与表转储。

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/egoist/mygo/plugins/sqlite"
)

type wcBlock struct {
	Key     string `json:"key"`
	Hash    string `json:"hash"`
	Field   string `json:"field"`
	Section string `json:"section"`
	Start   int    `json:"start"`
	End     int    `json:"end"`
	Text    string `json:"text"`
}

type wcStep struct {
	Op       string          `json:"op"`
	Input    json.RawMessage `json:"input"`
	SQL      string          `json:"sql"`
	Args     []any           `json:"args"`
	Response json.RawMessage `json:"response"`
	Dump     json.RawMessage `json:"dump"`
}

type wcGolden struct {
	Blocks []struct {
		Name        string    `json:"name"`
		Title       string    `json:"title"`
		Content     string    `json:"content"`
		Transcript  *string   `json:"transcript"`
		Fingerprint string    `json:"fingerprint"`
		Blocks      []wcBlock `json:"blocks"`
	} `json:"blocks"`
	DumpSQL   map[string]string   `json:"dumpSql"`
	Scenarios map[string][]wcStep `json:"scenarios"`
}

func loadWikiCompilerGolden(t *testing.T) (wcGolden, string) {
	t.Helper()
	dir := os.Getenv("GUIZHI_WIKICOMPILER_GOLDEN_DIR")
	if dir == "" {
		dir = os.Getenv("GUIZHI_GOLDEN_DIR")
	}
	path := filepath.Join(dir, "golden-wikicompiler.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skip("未找到 golden-wikicompiler.json（设置 GUIZHI_WIKICOMPILER_GOLDEN_DIR）")
	}
	var g wcGolden
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	return g, dir
}

func TestWikiBlocksGolden(t *testing.T) {
	g, _ := loadWikiCompilerGolden(t)
	if len(g.Blocks) == 0 {
		t.Fatal("基线没有分块用例")
	}
	for _, c := range g.Blocks {
		t.Run(c.Name, func(t *testing.T) {
			m := wikiMaterial{ID: "x", Title: c.Title, Content: c.Content, ReviewStatus: "clear"}
			if c.Transcript != nil {
				m.Transcript = *c.Transcript
			}
			if got := wikiFingerprint(m); got != c.Fingerprint {
				t.Errorf("指纹 %s want %s", got, c.Fingerprint)
			}
			got := wikiBlocks(m)
			if len(got) != len(c.Blocks) {
				t.Fatalf("块数 %d want %d", len(got), len(c.Blocks))
			}
			for i, w := range c.Blocks {
				b := got[i]
				gb := wcBlock{b.Key, b.Hash, b.Field, b.Section, b.Start, b.End, b.Text}
				if gb != w {
					t.Errorf("块[%d] 不一致\n got  %+v\n want %+v", i, wcShort(gb), wcShort(w))
				}
			}
		})
	}
}

func wcShort(b wcBlock) wcBlock {
	if len(b.Text) > 80 {
		b.Text = b.Text[:80] + "…"
	}
	return b
}

var reWcUUID = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
var reWcCreated = regexp.MustCompile(`"createdAt":\d+,?`)

// wcCanon 键排序的规范 JSON（Go 的 map 序列化本来就按键排序）。
func wcNormalize(t *testing.T, v any, uuids map[string]string) any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	// 先走一遍通用解码再编码，保证嵌套 map 的键序、数字表示一致
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(generic)
	text := reWcUUID.ReplaceAllStringFunc(string(raw), func(m string) string {
		if _, ok := uuids[m]; !ok {
			uuids[m] = fmt.Sprintf("<uuid%d>", len(uuids)+1)
		}
		return uuids[m]
	})
	text = strings.ReplaceAll(reWcCreated.ReplaceAllString(text, ""), ",}", "}")
	var out any
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("规范化后不是合法 JSON: %v\n%s", err, text)
	}
	return out
}

func wcDump(t *testing.T, l *Library, sqls map[string]string) any {
	t.Helper()
	keys := make([]string, 0, len(sqls))
	for k := range sqls {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := map[string]any{}
	for _, k := range keys {
		rows, err := l.db.Query(context.Background(), sqls[k])
		if err != nil {
			t.Fatalf("转储 %s: %v", k, err)
		}
		list := make([]any, 0, len(rows.Values))
		for _, r := range rows.Values {
			m := map[string]any{}
			for i, c := range rows.Columns {
				m[c] = r[i]
			}
			list = append(list, m)
		}
		out[k] = list
	}
	return out
}

func wcJSON(v any) string {
	b, _ := json.Marshal(v)
	s := string(b)
	if len(s) > 1500 {
		s = s[:1500] + "…"
	}
	return s
}

// wcDiff 找出两个 JSON 值的第一处差异路径，便于定位。
func wcDiff(path string, a, b any) string {
	if reflect.DeepEqual(a, b) {
		return ""
	}
	switch x := a.(type) {
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok {
			return path + ": 类型不同"
		}
		keys := map[string]bool{}
		for k := range x {
			keys[k] = true
		}
		for k := range y {
			keys[k] = true
		}
		sorted := make([]string, 0, len(keys))
		for k := range keys {
			sorted = append(sorted, k)
		}
		sort.Strings(sorted)
		for _, k := range sorted {
			if d := wcDiff(path+"."+k, x[k], y[k]); d != "" {
				return d
			}
		}
	case []any:
		y, ok := b.([]any)
		if !ok {
			return path + ": 类型不同"
		}
		for i := 0; i < len(x) || i < len(y); i++ {
			var xi, yi any
			if i < len(x) {
				xi = x[i]
			}
			if i < len(y) {
				yi = y[i]
			}
			if d := wcDiff(fmt.Sprintf("%s[%d]", path, i), xi, yi); d != "" {
				return d
			}
		}
	}
	return fmt.Sprintf("%s: got %s want %s", path, wcJSON(a), wcJSON(b))
}

func TestWikiCompilerScenarios(t *testing.T) {
	g, dir := loadWikiCompilerGolden(t)
	names := make([]string, 0, len(g.Scenarios))
	for n := range g.Scenarios {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		steps := g.Scenarios[name]
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			work := filepath.Join(t.TempDir(), "knowledge.db")
			data, err := os.ReadFile(filepath.Join(dir, "wikicompiler-base.db"))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(work, data, 0o600); err != nil {
				t.Fatal(err)
			}
			db, err := sqlite.Open(ctx, work, sqlite.OpenOptions{})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { db.Close() })
			l := &Library{db: db}
			b := &Bridge{handlers: map[string]handler{}}
			l.registerWikiCompiler(b)

			uuids := map[string]string{}
			var job string
			var lastWork map[string]any
			// 在 jsvalue 模型上替换引用：保持键序（贡献 JSON 的键序会原样落库，必须与 TS 一致）
			var resolve func(v any) any
			resolve = func(v any) any {
				switch x := v.(type) {
				case jsArr:
					out := make(jsArr, len(x))
					for i, e := range x {
						out[i] = resolve(e)
					}
					return out
				case *jsObj:
					if ref, ok := x.get("$ref").(string); ok {
						switch ref {
						case "job":
							return job
						case "work":
							if lastWork == nil {
								return jsNull
							}
							raw, _ := json.Marshal(lastWork)
							w, err := jsParse(string(raw))
							if err != nil {
								t.Fatal(err)
							}
							if s, ok := x.get("setItemId").(string); ok {
								w.(*jsObj).set("itemId", s)
							}
							if s, ok := x.get("setBlockKey").(string); ok {
								w.(*jsObj).get("block").(*jsObj).set("key", s)
							}
							return w
						case "suggestionToken":
							page, _ := x.get("page").(string)
							rows, err := l.db.Query(ctx, "SELECT draft_json FROM wiki_page_suggestions WHERE page_id=?", page)
							if err != nil || len(rows.Values) == 0 {
								return ""
							}
							tok, err := l.wikiSuggestionToken(ctx, page, rows.Values[0][0].(string))
							if err != nil {
								t.Fatal(err)
							}
							return tok
						}
					}
					out := newObj()
					for _, k := range x.keys() {
						out.set(k, resolve(x.get(k)))
					}
					return out
				}
				return v
			}
			for i, step := range steps {
				label := fmt.Sprintf("步骤 %d", i)
				var gotResp any
				switch step.Op {
				case "sql":
					if _, err := db.Transaction(ctx, []sqlite.Statement{{SQL: step.SQL, Args: step.Args}}); err != nil {
						t.Fatalf("%s sql: %v", label, err)
					}
				case "register":
					if err := l.wikiInterrupt(ctx); err != nil {
						t.Fatalf("%s interrupt: %v", label, err)
					}
				case "call":
					parsed, err := jsParse(string(step.Input))
					if err != nil {
						t.Fatal(err)
					}
					in := resolve(parsed)
					wcNormalize(t, jsToPlain(in), uuids) // 与 TS 录制顺序一致：先规范化输入，再响应，再转储（uuid 编号按出现顺序）
					text, _ := jsStringify(in)
					out, err := b.handlers[chWikiCompiler](ctx, []json.RawMessage{json.RawMessage(text)})
					if err != nil {
						t.Fatalf("%s handler: %v", label, err)
					}
					var resp map[string]any
					rb, _ := json.Marshal(out)
					_ = json.Unmarshal(rb, &resp)
					action := jsGet(in, "action")
					if jb, ok := resp["job"].(map[string]any); ok && action == "start" {
						job, _ = jb["id"].(string)
					}
					if w, ok := resp["work"].(map[string]any); ok && action == "next" {
						lastWork = w
					}
					gotResp = wcNormalize(t, resp, uuids)
					var want any
					_ = json.Unmarshal(step.Response, &want)
					if d := wcDiff("响应", gotResp, want); d != "" {
						t.Errorf("%s 响应不一致：%s", label, d)
					}
				}
				var wantDump any
				_ = json.Unmarshal(step.Dump, &wantDump)
				gotDump := wcNormalize(t, wcDump(t, l, g.DumpSQL), uuids)
				if d := wcDiff("转储", gotDump, wantDump); d != "" {
					t.Errorf("%s 转储不一致：%s", label, d)
					return // 状态已分叉，后面的比对没有意义
				}
			}
		})
	}
}
