package main

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

func (e *env) okJobs(n int) []job {
	var js []job
	for i := 0; i < n; i++ {
		js = append(js, job{
			url: func(id int) string { return fmt.Sprintf("%s/ok?n=%d", e.srv.base(), id) },
			o:   capOpts{allow: allowLoopbackHosts("127.0.0.1", "localhost")},
		})
	}
	return js
}

type fault struct {
	name   string
	path   string
	o      capOpts
	verify func(e *env, r taskResult) (bool, string)
}

func faults(e *env, _ int) {
	allow := allowLoopbackHosts("127.0.0.1", "localhost")
	home, _ := os.UserHomeDir()
	fs := []fault{
		{"服务端慢响应（6 秒，导航超时 2 秒）", "/slow?ms=6000", capOpts{navTimeout: 2 * time.Second, allow: allow}, func(e *env, r taskResult) (bool, string) {
			return !r.ok && (r.phase == "load" || r.phase == "navigate") && r.dur < 5*time.Second, fmt.Sprintf("phase=%s 耗时=%s 关闭=%s", r.phase, r.dur.Round(time.Millisecond), r.closeDur.Round(time.Millisecond))
		}},
		{"HTTP 500 仍会加载出页面（调用方必须自己看状态码）", "/status500", capOpts{allow: allow}, func(e *env, r taskResult) (bool, string) {
			return r.ok && r.title == "ERR500", fmt.Sprintf("ok=%v title=%q", r.ok, r.title)
		}},
		{"重定向死循环", "/redir-loop", capOpts{allow: allow}, func(e *env, r taskResult) (bool, string) {
			return !r.ok && strings.Contains(r.navErr, "TOO_MANY_REDIRECTS"), "navErr=" + r.navErr
		}},
		{"连接被重置", "/reset", capOpts{allow: allow}, func(e *env, r taskResult) (bool, string) {
			return !r.ok && r.navErr != "", "navErr=" + r.navErr
		}},
		{"下载被拒绝，不落盘，任务不挂死", "/dl", capOpts{navTimeout: 4 * time.Second, allow: allow}, func(e *env, r taskResult) (bool, string) {
			var found []string
			for _, p := range []string{filepath.Join(home, "Downloads", "cap.bin"), filepath.Join(e.b.userData, "cap.bin")} {
				if _, err := os.Stat(p); err == nil {
					found = append(found, p)
				}
			}
			return !r.ok && len(found) == 0 && r.dur < 8*time.Second, fmt.Sprintf("ok=%v phase=%s err=%s 落盘=%v 耗时=%s", r.ok, r.phase, r.err, found, r.dur.Round(time.Millisecond))
		}},
		{"alert/confirm/prompt 与 beforeunload 弹窗", "/dialog", capOpts{allow: allow}, func(e *env, r taskResult) (bool, string) {
			return r.ok && r.dialogs >= 3 && r.dur < 8*time.Second, fmt.Sprintf("ok=%v 弹窗=%d 耗时=%s err=%s", r.ok, r.dialogs, r.dur.Round(time.Millisecond), r.err)
		}},
		{"页面脚本死循环（主线程卡死）", "/hang", capOpts{evalTimeout: 3 * time.Second, settle: 500 * time.Millisecond, allow: allow}, func(e *env, r taskResult) (bool, string) {
			return !r.ok && r.phase == "evaluate" && r.closeDur < 8*time.Second, fmt.Sprintf("phase=%s 取值超时后关闭耗时=%s", r.phase, r.closeDur.Round(time.Millisecond))
		}},
		{"30 MB 超大页面", "/big", capOpts{navTimeout: 60 * time.Second, evalTimeout: 30 * time.Second, allow: allow}, func(e *env, r taskResult) (bool, string) {
			return r.ok && r.htmlLen > 10_000_000, fmt.Sprintf("ok=%v DOM 长度=%d 耗时=%s err=%s", r.ok, r.htmlLen, r.dur.Round(time.Millisecond), r.err)
		}},
		{"外部资源被拦截（TEST-NET 地址与 .invalid 域名）", "/ext", capOpts{allow: allow}, func(e *env, r taskResult) (bool, string) {
			return r.ok && r.blocked >= 2 && r.dur < 5*time.Second, fmt.Sprintf("ok=%v 拦截=%d 耗时=%s", r.ok, r.blocked, r.dur.Round(time.Millisecond))
		}},
		{"渲染进程崩溃（Page.crash）", "/ok?n=crash", capOpts{allow: allow, afterLoad: func(t *task) {
			t.d.callT(t.main, "Page.crash", nil, 3*time.Second)
			time.Sleep(500 * time.Millisecond)
		}, evalTimeout: 3 * time.Second}, func(e *env, r taskResult) (bool, string) {
			return !r.ok, fmt.Sprintf("ok=%v 收到崩溃事件=%v（事件不稳定，不能依赖；取值超时才是可靠信号） phase=%s err=%s", r.ok, r.crashed, r.phase, r.err)
		}},
	}
	for _, f := range fs {
		f := f
		jobs := append([]job{{url: func(id int) string { return e.srv.base() + f.path }, o: f.o}}, e.okJobs(6)...)
		res, _ := e.runJobs(jobs, 4)
		ok, detail := f.verify(e, res[0])
		e.check(f.name, ok, detail)
		noiseOK := 0
		for _, r := range res[1:] {
			if r.ok {
				noiseOK++
			}
		}
		e.check("  并发的 6 个正常任务不受影响", noiseOK == 6, fmt.Sprintf("%d/6", noiseOK))
		w, n := e.settleTargets(15 * time.Second)
		e.check("  target 回到基线", n == e.base, fmt.Sprintf("页面数=%d 等待=%s", n, w.Round(time.Millisecond)))
	}
}

func security(e *env, _ int) {
	allow := allowLoopbackHosts("127.0.0.1", "localhost")
	p := e.srv.port
	one := func(path string, o capOpts) taskResult {
		e.srv.resetSecret()
		res, _ := e.runJobs([]job{{url: func(id int) string { return e.srv.base() + path }, o: o}}, 1)
		e.settleTargets(10 * time.Second)
		return res[0]
	}
	hasVia := func(via string) bool {
		for _, s := range e.srv.secretHits() {
			if strings.Contains(s, "via="+via) {
				return true
			}
		}
		return false
	}

	// 正向对照：把 127.0.0.2 也放行，探针必须命中内网，否则下面“没命中”的结论不可信。
	wide := allowLoopbackHosts("127.0.0.1", "localhost", "127.0.0.2")
	e.srv.resetSecret()
	if resp, err := http.Get(fmt.Sprintf("http://127.0.0.2:%d/secret?via=selftest", p)); err == nil {
		resp.Body.Close()
	}
	e.check("自检：模拟站点的 127.0.0.2 监听可用", hasVia("selftest"), fmt.Sprint(e.srv.secretHits()))
	rr := one("/ssrf-redirect", capOpts{allow: wide})
	c1 := len(e.srv.secretHits()) > 0
	e.note("放行重定向探针：ok=%v phase=%s err=%s navErr=%s 拦截=%d 命中=%v", rr.ok, rr.phase, rr.err, rr.navErr, rr.blocked, e.srv.secretHits())
	ro := one("/oopif", capOpts{allow: wide, settle: 2 * time.Second})
	c2 := hasVia("iframe")
	e.note("放行 iframe 探针：ok=%v err=%s 拦截=%d 命中=%v", ro.ok, ro.err, ro.blocked, e.srv.secretHits())
	one("/popup", capOpts{allow: wide, afterLoad: func(t *task) {
		t.eval(fmt.Sprintf(`window.open('http://127.0.0.2:%d/secret?via=popup');1`, p), 3*time.Second)
	}, settle: 2 * time.Second})
	c3 := hasVia("popup")
	e.check("正向对照：放行内网时重定向与 iframe 探针能命中", c1 && c2, fmt.Sprintf("重定向=%v iframe=%v", c1, c2))
	e.note("window.open 在该环境下 %s", map[bool]string{true: "会创建弹窗（放行时命中内网）", false: "没有创建任何弹窗（放行时也未命中内网），弹窗路径不可达"}[c3])

	r := one("/ssrf-redirect", capOpts{allow: allow})
	e.check("302 重定向到 127.0.0.2：在跳转那一跳被拦", len(e.srv.secretHits()) == 0 && !r.ok, fmt.Sprintf("内网命中=%v navErr=%s", e.srv.secretHits(), r.navErr))

	r = one("/meta-ssrf", capOpts{allow: allow, settle: 1500 * time.Millisecond})
	e.check("meta refresh 跳到 127.0.0.2：被拦", len(e.srv.secretHits()) == 0, fmt.Sprintf("内网命中=%v blocked=%d ok=%v", e.srv.secretHits(), r.blocked, r.ok))

	popupProbe := func(gesture bool) func(*task) {
		return func(t *task) {
			t.evalG(fmt.Sprintf(`window.open('http://127.0.0.2:%d/secret?via=popup');window.open('http://127.0.0.1:%d/ok?n=popup');1`, p, p), 3*time.Second, gesture)
		}
	}
	// 没有用户手势：弹窗拦截生效，页面自己无法弹出窗口。
	r = one("/popup", capOpts{allow: allow, autoAttach: true, afterLoad: popupProbe(false), settle: 2 * time.Second})
	e.note("已知缺口：没有用户手势的 window.open 也会弹出窗口（WebView2 没有 Chromium 的弹窗拦截）：内网命中=%v 弹窗=%d", e.srv.secretHits(), r.popups)
	// 有用户手势（例如采集流程自己点击了链接）：已知缺口。
	r = one("/popup", capOpts{allow: allow, autoAttach: true, afterLoad: popupProbe(true), settle: 2 * time.Second})
	e.note("已知缺口：带用户手势的 window.open，即使浏览器级自动接管也拦不住弹窗的首个导航请求：内网命中=%v，接管的弹窗=%d，拦截=%d", e.srv.secretHits(), r.popups, r.blocked)
	r = one("/oopif", capOpts{allow: allow, autoAttach: true, settle: 2 * time.Second})
	e.check("跨站 iframe（接管并套用规则）：内网未被访问", !hasVia("iframe") && r.ok, fmt.Sprintf("内网命中=%v ok=%v 拦截=%d", e.srv.secretHits(), r.ok, r.blocked))
	r = one("/oopif", capOpts{allow: allow, autoAttach: false, settle: 2 * time.Second})
	leaked := hasVia("iframe")
	e.note("对照组（不接管子 target）：内网命中=%v —— %s", e.srv.secretHits(), map[bool]string{true: "说明只在主页面启用 Fetch 会漏掉跨站 iframe，必须递归 autoAttach", false: "跨站 iframe 没有漏过（本环境下父会话的 Fetch 已覆盖）"}[leaked])
	// 缓解 A：浏览器参数 --block-new-web-contents（对 WebView2 无效，见结果）；缓解 B：每个新文档里把 window.open 置空。
	e.b.stop()
	extraBrowserArgs = "--block-new-web-contents"
	if err := e.newBrowser(); err != nil {
		e.check("以 --block-new-web-contents 重启采集进程", false, err.Error())
		return
	}
	r = one("/popup", capOpts{allow: allow, autoAttach: true, afterLoad: popupProbe(true), settle: 2 * time.Second})
	e.note("缓解 A（--block-new-web-contents）：内网命中=%v 弹窗=%d —— %s", e.srv.secretHits(), r.popups, map[bool]string{true: "无效", false: "有效"}[hasVia("popup")])
	e.b.stop()
	extraBrowserArgs = ""
	if err := e.newBrowser(); err != nil {
		e.check("恢复默认参数重启采集进程", false, err.Error())
		return
	}
	r = one("/popup", capOpts{allow: allow, autoAttach: true, blockOpen: true, afterLoad: popupProbe(true), settle: 2 * time.Second})
	e.check("缓解 B：新文档里置空 window.open 后弹窗与内网访问都消失", !hasVia("popup") && r.popups == 0, fmt.Sprintf("内网命中=%v 弹窗=%d", e.srv.secretHits(), r.popups))
	r = one("/ok?n=1", capOpts{allow: allow})
	e.check("缓解不影响普通页面", r.ok, "")
}
func orphans(userData string) int {
	frag := filepath.Base(userData)
	ps := `(Get-CimInstance Win32_Process | Where-Object { $_.ProcessId -ne $PID -and $_.CommandLine -like '*` + frag + `*' } | Measure-Object).Count`
	out, err := exec.Command("powershell", "-NoProfile", "-Command", ps).Output()
	if err != nil {
		return -1
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return n
}

func killScenario(e *env, _ int) {
	var wg sync.WaitGroup
	res := make([]taskResult, 3)
	ends := make([]time.Time, 3)
	for i := range res {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := int(taskSeq.Add(1))
			res[i] = e.b.capture(fmt.Sprintf("%s/slow?ms=20000", e.srv.base()), id, capOpts{navTimeout: 30 * time.Second, allow: allowLoopbackHosts("127.0.0.1")})
			ends[i] = time.Now()
		}()
	}
	time.Sleep(1500 * time.Millisecond)
	old := e.b
	tKill := time.Now()
	old.kill()
	wg.Wait()
	var worst time.Duration
	allFail := true
	for i := range res {
		if d := ends[i].Sub(tKill); d > worst {
			worst = d
		}
		if res[i].ok {
			allFail = false
		}
	}
	e.check("进程被杀后 3 个在途任务都报错", allFail, fmt.Sprintf("错误=%q", res[0].err))
	e.check("最慢的任务在 3 秒内得知失败", worst < 3*time.Second, fmt.Sprintf("最慢 %s（其中清理 target 的等待也计入）", worst.Round(time.Millisecond)))
	select {
	case <-old.d.closed:
		e.check("CDP 连接关闭事件已触发", true, fmt.Sprintf("距被杀 %s", old.d.closeT.Sub(tKill).Round(time.Millisecond)))
	default:
		e.check("CDP 连接关闭事件已触发", false, "")
	}
	time.Sleep(3 * time.Second)
	n := orphans(old.userData)
	e.check("没有残留的 WebView2 子进程", n == 0, fmt.Sprintf("命令行含该数据目录的进程 %d 个", n))
	os.RemoveAll(old.userData)

	t0 := time.Now()
	if err := e.newBrowser(); err != nil {
		e.check("重启采集进程", false, err.Error())
		return
	}
	e.check("重启采集进程", true, fmt.Sprintf("耗时 %s", time.Since(t0).Round(time.Millisecond)))
	jobs := e.okJobs(20)
	rs, wall := e.runJobs(jobs, 3)
	ok := 0
	for _, r := range rs {
		if r.ok {
			ok++
		}
	}
	e.check("重启后继续跑 20 个任务", ok == 20, fmt.Sprintf("%d/20 墙钟=%s", ok, wall.Round(time.Millisecond)))
}

func memScenario(e *env, _ int) {
	mb0, n0 := treeStats(e.b.pid())
	e.note("起点：进程树 %d 个进程，工作集 %.0f MB", n0, mb0)
	all := e.corpusJobs(6)
	var last float64
	var lastN int
	for batch := 0; batch < 3; batch++ {
		jobs := all[batch*100 : batch*100+100]
		rs, wall := e.runJobs(jobs, 3)
		ok := 0
		for _, r := range rs {
			if r.ok {
				ok++
			}
		}
		e.settleTargets(10 * time.Second)
		time.Sleep(3 * time.Second)
		last, lastN = treeStats(e.b.pid())
		e.note("第 %d 批 100 个任务：成功 %d，墙钟 %s；之后进程树 %d 个进程，工作集 %.0f MB（相对起点 %+.0f MB）", batch+1, ok, wall.Round(time.Second), lastN, last, last-mb0)
	}
	e.check("进程数不随任务数增长", lastN <= n0+4, fmt.Sprintf("起点 %d → 300 个任务后 %d", n0, lastN))
	e.note("增长 %.0f MB / 300 个任务；是否需要定期重启采集进程，以这组数字和真实站点的复测为准", last-mb0)
}
