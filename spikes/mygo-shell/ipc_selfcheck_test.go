package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUnimplementedChannelsReportsMissing(t *testing.T) {
	b := &Bridge{handlers: map[string]handler{}, sends: map[string]func([]json.RawMessage){}, seen: map[string]int{}, missing: map[string]int{}}
	if len(b.unimplementedChannels()) == 0 {
		t.Fatal("没有注册任何处理器时应报告未实现")
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
	for _, c := range []string{"knowledge:list", "settings:get", "import:enqueue", "window:isVisible"} {
		if !seen[c] {
			t.Errorf("清单缺少 %s（需重新运行 gen-channels.mts）", c)
		}
	}
	// 已移除功能的频道不应再出现在渲染进程可调用清单里
	for _, prefix := range []string{"research:", "illustration:", "discovery:", "discoveryView:", "discoveryCandidate:", "readingGraphics:", "mobile-capture:", "reading-sync:", "platformCapture:discover", "platformCapture:search", "platformCapture:cancelDiscovery"} {
		for c := range seen {
			if strings.HasPrefix(c, prefix) {
				t.Errorf("已移除功能的频道仍在清单里：%s", c)
			}
		}
	}
}
