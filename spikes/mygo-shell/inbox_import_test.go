package main

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
)

// 把 NOW（运行时生成的时间戳）抹成固定值：收件箱里的语义索引汇总卡带 Date.now()。
func maskNow(v any) any {
	switch x := v.(type) {
	case float64:
		if x >= nowThreshold {
			return "NOW"
		}
	case []any:
		for i := range x {
			x[i] = maskNow(x[i])
		}
	case map[string]any:
		for k := range x {
			x[k] = maskNow(x[k])
		}
	}
	return v
}

func TestInboxListParity(t *testing.T) {
	g, lib := loadGolden(t)
	got, err := lib.inboxList(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	wantAny := rawToAny(t, g.Inbox)
	// 平台发现已从 Go 壳移除：TS 仍报告一个恒为 0 的 discovery-candidate 计数（夹具里没有候选），Go 不再提供该键
	if root, ok := wantAny.(map[string]any); ok {
		if counts, ok := root["counts"].(map[string]any); ok {
			if n, _ := counts["discovery-candidate"].(float64); n != 0 {
				t.Fatalf("夹具里不应再有发现候选：%v", counts["discovery-candidate"])
			}
			delete(counts, "discovery-candidate")
		}
	}
	want, have := maskNow(wantAny), maskNow(normalize(t, got))
	if !reflect.DeepEqual(want, have) {
		w, _ := json.MarshalIndent(want, "", " ")
		h, _ := json.MarshalIndent(have, "", " ")
		t.Fatalf("inbox:list 不一致\nTS: %s\nGo: %s", w, h)
	}
}

func TestImportListParity(t *testing.T) {
	g, lib := loadGolden(t)
	b := &Bridge{handlers: map[string]handler{}}
	lib.registerImportList(b)
	if len(g.Imports) == 0 {
		t.Fatal("golden 里没有 imports")
	}
	for i, c := range g.Imports {
		args := []json.RawMessage{}
		if len(c.Query) > 0 {
			args = append(args, c.Query)
		}
		got, err := b.handlers["import:list"](context.Background(), args)
		if c.Error != "" {
			if err == nil || err.Error() != c.Error {
				t.Errorf("case %d %s: 期望错误 %q，得到 %v", i, c.Query, c.Error, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("case %d %s: %v", i, c.Query, err)
			continue
		}
		want, have := rawToAny(t, c.Result), normalize(t, got)
		if !reflect.DeepEqual(want, have) {
			w, _ := json.MarshalIndent(want, "", " ")
			h, _ := json.MarshalIndent(have, "", " ")
			t.Errorf("case %d query=%s 不一致\nTS: %.1500s\nGo: %.1500s", i, c.Query, w, h)
		}
	}
}
