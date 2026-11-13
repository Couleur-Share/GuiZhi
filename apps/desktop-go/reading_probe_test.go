package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 在真实隐藏 WebView2 里运行阅读页：正常页通过；脚本抛错得到带编号的故障；同步死循环触发五秒无响应故障。
func TestReadingProbeInWebView2(t *testing.T) {
	if testing.Short() {
		t.Skip("-short：跳过需要启动浏览器的测试")
	}
	dir := os.Getenv("GUIZHI_THEMED_GOLDEN_DIR")
	if dir == "" {
		t.Skip("未设置 GUIZHI_THEMED_GOLDEN_DIR")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "golden-themed.json"))
	if err != nil {
		t.Fatal(err)
	}
	var g struct {
		Recs []forumRec `json:"recs"`
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	var base string
	for _, r := range g.Recs {
		if r.Kind != "thmV3Inner" || !strings.Contains(string(r.Output), `"ok"`) {
			continue
		}
		var in struct {
			Version     json.RawMessage `json:"version"`
			Interactive bool            `json:"interactive"`
			Partial     bool            `json:"partial"`
		}
		mustJSON(t, r.Input, &in)
		if !in.Interactive || in.Partial || !hasScripts(in.Version) {
			continue
		}
		base = string(in.Version)
		break
	}
	if base == "" {
		t.Skip("基线里没有带脚本的交互页面")
	}
	host := newRenderHost(func() *safeFetcher { return nil })
	t.Cleanup(host.Close)
	l := &Library{}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	withCode := func(code string) string {
		v, err := jsParse(base)
		if err != nil {
			t.Fatal(err)
		}
		page := v.(*jsObj)
		scripts := themedArr(page.get("design").(*jsObj).get("scripts"))
		scripts[0].(*jsObj).set("code", code)
		s, _ := jsStringify(page)
		return s
	}
	t.Run("正常脚本通过", func(t *testing.T) {
		msg, err := l.probeReadingPage(host, ctx, withCode("window.__ok=1;"))
		if err != nil || msg != "" {
			t.Fatalf("应通过：%q %v", msg, err)
		}
	})
	t.Run("脚本抛错得到带编号的故障", func(t *testing.T) {
		msg, err := l.probeReadingPage(host, ctx, withCode(`throw new Error("boom")`))
		if err != nil || !strings.Contains(msg, "运行失败：boom") {
			t.Fatalf("故障说明不符：%q %v", msg, err)
		}
		t.Log("故障说明：", msg)
	})
	t.Run("同步死循环触发无响应故障", func(t *testing.T) {
		old := readingHeartbeatBudget
		readingHeartbeatBudget = 2 * time.Second
		defer func() { readingHeartbeatBudget = old }()
		start := time.Now()
		msg, err := l.probeReadingPage(host, ctx, withCode(`for(;;){}`))
		t.Logf("耗时 %v，说明：%q，错误：%v", time.Since(start), msg, err)
		if err != nil || (!strings.Contains(msg, "未响应") && msg != "阅读交互初始化未完成") {
			t.Fatalf("死循环应被判为故障：%q %v", msg, err)
		}
	})
	t.Run("没有可运行脚本时跳过", func(t *testing.T) {
		v, _ := jsParse(base)
		for _, s := range themedArr(v.(*jsObj).get("design").(*jsObj).get("scripts")) {
			s.(*jsObj).set("status", "failed")
		}
		s, _ := jsStringify(v)
		if msg, err := l.probeReadingPage(host, ctx, s); msg != "" || err != nil {
			t.Fatalf("应跳过：%q %v", msg, err)
		}
	})
}

func hasScripts(raw json.RawMessage) bool {
	v, err := jsParse(string(raw))
	if err != nil {
		return false
	}
	d, ok := jsGet(v, "design").(*jsObj)
	return ok && len(themedArr(d.get("scripts"))) > 0
}
