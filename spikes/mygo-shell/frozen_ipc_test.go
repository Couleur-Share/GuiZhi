package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestFrozenStubs(t *testing.T) {
	b := &Bridge{handlers: map[string]handler{}, sends: map[string]func([]json.RawMessage){}, seen: map[string]int{}, missing: map[string]int{}}
	real := false
	b.handlers["research:get"] = func(context.Context, []json.RawMessage) (any, error) { real = true; return nil, nil }
	b.registerFrozenStubs()
	if v, err := b.handlers["research:list"](context.Background(), nil); err != nil || len(v.([]any)) != 0 {
		t.Fatalf("research:list 应返回空列表：%v %v", v, err)
	}
	if _, err := b.handlers["research:create"](context.Background(), nil); err == nil || !strings.Contains(err.Error(), "已冻结") {
		t.Fatalf("冻结频道应如实报错：%v", err)
	}
	// 已有真实处理器的频道不被桩覆盖
	if _, _ = b.handlers["research:get"](context.Background(), nil); !real {
		t.Fatal("不应覆盖已有处理器")
	}
	// 非冻结频道不补桩，保持「未实现」可见
	if b.handlers["knowledge:list"] != nil {
		t.Fatal("非冻结频道不应补桩")
	}
	miss := b.unimplementedChannels()
	for _, m := range miss {
		if isFrozenChannel(strings.TrimPrefix(m, "(send) ")) {
			t.Errorf("冻结频道不应再算未实现：%s", m)
		}
	}
	if len(miss) == 0 {
		t.Fatal("没有注册任何真实处理器时应报告未实现")
	}
}

func TestGeneratedChannelListsSane(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range rendererInvokeChannels {
		if seen[c] || !strings.Contains(c, ":") {
			t.Errorf("频道清单异常：%q", c)
		}
		seen[c] = true
	}
	for _, c := range []string{"knowledge:list", "settings:get", "import:enqueue", "research:list", "window:isVisible"} {
		if !seen[c] {
			t.Errorf("清单缺少 %s（需重新运行 gen-channels.mts）", c)
		}
	}
}
