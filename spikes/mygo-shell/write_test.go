package main

// 写入频道的差分测试：同一串操作分别由 TS DAO 和 Go 执行，比较「返回值 + 写后的原始表内容」。
// 比较前统一抹平随机 uuid（按内容排序后编号）与当前时间戳，其余字段（含 FTS 分词文本、标签关联、
// fts 映射表）必须逐字节一致。

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/egoist/mygo/plugins/sqlite"
)

type writeOp struct {
	Fn   string            `json:"fn"`
	Args []json.RawMessage `json:"args"`
}

type writeScenario struct {
	Ops     []writeOp `json:"ops"`
	Results []struct {
		Value json.RawMessage `json:"value"`
		Error *string         `json:"error"`
	} `json:"results"`
	Dump map[string][][]any `json:"dump"`
}

type writeGolden struct {
	DumpSQL map[string]string        `json:"dumpSql"`
	Writes  map[string]writeScenario `json:"writes"`
}

// 抹平时间戳的阈值：基准库造的数据都在 1.7e12 附近，运行时 Date.now() 在 1.75e12 之后
const nowThreshold = 1.75e12

func openWriteCopy(t *testing.T, dir, base string) *Library {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, base))
	if err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(t.TempDir(), "knowledge.db")
	if err := os.WriteFile(work, data, 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := sqlite.Open(context.Background(), work, sqlite.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return &Library{db: db}
}

func queryIDs(t *testing.T, l *Library, sql string) map[string]bool {
	t.Helper()
	rows, err := l.db.Query(context.Background(), sql)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, r := range rows.Values {
		out[r[0].(string)] = true
	}
	return out
}

func runGoOps(t *testing.T, l *Library, ops []writeOp) []map[string]any {
	t.Helper()
	b := &Bridge{handlers: map[string]handler{}}
	l.registerWrite(b)
	l.registerCatalogWrite(b)
	l.registerInboxWrite(b)
	l.registerAskSessions(b)
	l.registerAIUsage(b)
	l.registerSemantic(b)
	l.registerWiki(b)
	l.registerKnowledgeDraft(b)
	jt := newJobTestEnv(l)
	l.registerBackgroundJobs(b)
	ctx := context.Background()
	out := make([]map[string]any, 0, len(ops))
	for _, op := range ops {
		var (
			v   any
			err error
		)
		switch op.Fn {
		case "deleteForever":
			v, err = l.deleteForever(ctx, normalizeIDs(op.Args[0]))
		case "emptyTrash":
			v, err = l.emptyTrash(ctx)
		case "job.clock", "job.register", "job.schedule", "job.unschedule", "job.tick", "job.settle", "job.drain":
			v, err = jt.run(ctx, strings.TrimPrefix(op.Fn, "job."), op.Args)
		default:
			channel := "knowledge:" + op.Fn
			if ns, method, found := strings.Cut(op.Fn, "."); found {
				channel = ns + ":" + method
				if ns == "draft" {
					channel = "knowledge:saveDraft"
				}
				if ns == "job" {
					channel = "backgroundJob:" + method
					if method == "sync" {
						channel = "backgroundJob:syncRenderer"
					}
				}
				if ns == "ask" && (method == "query" || method == "meta") {
					channel = "askSession:" + method
				}
			}
			h, ok := b.handlers[channel]
			if !ok {
				t.Fatalf("频道未登记: %s", op.Fn)
			}
			v, err = h(ctx, resolveCursorArgs(op.Args, out))
		}
		if err != nil {
			out = append(out, map[string]any{"error": err.Error()})
		} else {
			out = append(out, map[string]any{"value": v})
		}
	}
	return out
}

func dumpGo(t *testing.T, l *Library, sqls map[string]string) map[string][][]any {
	t.Helper()
	out := map[string][][]any{}
	for name, sql := range sqls {
		rows, err := l.db.Query(context.Background(), sql)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		out[name] = rows.Values
	}
	return out
}

// normalizeScenario 对一侧的（返回值, 转储）做同一套抹平，返回可直接比较的结构。
func normalizeScenario(t *testing.T, baseItems, baseTags, baseCollections, baseWikiPages, baseWikiRevs map[string]bool, results any, dump map[string][][]any) (any, map[string]any) {
	t.Helper()
	resAny := normalize(t, results)
	dumpAny := normalize(t, dump).(map[string]any)

	rows := func(name string) [][]any {
		var out [][]any
		for _, r := range dumpAny[name].([]any) {
			out = append(out, r.([]any))
		}
		return out
	}
	idMap := map[string]string{}
	assign := func(table string, known map[string]bool, prefix string, keys ...int) {
		var fresh [][]any
		for _, r := range rows(table) {
			if id, _ := r[0].(string); !known[id] {
				fresh = append(fresh, r)
			}
		}
		sort.SliceStable(fresh, func(i, j int) bool {
			for _, k := range keys {
				a, b := fmt.Sprint(fresh[i][k]), fmt.Sprint(fresh[j][k])
				if a != b {
					return a < b
				}
			}
			return false
		})
		for n, r := range fresh {
			idMap[r[0].(string)] = fmt.Sprintf("%s#%d", prefix, n+1)
		}
	}
	assign("items", baseItems, "ITEM", 1, 2)
	assign("tags", baseTags, "TAG", 1)
	assign("collections", baseCollections, "COLL", 1)
	assign("wikiPages", baseWikiPages, "PAGE", 2)
	assign("wikiRevisions", baseWikiRevs, "REV", 2, 4, 5)

	var walk func(v any) any
	walk = func(v any) any {
		switch x := v.(type) {
		case string:
			if m, ok := idMap[x]; ok {
				return m
			}
			return x
		case float64:
			if x >= nowThreshold {
				return "NOW"
			}
			return x
		case []any:
			for i := range x {
				x[i] = walk(x[i])
			}
			return x
		case map[string]any:
			for k := range x {
				x[k] = walk(x[k])
			}
			// 以 id 为键的对象（如 backlinkCounts）：键也要换成稳定占位
			for k, val := range x {
				if m, ok := idMap[k]; ok {
					delete(x, k)
					x[m] = val
				}
			}
			if _, ok := x["lastSearchMs"].(float64); ok {
				x["lastSearchMs"] = "MS" // 检索耗时是观测值，只比较「有没有」
			}
			return x
		}
		return v
	}
	// 已知且有意保留的差异：TS 用 localeCompare（跟随操作系统语言）给标签排序后拼进 fts 的 tags 列，
	// 同一批标签在 zh-CN 与 en 下顺序不同，Go 的 x/text 也不会逐位复刻 ICU。该列只参与分词匹配，
	// 顺序不影响命中，所以按词的集合比较（词本身、数量必须一致）。
	for _, r := range dumpAny["fts"].([]any) {
		row := r.([]any)
		words := strings.Fields(row[3].(string))
		sort.Strings(words)
		row[3] = strings.Join(words, " ")
	}
	resAny = walk(resAny)
	resAny = sortNowTies(resAny)
	normalized := map[string]any{}
	for name := range dumpAny {
		rs := walk(dumpAny[name]).([]any)
		sort.SliceStable(rs, func(i, j int) bool {
			a, _ := json.Marshal(rs[i])
			b, _ := json.Marshal(rs[j])
			return string(a) < string(b)
		})
		normalized[name] = rs
	}
	return resAny, normalized
}

func TestWriteParity(t *testing.T) {
	dir := os.Getenv("GUIZHI_GOLDEN_DIR")
	if dir == "" {
		t.Skip("未设置 GUIZHI_GOLDEN_DIR")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	var g writeGolden
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	if len(g.Writes) == 0 {
		t.Fatal("golden 里没有 writes，请重新生成基准")
	}
	names := make([]string, 0, len(g.Writes))
	for n := range g.Writes {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, name := range names {
		sc := g.Writes[name]
		t.Run(name, func(t *testing.T) {
			base := "knowledge.db"
			switch {
			case strings.HasPrefix(name, "semantic."):
				base = "semantic-base.db" // 语义场景用播种过向量的独立基准库
			case strings.HasPrefix(name, "wiki.flush"):
				base = "wiki-flush-base.db" // 带失效记录与贡献的 Wiki 库
			case strings.HasPrefix(name, "wiki."):
				base = "wiki-base.db"
			}
			l := openWriteCopy(t, dir, base)
			baseItems := queryIDs(t, l, "SELECT id FROM knowledge_items")
			baseTags := queryIDs(t, l, "SELECT id FROM tags")
			baseCollections := queryIDs(t, l, "SELECT id FROM collections")
			baseWikiPages := queryIDs(t, l, "SELECT id FROM wiki_pages")
			baseWikiRevs := queryIDs(t, l, "SELECT id FROM wiki_page_revisions")

			goResults := runGoOps(t, l, sc.Ops)
			goDump := dumpGo(t, l, g.DumpSQL)

			// TS 侧结果：{value} 或 {error}
			tsResults := make([]map[string]any, 0, len(sc.Results))
			for _, r := range sc.Results {
				if r.Error != nil {
					tsResults = append(tsResults, map[string]any{"error": true})
					continue
				}
				tsResults = append(tsResults, map[string]any{"value": rawToAny(t, r.Value)})
			}
			// 报错只比较「是否报错」：错误文案来自 SQLite 驱动，措辞不属于契约
			for _, r := range goResults {
				if _, ok := r["error"]; ok {
					r["error"] = true
				}
			}

			wantRes, wantDump := normalizeScenario(t, baseItems, baseTags, baseCollections, baseWikiPages, baseWikiRevs, tsResults, sc.Dump)
			gotRes, gotDump := normalizeScenario(t, baseItems, baseTags, baseCollections, baseWikiPages, baseWikiRevs, goResults, goDump)
			if !reflect.DeepEqual(wantRes, gotRes) {
				w, _ := json.Marshal(wantRes)
				gt, _ := json.Marshal(gotRes)
				t.Errorf("返回值不一致（只显示首个差异附近）\nTS: %s\nGo: %s", diffWindow(w, gt, true), diffWindow(w, gt, false))
			}
			for table := range wantDump {
				if !reflect.DeepEqual(wantDump[table], gotDump[table]) {
					w, _ := json.MarshalIndent(wantDump[table], "", " ")
					gt, _ := json.MarshalIndent(gotDump[table], "", " ")
					t.Errorf("表 %s 不一致\nTS: %s\nGo: %s", table, w, gt)
				}
			}
		})
	}
}

// resolveCursorArgs 把参数里的 {"__cursorOf": n} 换成前面第 n 步结果的 nextCursor（与 make-golden.mts 的 resolveArgs 对应）。
func resolveCursorArgs(args []json.RawMessage, results []map[string]any) []json.RawMessage {
	out := make([]json.RawMessage, len(args))
	for i, raw := range args {
		var walk func(v any) any
		v, err := jsParse(string(raw))
		if err != nil {
			out[i] = raw
			continue
		}
		walk = func(v any) any {
			switch x := v.(type) {
			case *jsObj:
				if n, ok := x.get("__cursorOf").(float64); ok {
					if int(n) < len(results) {
						if m, ok := results[int(n)]["value"].(map[string]any); ok {
							if c, ok := m["nextCursor"].(string); ok {
								return c
							}
						}
					}
					return jsNull
				}
				for _, k := range x.keys() {
					x.m[k] = walk(x.m[k])
				}
			case jsArr:
				for j := range x {
					x[j] = walk(x[j])
				}
			}
			return v
		}
		s, _ := jsStringify(walk(v))
		out[i] = json.RawMessage(s)
	}
	return out
}

// diffWindow 取两段 JSON 首个不同位置前后各约 160 字节；用于避免大结果刷屏。
func diffWindow(a, b []byte, first bool) string {
	i := 0
	for i < len(a) && i < len(b) && a[i] == b[i] {
		i++
	}
	s := b
	if first {
		s = a
	}
	lo, hi := max(0, i-160), min(len(s), i+160)
	return fmt.Sprintf("…@%d… %s", i, s[lo:hi])
}

// sortNowTies：带 updatedAt 的对象数组里，updatedAt 同为 "NOW" 的相邻元素本来就没有确定顺序
// （TS 逐页取时间戳，Go 一批语句同一毫秒），按 id 排序后再比较；其余顺序照旧。
func sortNowTies(v any) any {
	switch x := v.(type) {
	case []any:
		for i := range x {
			x[i] = sortNowTies(x[i])
		}
		// 图谱的边没有 ORDER BY：SQLite 按主键（含随机 uuid）索引扫描，顺序无意义
		if len(x) > 0 {
			if first, ok := x[0].(map[string]any); ok {
				if _, hasS := first["source"]; hasS {
					if _, hasT := first["target"]; hasT {
						sort.SliceStable(x, func(a, b int) bool {
							ma, _ := x[a].(map[string]any)
							mb, _ := x[b].(map[string]any)
							ka, kb := fmt.Sprint(ma["source"], "\x00", ma["target"]), fmt.Sprint(mb["source"], "\x00", mb["target"])
							return ka < kb
						})
					}
				}
			}
		}
		isNow := func(e any) bool {
			m, ok := e.(map[string]any)
			return ok && m["updatedAt"] == "NOW"
		}
		for i := 0; i < len(x); {
			if !isNow(x[i]) {
				i++
				continue
			}
			j := i
			for j < len(x) && isNow(x[j]) {
				j++
			}
			group := x[i:j]
			sort.SliceStable(group, func(a, b int) bool {
				return fmt.Sprint(group[a].(map[string]any)["id"]) < fmt.Sprint(group[b].(map[string]any)["id"])
			})
			i = j
		}
		return x
	case map[string]any:
		for k := range x {
			x[k] = sortNowTies(x[k])
		}
		// 多个主进程任务并发执行时，handler 被调用的先后没有契约，按 id 排序后比较
		if h, ok := x["handled"].([]any); ok {
			sort.SliceStable(h, func(a, b int) bool {
				return fmt.Sprint(h[a].(map[string]any)["id"]) < fmt.Sprint(h[b].(map[string]any)["id"])
			})
		}
		return x
	}
	return v
}
