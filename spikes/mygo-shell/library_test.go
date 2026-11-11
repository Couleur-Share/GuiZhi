package main

// 一致性测试：Go 的输出必须与 TS（packages/db）在同一份数据库上的输出逐字段相等。
// 先生成基准：pnpm dlx tsx spikes/mygo-db/make-golden.mts <目录>
// 再运行：GUIZHI_GOLDEN_DIR=<目录> go test ./...

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/egoist/mygo/plugins/sqlite"
)

type golden struct {
	Fts []struct {
		Input     string  `json:"input"`
		Mode      string  `json:"mode"`
		Match     *string `json:"match"`
		Segmented string  `json:"segmented"`
	} `json:"fts"`
	Collections json.RawMessage `json:"collections"`
	Tags        json.RawMessage `json:"tags"`
	List        []struct {
		Query  json.RawMessage `json:"query"`
		Result json.RawMessage `json:"result"`
		Error  string          `json:"error"`
	} `json:"list"`
	Inbox   json.RawMessage `json:"inbox"`
	Imports []struct {
		Query  json.RawMessage `json:"query"`
		Result json.RawMessage `json:"result"`
		Error  string          `json:"error"`
	} `json:"imports"`
	Counts []struct {
		Query  json.RawMessage `json:"query"`
		Result json.RawMessage `json:"result"`
	} `json:"counts"`
}

func loadGolden(t *testing.T) (golden, *Library) {
	t.Helper()
	dir := os.Getenv("GUIZHI_GOLDEN_DIR")
	if dir == "" {
		t.Skip("未设置 GUIZHI_GOLDEN_DIR")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	var g golden
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	// 拷贝一份再打开，不改基准库
	work := filepath.Join(t.TempDir(), "knowledge.db")
	data, err := os.ReadFile(filepath.Join(dir, "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(work, data, 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := sqlite.Open(context.Background(), work, sqlite.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return g, &Library{db: db}
}

// normalize 经 JSON 往返，抹掉 Go/JS 在数字类型与键顺序上的表示差异。
func normalize(t *testing.T, v any) any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func rawToAny(t *testing.T, raw json.RawMessage) any {
	t.Helper()
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestFtsParity(t *testing.T) {
	g, _ := loadGolden(t)
	for _, c := range g.Fts {
		got, ok := BuildFtsMatchQuery(c.Input, FtsMatchMode(c.Mode))
		switch {
		case c.Match == nil && ok:
			t.Errorf("%q(%s)：TS 无结果，Go 得到 %q", c.Input, c.Mode, got)
		case c.Match != nil && (!ok || got != *c.Match):
			t.Errorf("%q(%s)：TS=%q Go=%q(ok=%v)", c.Input, c.Mode, *c.Match, got, ok)
		}
		if seg := SegmentTextForFts(c.Input); seg != c.Segmented {
			t.Errorf("segment %q：TS=%q Go=%q", c.Input, c.Segmented, seg)
		}
	}
}

func TestCollectionAndTagListParity(t *testing.T) {
	g, lib := loadGolden(t)
	ctx := context.Background()
	cols, err := lib.collectionList(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(normalize(t, cols), rawToAny(t, g.Collections)) {
		t.Errorf("collection:list 不一致\nGo: %v\nTS: %s", normalize(t, cols), g.Collections)
	}
	tags, err := lib.tagList(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(normalize(t, tags), rawToAny(t, g.Tags)) {
		t.Errorf("tag:list 不一致\nGo: %v\nTS: %s", normalize(t, tags), g.Tags)
	}
}

func TestKnowledgeCountsParity(t *testing.T) {
	g, lib := loadGolden(t)
	ctx := context.Background()
	for _, c := range g.Counts {
		q := FacetCountsQuery{Scope: "all"}
		if string(c.Query) != "null" {
			if err := json.Unmarshal(c.Query, &q); err != nil {
				t.Fatal(err)
			}
		}
		got, err := lib.knowledgeCounts(ctx, q)
		if err != nil {
			t.Errorf("%s：%v", c.Query, err)
			continue
		}
		if !reflect.DeepEqual(normalize(t, got), rawToAny(t, c.Result)) {
			t.Errorf("knowledge:counts %s 不一致\nGo: %v\nTS: %s", c.Query, normalize(t, got), c.Result)
		}
	}
}

func TestKnowledgeListParity(t *testing.T) {
	g, lib := loadGolden(t)
	ctx := context.Background()
	if len(g.List) == 0 {
		t.Fatal("golden 中没有 list 用例")
	}
	for i, c := range g.List {
		var q ListQuery
		if err := json.Unmarshal(c.Query, &q); err != nil {
			t.Fatal(err)
		}
		got, err := lib.knowledgeList(ctx, q)
		if c.Error != "" {
			if err == nil || err.Error() != c.Error {
				t.Errorf("#%d %s：期望错误 %q，得到 %v", i, c.Query, c.Error, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("#%d %s：%v", i, c.Query, err)
			continue
		}
		if want := rawToAny(t, c.Result); !reflect.DeepEqual(normalize(t, got), want) {
			gb, _ := json.MarshalIndent(normalize(t, got), "", " ")
			t.Errorf("#%d knowledge:list %s 不一致\nGo: %s\nTS: %s", i, c.Query, gb, c.Result)
		}
	}
}
