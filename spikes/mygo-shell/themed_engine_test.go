package main

import (
	"strings"
	"testing"
)

func TestThemedEngineRenderBlocks(t *testing.T) {
	out, err := themedCall("renderBlocks", "# 标题\n\n段落 **加粗**，含 <script>alert(1)</script> 与 [链接](https://a.example)。\n\n- 一\n- 二")
	if err != nil {
		t.Fatal(err)
	}
	t.Log(out)
	if !strings.Contains(out, `"id":"b0"`) || strings.Contains(out, `"html":"<p>段落 <strong>加粗</strong>，含 <script`) {
		t.Fatalf("out=%s", out)
	}
}
