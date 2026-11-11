// 独立采集进程的真实页面与并发压测。
//
// 回放本机 59 页真实网页（静态 HTML）到本地模拟站点，在独立采集进程里并发抓取，并注入各类故障与安全探针。
// 不访问外网：采集进程带 --host-resolver-rules 兜底，所有非本地请求同时在 Fetch 阶段被拦截。
//
// 用法：mygo-capstress.exe -corpus <语料目录> [-run 名称,名称] [-rounds N]
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type env struct {
	srv   *server
	b     *browser
	base  int // 基线页面数（采集进程自带的空白窗口）
	fails int
}

func (e *env) check(name string, ok bool, detail string) {
	tag := "[ OK ]"
	if !ok {
		tag = "[FAIL]"
		e.fails++
	}
	fmt.Printf("%s %-46s %s\n", tag, name, detail)
}

func (e *env) note(format string, a ...any) { fmt.Printf("       %s\n", fmt.Sprintf(format, a...)) }

func (e *env) newBrowser() error {
	b, err := startBrowser()
	if err != nil {
		return err
	}
	if _, err := b.d.call("", "Browser.setDownloadBehavior", map[string]any{"behavior": "deny"}); err != nil {
		return err
	}
	e.b = b
	e.base = b.pageCount()
	return nil
}

type job struct {
	url  func(id int) string
	o    capOpts
	want string
}

func (e *env) runJobs(jobs []job, conc int) ([]taskResult, time.Duration) {
	res := make([]taskResult, len(jobs))
	ch := make(chan int)
	var wg sync.WaitGroup
	t0 := time.Now()
	for w := 0; w < conc; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range ch {
				id := int(taskSeq.Add(1))
				res[i] = e.b.capture(jobs[i].url(id), id, jobs[i].o)
			}
		}()
	}
	for i := range jobs {
		ch <- i
	}
	close(ch)
	wg.Wait()
	return res, time.Since(t0)
}

func (e *env) corpusJobs(rounds int) []job {
	var js []job
	for r := 0; r < rounds; r++ {
		for _, p := range e.srv.pages {
			p := p
			js = append(js, job{
				url:  func(id int) string { return fmt.Sprintf("%s/p/%s?task=%d", e.srv.base(), p.id, id) },
				o:    capOpts{allow: allowLoopbackHosts("127.0.0.1", "localhost")},
				want: p.title,
			})
		}
	}
	return js
}

func pct(ds []time.Duration, p float64) time.Duration {
	if len(ds) == 0 {
		return 0
	}
	s := append([]time.Duration(nil), ds...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[int(float64(len(s)-1)*p)]
}

// settleTargets 等待页面数回到基线（关闭是异步的），返回等待时间。
func (e *env) settleTargets(max time.Duration) (time.Duration, int) {
	t0 := time.Now()
	for {
		n := e.b.pageCount()
		if n == e.base || time.Since(t0) > max {
			return time.Since(t0), n
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (e *env) summarize(name string, jobs []job, res []taskResult, wall time.Duration, conc int) {
	var ok, titleOK, htmlOK, blocked int
	var ds, cs []time.Duration
	var errs = map[string]int{}
	var mism []string
	for i, r := range res {
		ds = append(ds, r.dur)
		cs = append(cs, r.closeDur)
		blocked += r.blocked
		if r.ok {
			ok++
			if r.title == jobs[i].want {
				titleOK++
			} else if len(mism) < 5 {
				mism = append(mism, fmt.Sprintf("want=%q got=%q", jobs[i].want, r.title))
			}
			if r.htmlLen > 1000 {
				htmlOK++
			}
		} else {
			errs[r.phase+": "+r.err]++
		}
	}
	e.check(name+" 全部成功", ok == len(res), fmt.Sprintf("%d/%d 并发=%d 墙钟=%s 吞吐=%.1f 页/秒", ok, len(res), conc, wall.Round(time.Millisecond), float64(len(res))/wall.Seconds()))
	e.note("单页耗时 p50=%s p95=%s max=%s；关闭耗时 p95=%s；被拦截的外部请求共 %d 个",
		pct(ds, .5).Round(time.Millisecond), pct(ds, .95).Round(time.Millisecond), pct(ds, 1).Round(time.Millisecond), pct(cs, .95).Round(time.Millisecond), blocked)
	e.check(name+" 标题与源文件一致", titleOK == ok, fmt.Sprintf("%d/%d；DOM 长度>1000：%d", titleOK, ok, htmlOK))
	for _, m := range mism {
		e.note("标题不一致 %s", m)
	}
	for k, n := range errs {
		e.note("失败 ×%d %s", n, k)
	}
	w, n := e.settleTargets(10 * time.Second)
	e.check(name+" target 无泄漏", n == e.base, fmt.Sprintf("页面数=%d 基线=%d 等待=%s", n, e.base, w.Round(time.Millisecond)))
}

func main() {
	if len(os.Args) > 2 && os.Args[1] == "-child" {
		runChild(os.Args[2])
		return
	}
	corpus := flag.String("corpus", filepath.Join(os.Getenv("TEMP"), "guizhi-crawl-eval", "cache"), "语料目录")
	run := flag.String("run", "all", "要运行的场景，逗号分隔")
	offscreen := flag.Bool("offscreen", true, "在独立的离屏桌面里运行，窗口不会出现在用户屏幕上")
	inner := flag.Bool("inner", false, "（内部）已在离屏桌面中")
	logPath := flag.String("log", "", "（内部）把输出写到该文件")
	rounds := flag.Int("rounds", 3, "并发场景中语料的轮数")
	flag.Parse()
	if *offscreen && !*inner {
		lp := filepath.Join(os.TempDir(), fmt.Sprintf("capstress-%d.log", time.Now().Unix()))
		code := runOffscreen([]string{"-corpus", *corpus, "-run", *run, "-rounds", fmt.Sprint(*rounds)}, lp, 30*time.Minute)
		if b, err := os.ReadFile(lp); err == nil {
			os.Stdout.Write(b)
		}
		os.Exit(code)
	}
	if *logPath != "" {
		f, err := os.Create(*logPath)
		if err == nil {
			os.Stdout = f
			os.Stderr = f
		}
	}

	srv, err := newServer(*corpus)
	if err != nil {
		fmt.Println("[FAIL] 模拟站点", err)
		os.Exit(2)
	}
	defer srv.close()
	fmt.Printf("语料 %d 页，模拟站点 %s（另监听 127.0.0.2:%d 作为“内网”）\n", len(srv.pages), srv.base(), srv.port)
	e := &env{srv: srv}
	t0 := time.Now()
	if err := e.newBrowser(); err != nil {
		fmt.Println("[FAIL] 启动采集进程", err)
		os.Exit(2)
	}
	e.check("Target.createTarget 在 WebView2 里可用", e.base >= 1, fmt.Sprintf("启动 %s，基线页面 %d", time.Since(t0).Round(time.Millisecond), e.base))
	defer func() {
		if e.b != nil {
			e.b.stop()
		}
	}()
	time.AfterFunc(25*time.Minute, func() { fmt.Println("[FAIL] 总超时 25 分钟"); os.Exit(3) })

	want := map[string]bool{}
	for _, n := range strings.Split(*run, ",") {
		want[strings.TrimSpace(n)] = true
	}
	for _, s := range scenarios {
		if want["all"] || want[s.name] {
			fmt.Printf("\n== %s ==\n", s.title)
			s.fn(e, *rounds)
		}
	}
	fmt.Printf("\n失败 %d 项。总耗时 %s\n", e.fails, time.Since(t0).Round(time.Second))
	if e.fails > 0 {
		os.Exit(1)
	}
}

type scenario struct {
	name, title string
	fn          func(*env, int)
}

var scenarios = []scenario{
	{"base", "1. 基线：59 页串行", func(e *env, _ int) {
		jobs := e.corpusJobs(1)
		res, wall := e.runJobs(jobs, 1)
		e.summarize("串行", jobs, res, wall, 1)
	}},
	{"conc", "2. 并发 3 与 8，并检查请求头串扰", func(e *env, rounds int) {
		for _, conc := range []int{3, 8} {
			ok0, mm0, ms0 := e.srv.crosstalk()
			jobs := e.corpusJobs(rounds)
			res, wall := e.runJobs(jobs, conc)
			e.summarize(fmt.Sprintf("并发%d", conc), jobs, res, wall, conc)
			ok1, mm1, ms1 := e.srv.crosstalk()
			e.check(fmt.Sprintf("并发%d 每个文档请求都带自己的任务令牌", conc), ok1-ok0 >= len(jobs) && mm1 == mm0 && ms1 == ms0,
				fmt.Sprintf("令牌正确 %d，串扰 %d，缺失 %d（任务 %d）", ok1-ok0, mm1-mm0, ms1-ms0, len(jobs)))
		}
	}},
	{"rules", "3. 并发任务各自的拦截规则互不影响", func(e *env, rounds int) {
		var jobs []job
		for i := 0; i < 12*rounds; i++ {
			i := i
			o := capOpts{allow: allowLoopbackHosts("127.0.0.1")}
			if i%2 == 1 {
				o.allow = allowLoopbackHosts() // 全部拦截
			}
			jobs = append(jobs, job{url: func(id int) string { return fmt.Sprintf("%s/ok?n=%d", e.srv.base(), id) }, o: o})
		}
		res, _ := e.runJobs(jobs, 6)
		good, bad := 0, 0
		for i, r := range res {
			if i%2 == 0 && r.ok {
				good++
			}
			if i%2 == 1 && !r.ok && strings.Contains(r.navErr, "BLOCKED_BY_CLIENT") {
				bad++
			}
		}
		e.check("放行任务成功、全拦截任务被拒", good == len(jobs)/2 && bad == len(jobs)/2, fmt.Sprintf("放行成功 %d/%d，拦截生效 %d/%d", good, len(jobs)/2, bad, len(jobs)/2))
		_, n := e.settleTargets(10 * time.Second)
		e.check("target 无泄漏", n == e.base, fmt.Sprintf("页面数=%d", n))
	}},
	{"faults", "4. 故障注入（每项与 6 个正常任务并发）", faults},
	{"security", "5. 安全探针：重定向、meta 刷新、弹窗、跨站 iframe", security},
	{"kill", "6. 采集进程被杀：检测速度与恢复", killScenario},
	{"mem", "7. 内存：300 个任务前后", memScenario},
}
