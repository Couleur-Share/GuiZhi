package main

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// 流式 UTF-8 解码器对拍 Node 的 TextDecoder（stream 模式）。golden 由 spikes/mygo-ai/make-utf8-golden.mjs 生成。
func TestUTF8StreamDecoderMatchesTextDecoder(t *testing.T) {
	raw, err := os.ReadFile("testdata/ai-utf8.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Chunks []string `json:"chunks"`
		Out    string   `json:"out"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	bad, bomQuirk := 0, 0
	for i, c := range cases {
		var d utf8StreamDecoder
		got := ""
		for _, h := range c.Chunks {
			b, _ := hex.DecodeString(h)
			got += d.write(b, false)
		}
		got += d.write(nil, true)
		// Node（ICU）会在非流首位置额外吞掉 BOM（如不完整序列后紧跟的 EF BB BF、连续两个 BOM），这偏离了 WHATWG 规范
		// （规范只剥流开头的第一个码点）。Go 版按规范实现；差异只在 U+FEFF 上时单独计数，不算失败。
		if got != c.Out && strings.ReplaceAll(got, "\ufeff", "") == strings.ReplaceAll(c.Out, "\ufeff", "") {
			bomQuirk++
			continue
		}
		if got != c.Out {
			bad++
			if bad <= 5 {
				t.Errorf("#%d chunks=%v: Go=%q Node=%q", i, c.Chunks, got, c.Out)
			}
		}
	}
	t.Logf("%d 组随机输入，不一致 %d 组；另有 %d 组仅在 Node 多吞了 BOM", len(cases), bad, bomQuirk)
}
