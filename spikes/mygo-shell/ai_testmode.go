package main

// -aitest 模式：把 spikes/mygo-ai 的对拍运行器注入真实渲染页，通过真实 preload 的 window.api.ai 跑同一批场景，
// 结果写成 JSON，与 TS 基线（ts-run.mts）比较。事件从 Go 推到页面、取消走 Bridge.Send 都是真实链路。

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/egoist/mygo"
)

func runAITest(p *mygo.Page, base, scenPath, runnerPath, outPath string, timeout time.Duration) error {
	runner, err := os.ReadFile(runnerPath)
	if err != nil {
		return err
	}
	scen, err := os.ReadFile(scenPath)
	if err != nil {
		return err
	}
	// 等页面里的 window.api.ai 就绪（preload 已执行即可，不必等整个应用渲染完）
	deadline := time.Now().Add(timeout)
	for {
		if v, err := p.Eval(`typeof window.api === 'object' && typeof window.api.ai === 'object'`); err == nil && v == true {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("等待 window.api.ai 超时")
		}
		time.Sleep(50 * time.Millisecond)
	}
	src := strings.Replace(string(runner), "export async function runAll", "async function runAll", 1)
	js := fmt.Sprintf(`(() => {
  window.__aiDone = false; window.__aiResult = '';
  %s
  runAll(%s, window.api.ai, %q)
    .then((r) => { window.__aiResult = JSON.stringify(r); window.__aiDone = true; })
    .catch((e) => { window.__aiResult = 'ERR ' + (e && e.stack || e); window.__aiDone = true; });
  return 1;
})()`, src, string(scen), base)
	if _, err := p.Eval(js); err != nil {
		return fmt.Errorf("注入运行器：%w", err)
	}
	for {
		if v, err := p.Eval(`window.__aiDone`); err == nil && v == true {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("场景运行超时")
		}
		time.Sleep(100 * time.Millisecond)
	}
	v, err := p.Eval(`window.__aiResult`)
	if err != nil {
		return err
	}
	s, _ := v.(string)
	if strings.HasPrefix(s, "ERR ") {
		return fmt.Errorf("页面内运行失败：%s", s)
	}
	return os.WriteFile(outPath, []byte(s), 0o644)
}
