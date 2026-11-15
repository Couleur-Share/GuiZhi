package aiflow

// 全局问答里 golden 管不到的 Go 侧约束：

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"
)

// TestParseJSONFollowsJSONParse 固定 parseJSON 与 V8 的 JSON.parse 一致的几个边界：
// 溢出的数字是 ±Infinity 而不是整段报错；顶层值之后不能再有内容；JSON 之外的写法一律报错。
func TestParseJSONFollowsJSONParse(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    any
		wantErr bool
	}{
		{"对象与数组", `{"a":[1,2.5,"x",true,null]}`, map[string]any{"a": []any{1.0, 2.5, "x", true, nil}}, false},
		{"溢出是正无穷", `1e999`, math.Inf(1), false},
		{"溢出是负无穷", `-1e999`, math.Inf(-1), false},
		{"嵌套结构里的溢出", `{"a":{"b":[1e400]}}`, map[string]any{"a": map[string]any{"b": []any{math.Inf(1)}}}, false},
		{"下溢为零", `1e-999`, 0.0, false},
		{"超过 2^53 的整数取最近的 double", `9007199254740993`, 9007199254740992.0, false},
		{"尾随空白可以", "{\"a\":1} \n\t", map[string]any{"a": 1.0}, false},
		{"尾随内容报错", `{"a":1} x`, nil, true},
		{"两个顶层值报错", `{"a":1}{"b":2}`, nil, true},
		{"空文本报错", ``, nil, true},
		{"BOM 报错", "\ufeff{}", nil, true},
		{"重复的键取最后一个", `{"a":1,"a":2}`, map[string]any{"a": 2.0}, false},
		{"NaN 报错", `NaN`, nil, true},
		{"单引号报错", `{'a':1}`, nil, true},
		{"尾逗号报错", `[1,]`, nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseJSON(c.in)
			if (err != nil) != c.wantErr {
				t.Fatalf("parseJSON(%q) 的错误=%v，期望报错=%v", c.in, err, c.wantErr)
			}
			if !c.wantErr && !reflect.DeepEqual(got, c.want) {
				t.Errorf("parseJSON(%q)=%#v，期望 %#v", c.in, got, c.want)
			}
		})
	}
}

// storeProbe 在指定的存取调用进行中取消问答的 ctx，并记录各存取调用拿到的 ctx 是否已被取消。
type storeProbe struct {
	target string
	cancel context.CancelFunc
	called map[string]bool
	seen   map[string]error
}

func (p *storeProbe) enter(ctx context.Context, name string) {
	p.called[name] = true
	if name == p.target {
		p.cancel() // 用户在这次调用进行中点了「停止」
	}
	if err := ctx.Err(); err != nil {
		p.seen[name] = err
	}
}

// TestQAStoreCallsIgnoreCancellation 固定：Wiki 目录、页面检索、读条目与页面这几个存取调用是不可取消的
// （TS 里它们是不带 signal 的 IPC）。用户在调用进行中点了「停止」，调用拿到的 ctx 仍然有效，
// 否则真实的存取实现会返回 context canceled，引擎把它当成读取失败，行为就和 TS 不一样了；
// 取消只在检查点生效，读完之后才以「已取消」收场。golden 的假依赖不看 ctx，管不到这一点。
func TestQAStoreCallsIgnoreCancellation(t *testing.T) {
	const (
		search = `{"action":"search","query":"检索词"}`
		junk   = "我不会输出 JSON"
	)
	cases := []struct {
		name   string
		target string
		chats  []string
	}{
		{"Wiki 目录", "WikiCatalog", nil},
		{"Wiki 页面检索", "SearchWikiPages", []string{search}},
		// 检索结果里 Wiki 命中在前：编号 1 是页面 w1，编号 2、3 是条目 a、b
		{"读 Wiki 页面", "ReadWikiPage", []string{search, `{"action":"read","target":1}`}},
		{"循环里读条目", "ReadItem", []string{search, `{"action":"read","target":2}`}},
		// 兜底管线读完所有命中（两份）之后才检查取消：第二次读取发生在取消之后
		{"兜底管线读条目", "ReadItem", []string{junk, junk, junk}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			probe := &storeProbe{target: c.target, cancel: cancel, called: map[string]bool{}, seen: map[string]error{}}
			chats := append([]string{}, c.chats...)
			deps := QADeps{
				Chat: func(context.Context, []ChatMessage, ScenarioChatOptions) (ScenarioChatResult, error) {
					if len(chats) == 0 {
						t.Error("对话脚本已用尽：取消之后不该再发起对话")
						return ScenarioChatResult{}, errors.New("脚本已用尽")
					}
					next := chats[0]
					chats = chats[1:]
					return ScenarioChatResult{Content: next, Model: "m"}, nil
				},
				SearchItems: func(context.Context, string, int) ([]SearchHit, error) {
					return []SearchHit{{ID: "a", Title: "A", Snippet: "摘录"}, {ID: "b", Title: "B", Snippet: "摘录"}}, nil
				},
				ReadItem: func(readCtx context.Context, id string) (*QAItemContent, error) {
					probe.enter(readCtx, "ReadItem")
					return &QAItemContent{Title: id, Content: "正文 " + id}, nil
				},
				WikiCatalog: func(catalogCtx context.Context) ([]WikiCatalogEntry, error) {
					probe.enter(catalogCtx, "WikiCatalog")
					return nil, nil
				},
				SearchWikiPages: func(searchCtx context.Context, _ string, _ int) ([]WikiSearchHit, error) {
					probe.enter(searchCtx, "SearchWikiPages")
					return []WikiSearchHit{{ID: "w1", Title: "页面", Kind: WikiKindTopic, Summary: "摘要"}}, nil
				},
				ReadWikiPage: func(pageCtx context.Context, id string) (*WikiPageDetail, error) {
					probe.enter(pageCtx, "ReadWikiPage")
					return &WikiPageDetail{Page: WikiPage{ID: id, Title: "页面", Body: "正文"}}, nil
				},
			}

			_, err := (&QAEngine{Deps: deps}).Ask(ctx, "问题", nil, nil, nil)
			if !IsAborted(err) {
				t.Fatalf("取消之后应在检查点以「已取消」收场，实际：%v", err)
			}
			if !probe.called[c.target] {
				t.Fatalf("没有走到 %s，用例失效", c.target)
			}
			for name, seen := range probe.seen {
				t.Errorf("%s 拿到的 ctx 已被取消：%v", name, seen)
			}
		})
	}
}

// TestNewQADepsCallbacks 固定 NewQADeps 对两个可选回调的处理：onEvidence 原样转给引擎（没传就保持 nil，引擎据此跳过）；
// onWarning 没传时语义检索降级照常工作，不会因为回调为 nil 而 panic。
func TestNewQADepsCallbacks(t *testing.T) {
	store := &qaDepsStore{t: t}

	t.Run("onEvidence 透传", func(t *testing.T) {
		var got []QASourceRef
		deps := NewQADeps(&Runner{}, &Hybrid{}, store, nil, func(sources []QASourceRef) { got = sources })
		want := []QASourceRef{{Ordinal: 1, Kind: QAKindItem, RefID: "a", Title: "A"}}
		deps.OnEvidence(want)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("onEvidence 收到 %#v，期望 %#v", got, want)
		}
		if NewQADeps(&Runner{}, &Hybrid{}, store, nil, nil).OnEvidence != nil {
			t.Error("没传 onEvidence 时应保持 nil")
		}
	})

	t.Run("没传 onWarning 时语义检索降级不报错", func(t *testing.T) {
		var golden struct {
			Cases []struct {
				Name     string         `json:"name"`
				Settings map[string]any `json:"settings"`
				Script   []scriptStep   `json:"script"`
			} `json:"cases"`
		}
		loadGolden(t, "qa-deps", &golden)
		const fixture = "search：语义抛错告警，同一句话两次检索只回调一次"
		for _, c := range golden.Cases {
			if c.Name != fixture {
				continue
			}
			source := &qaDepsSource{fakeHybridSource: &fakeHybridSource{}}
			failure := "semantic index is rebuilding"
			source.spec.Throws = &failure
			source.fts.Entries = []goldenFTSEntry{{ID: "a", Title: "关键词标题-a"}}
			runner, _, _, _ := newSemanticRunner(c.Settings, c.Script, nil)
			hybrid := &Hybrid{Runner: runner, Source: source, Vectors: &QueryVectors{}, SemanticTimeout: 5 * time.Second}
			deps := NewQADeps(runner, hybrid, store, nil, nil)
			for _, query := range []string{"检索词一", "检索词二"} {
				hits, err := deps.SearchItems(context.Background(), query, 5)
				if err != nil || len(hits) != 1 || hits[0].ID != "a" {
					t.Fatalf("%q 应降级为关键词结果，实际 hits=%v err=%v", query, hits, err)
				}
			}
			return
		}
		t.Fatalf("golden 里没有用例 %q", fixture)
	})
}
