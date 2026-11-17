// islandprobe 在真 WebView2 里核实计划 §4.7 的三项：岛的安全姿态、隐藏页（采集）的能力、长文阅读。
// 不进产品；结果写成 JSON，结论记在 docs/agents/viva-ui-development.md §8.3。
//
// 用法（在 apps/desktop-go 下；务必用一次性的 LOCALAPPDATA，别碰真实的 %LOCALAPPDATA%\GuiZhi）：
//
//	go run ./viva/cmd/islandprobe -probe security,ephemeral,msgsize,reading,throttle -out result.json
//	go run ./viva/cmd/islandprobe -probe markdown -out md.json   // 原生 Markdown 的无头基准，不开窗口
//
// 窗口探针会在屏幕上开一个 960x640 的窗口，跑完自己退出；全程不碰用户的归知与数据目录。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	vapp "github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"

	"guizhi-mygo-shell/viva/island"
)

// probeClass 是 WebView2 profile 的专用目录名（绝不能叫 GuiZhi）。
const probeClass = "GuiZhiIslandProbe"

// securityModes 是安全探针要跑的分组：bare（加固前基线：没有任何响应头）、inline（经 Config.CSP 放行内联脚本）、
// prod（岛的默认响应头，脚本外链）。
var securityModes = []string{"bare", "inline", "prod"}

func main() {
	probes := flag.String("probe", "security", "要跑的探针，逗号分隔：security、ephemeral、msgsize、reading、throttle、markdown")
	outPath := flag.String("out", "", "结果 JSON 路径（空则只打印）")
	throttleSec := flag.Int("throttle-sec", 30, "throttle 每个场景的计时秒数")
	modes := flag.String("modes", strings.Join(securityModes, ","), "security 探针要跑的分组，逗号分隔：bare、inline、prod")
	flag.Parse()
	// 与产品一样登记窗口 Class：开岛前会清理以前留下的临时 profile 目录（R5）。
	island.SetProfileClass(probeClass)
	securityModes = nil
	for _, mode := range strings.Split(*modes, ",") {
		if mode = strings.TrimSpace(mode); mode != "" {
			securityModes = append(securityModes, mode)
		}
	}

	want := map[string]bool{}
	for _, name := range strings.Split(*probes, ",") {
		if name = strings.TrimSpace(name); name != "" {
			want[name] = true
		}
	}
	results := map[string]any{"startedAt": time.Now().Format(time.RFC3339)}

	// 无头基准不需要窗口。
	if want["markdown"] {
		results["markdown"] = runMarkdownBench()
		delete(want, "markdown")
	}
	if len(want) > 0 {
		for name, value := range runWindowProbes(want, *throttleSec) {
			results[name] = value
		}
	}
	results["finishedAt"] = time.Now().Format(time.RFC3339)

	text, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "编码结果失败：", err)
		os.Exit(1)
	}
	fmt.Println(string(text))
	if *outPath != "" {
		if err := os.WriteFile(*outPath, append(text, '\n'), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "写结果失败：", err)
			os.Exit(1)
		}
	}
}

// env 是窗口探针的运行环境：一个主窗口、它的 WebHost，以及把任务排到窗口线程上的队列。
type env struct {
	app *vapp.Application
	win *vapp.Window
	web *vapp.WebHost
	inv *vapp.Invalidator

	mu   sync.Mutex
	jobs []func()

	// placed 是主窗口里摆出来的岛（只在窗口线程读写）；没摆的页面对 Viva 来说是隐藏的。
	placed []gfx.NativeViewID

	hits *hitCounter
	loop string // 本机回环 HTTP 服务的地址，岛里的页面把它当作「外部」
}

// post 把 fn 排到窗口线程上执行（下一帧开头，OnWake）。任意 goroutine。
func (e *env) post(fn func()) {
	e.mu.Lock()
	e.jobs = append(e.jobs, fn)
	e.mu.Unlock()
	e.inv.Invalidate()
}

// drain 在窗口线程上执行排队的任务。
func (e *env) drain() {
	e.mu.Lock()
	jobs := e.jobs
	e.jobs = nil
	e.mu.Unlock()
	for _, job := range jobs {
		job()
	}
}

// onUI 在窗口线程上执行 fn 并等它跑完。
func (e *env) onUI(fn func()) {
	done := make(chan struct{})
	e.post(func() {
		defer close(done)
		fn()
	})
	<-done
}

// open 在窗口线程上开一个岛；place 为真时把它摆满主窗口，extraArgs 非空时只在 Open 期间追加进浏览器参数
// （与岛包里 W1 追加 --disable-component-update 是同一个机制）。
func (e *env) open(cfg island.Config, place bool, extraArgs string) (*island.Island, error) {
	var isl *island.Island
	var err error
	e.onUI(func() {
		restore := setBrowserArgs(extraArgs)
		isl, err = island.Open(e.web, cfg)
		restore()
		if err == nil && place {
			e.placed = append(e.placed, isl.View().ID())
		}
	})
	return isl, err
}

// openBare 复刻加固之前的岛：只有 Navigate 策略（island 同一规则的简化版）、没有任何响应头。
// 只用来取「加固前」的基线（与加固后的 prod 同一份探针脚本逐项对照），不进产品。
func (e *env) openBare(cfg island.Config) (*vapp.WebView, error) {
	var view *vapp.WebView
	var err error
	e.onUI(func() {
		restore := setBrowserArgs("--disable-component-update")
		view, err = e.web.Open(vapp.WebViewConfig{
			URL:                 "gzisland://doc/index.html",
			Scheme:              &vapp.WebScheme{Name: "gzisland", Serve: cfg.Serve},
			Proxy:               vapp.WebProxyDirect,
			Ephemeral:           true,
			BridgeMainFrameOnly: true,
			OnMessage:           cfg.OnMessage,
			Navigate: func(n vapp.WebNavigation) bool {
				if strings.HasPrefix(n.URL, "gzisland://doc/") || n.URL == "about:blank" || n.URL == "about:srcdoc" {
					return true
				}
				if cfg.OnDenied != nil {
					cfg.OnDenied(n.URL, n.MainFrame)
				}
				return false
			},
		})
		restore()
		if err == nil {
			e.placed = append(e.placed, view.ID())
		}
	})
	return view, err
}

// closeBare 摘掉并销毁 openBare 开的页面。
func (e *env) closeBare(view *vapp.WebView) {
	if view == nil {
		return
	}
	e.onUI(func() {
		id := view.ID()
		kept := e.placed[:0]
		for _, p := range e.placed {
			if p != id {
				kept = append(kept, p)
			}
		}
		e.placed = kept
		view.Close()
	})
}

// closeIsland 把岛从摆放里摘掉并销毁。
func (e *env) closeIsland(isl *island.Island) {
	if isl == nil {
		return
	}
	e.onUI(func() {
		id := isl.View().ID()
		kept := e.placed[:0]
		for _, p := range e.placed {
			if p != id {
				kept = append(kept, p)
			}
		}
		e.placed = kept
		isl.Close()
	})
}

// browserArgsVar 与 island 包同名常量一致（那里未导出）。
const browserArgsVar = "WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS"

// setBrowserArgs 在进程环境里设置额外浏览器参数，返回还原函数。args 为空时什么也不做。
func setBrowserArgs(args string) func() {
	if args == "" {
		return func() {}
	}
	prev, had := os.LookupEnv(browserArgsVar)
	value := args
	if strings.TrimSpace(prev) != "" {
		value = prev + " " + args
	}
	_ = os.Setenv(browserArgsVar, value)
	return func() {
		if had {
			_ = os.Setenv(browserArgsVar, prev)
		} else {
			_ = os.Unsetenv(browserArgsVar)
		}
	}
}

// runWindowProbes 开主窗口，在它的 OnReady 之后从另一条 goroutine 依次跑探针，跑完退出应用。
func runWindowProbes(want map[string]bool, throttleSec int) map[string]any {
	results := map[string]any{}
	loop, hits, stopLoop := startLoopback()
	defer stopLoop()

	e := &env{web: &vapp.WebHost{}, inv: &vapp.Invalidator{}, hits: hits, loop: loop}
	ready := make(chan struct{})
	var readyOnce sync.Once
	e.web.OnReady = func() { readyOnce.Do(func() { close(ready) }) }

	runDone := make(chan struct{})
	go func() {
		defer close(runDone)
		<-ready
		runProbes(e, want, throttleSec, results)
		e.onUI(func() { e.app.Quit() })
	}()

	origin := gfx.Point{X: 160, Y: 90}
	config := vapp.Config{
		Title: "island-probe", Class: probeClass, Width: 960, Height: 640, Origin: &origin,
		Web: e.web, Invalidator: e.inv, OnWake: e.drain,
		Paint: func(canvas *gfx.Canvas, size gfx.Size) {
			canvas.Fill(gfx.Rect{Width: size.Width, Height: size.Height}, gfx.Color{Red: 0.1, Green: 0.1, Blue: 0.12, Alpha: 1})
			for _, id := range e.placed {
				canvas.NativeView(id, gfx.Rect{Width: size.Width, Height: size.Height})
			}
		},
	}
	err := vapp.RunApplication(func(a *vapp.Application) error {
		var canvas gfx.Canvas
		win, err := a.OpenWindow(config, &canvas)
		if err != nil {
			return err
		}
		e.app, e.win = a, win
		return nil
	})
	if err != nil {
		results["runError"] = err.Error()
	}
	<-runDone
	return results
}

// runProbes 依次跑被选中的探针，单个探针崩溃（panic）不拖垮其余。
func runProbes(e *env, want map[string]bool, throttleSec int, results map[string]any) {
	run := func(name string, fn func() any) {
		if !want[name] {
			return
		}
		fmt.Fprintf(os.Stderr, "== 探针 %s 开始\n", name)
		start := time.Now()
		func() {
			defer func() {
				if r := recover(); r != nil {
					results[name] = map[string]any{"panic": fmt.Sprint(r)}
				}
			}()
			results[name] = fn()
		}()
		fmt.Fprintf(os.Stderr, "== 探针 %s 结束，用时 %.1fs\n", name, time.Since(start).Seconds())
	}
	run("security", func() any {
		out := map[string]any{}
		for _, mode := range securityModes {
			fmt.Fprintf(os.Stderr, "-- security/%s 开始 %s\n", mode, time.Now().Format("15:04:05.000"))
			out[mode] = securityProbe(e, mode)
			fmt.Fprintf(os.Stderr, "-- security/%s 结束 %s\n", mode, time.Now().Format("15:04:05.000"))
		}
		return out
	})
	run("ephemeral", func() any { return ephemeralProbe(e) })
	run("msgsize", func() any { return msgSizeProbe(e) })
	run("reading", func() any { return readingProbe(e) })
	run("throttle", func() any { return throttleProbe(e, throttleSec) })
}
