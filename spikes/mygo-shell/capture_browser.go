package main

// 采集浏览器：网页渲染（renderHost）与平台登录采集共用的隐藏浏览器进程。来自 mygo-capstress 压测原型。
//
// 进程模型：归知主进程按需拉起「自己的另一个实例」（同一个 exe，环境变量 GUIZHI_CAPTURE_CHILD 指向独立的
// WebView2 用户数据目录），子进程只托管隐藏的 MyGo 窗口并打开 WebView2 的远程调试端口；主进程用 CDP 控制，
// 页面渲染进程崩溃或卡死不会拖垮主进程。窗口出问题（卡死、渲染进程崩溃、使用次数过多）整个关闭并重建。
//
// 为什么用 MyGo 窗口池而不是 CDP 的 Target.createTarget：MyGo 窗口的 window.open 由宿主层的处理器拒绝
//（不设处理器时 MyGo 会把 http(s) 地址交给系统默认浏览器，必须显式拒绝）；createTarget 新建的页面不经过该处理器。

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
)

// captureChildEnv 非空即以采集子进程模式运行，值为 WebView2 用户数据目录。
const captureChildEnv = "GUIZHI_CAPTURE_CHILD"

// captureHostRules：兜底——浏览器自己不解析任何域名。所有 http(s) 请求都由 Go 侧的安全出口应答（见 renderHost），
// 任何漏过拦截的请求（含 WebSocket）到这里都会解析失败，而不是直连网络或内网。
const captureHostRules = `--host-resolver-rules="MAP * ~NOTFOUND"`

func findDevToolsPortFile(dir string, wait time.Duration) string {
	end := time.Now().Add(wait)
	for time.Now().Before(end) {
		var found string
		filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() && d.Name() == "DevToolsActivePort" {
				found = p
			}
			return nil
		})
		if found != "" {
			return found
		}
		time.Sleep(100 * time.Millisecond)
	}
	return ""
}

// runCaptureChildIfRequested 在 main / TestMain 最开头调用：是采集子进程则运行到结束并返回 true。
func runCaptureChildIfRequested() bool {
	userData := os.Getenv(captureChildEnv)
	if userData == "" {
		return false
	}
	runCaptureChild(userData)
	return true
}

func newHiddenCaptureWindow(title string) {
	w := mygo.NewWindow(mygo.WindowOptions{
		Title: title, Width: 1024, Height: 768, URL: "about:blank", Hidden: true,
		Page: mygo.PageOptions{BackgroundThrottling: mygo.ThrottlingOff},
	})
	// 必须显式拒绝：不设处理器时，MyGo 会把 window.open 的 http(s) 地址交给系统默认浏览器。
	w.Page().SetWindowOpenHandler(func(mygo.WindowOpenRequest) *mygo.WindowOptions { return nil })
}

// 平台登录采集的窗口模式（环境变量 GUIZHI_CAPTURE_WINDOW）：visible = 正常可见的登录窗口；
// offscreen = 放到所有显示器之外（自动化验证用，不打扰正在使用的桌面）；空 = 隐藏（渲染池与后台采集）。
const (
	captureWindowEnv   = "GUIZHI_CAPTURE_WINDOW"
	captureTitleEnv    = "GUIZHI_CAPTURE_TITLE"
	captureWindowAfter = 1040
)

// newCaptureChildWindow 创建子进程的首个窗口：隐藏模式与历史行为完全一致，平台采集按环境变量显示。
func newCaptureChildWindow() {
	mode := os.Getenv(captureWindowEnv)
	if mode != "visible" && mode != "offscreen" {
		newHiddenCaptureWindow("capture-child")
		return
	}
	title := os.Getenv(captureTitleEnv)
	if title == "" {
		title = "归知平台登录"
	}
	opt := mygo.WindowOptions{
		Title: title, Width: captureWindowAfter, Height: 760, MinWidth: 760, MinHeight: 560, URL: "about:blank",
		Page: mygo.PageOptions{BackgroundThrottling: mygo.ThrottlingOff},
	}
	if mode == "offscreen" {
		opt.X, opt.Y = -32000, -32000
	}
	w := mygo.NewWindow(opt)
	w.Page().SetWindowOpenHandler(func(mygo.WindowOpenRequest) *mygo.WindowOptions { return nil })
}

func runCaptureChild(userData string) {
	// 持久会话目录会复用：先清掉上次运行遗留的端口文件，否则会读到已失效的端口。
	// A reused profile dir keeps the previous run's port file; remove it before launching.
	removeStaleDevToolsPort(userData)
	mygo.App.WhenReady(func() {
		newCaptureChildWindow()
		go func() {
			f := findDevToolsPortFile(userData, 15*time.Second)
			raw, _ := os.ReadFile(f)
			lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
			if len(lines) < 2 {
				fmt.Println("ERR no-port")
				mygo.App.Quit()
				return
			}
			fmt.Printf("READY %s %s\n", strings.TrimSpace(lines[0]), strings.TrimSpace(lines[1]))
			// 控制通道：主进程发 NEW 就新建一个隐藏窗口（页池里的一个），主进程退出或关闭管道即结束。
			sc := bufio.NewScanner(os.Stdin)
			for sc.Scan() {
				if sc.Text() == "NEW" {
					newHiddenCaptureWindow("capture-page")
					fmt.Println("WIN")
				}
			}
			mygo.App.Quit()
		}()
	})
	if err := mygo.App.Run(); err != nil {
		fmt.Println("ERR", err)
	}
}

// ---------- 主进程侧 ----------

type captureBrowser struct {
	cmd      *exec.Cmd
	d        *cdpConn
	userData string
	stdin    io.WriteCloser
	winCh    chan struct{}
	winMu    sync.Mutex
	pool     *capturePool
	// tasks：target → 拥有它的渲染任务，弹窗/子目标据此归属
	tasks sync.Map
}

type captureTarget struct {
	TargetID string `json:"targetId"`
	Type     string `json:"type"`
	URL      string `json:"url"`
}

// startCaptureBrowser 拉起采集子进程并连上 CDP。exe 为空取当前可执行文件（测试里是测试二进制，由 TestMain 接管）。
func startCaptureBrowser(ctx context.Context, exe string, poolSize int) (*captureBrowser, error) {
	if exe == "" {
		var err error
		if exe, err = os.Executable(); err != nil {
			return nil, err
		}
	}
	ud, err := os.MkdirTemp("", "guizhi-capture-")
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(),
		captureChildEnv+"="+ud,
		"WEBVIEW2_USER_DATA_FOLDER="+ud,
		"WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS=--remote-debugging-port=0 --force-webrtc-ip-handling-policy=disable_non_proxied_udp "+captureHostRules)
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		os.RemoveAll(ud)
		return nil, err
	}
	lineCh := make(chan string, 1)
	winCh := make(chan struct{}, 256)
	go func() {
		sc := bufio.NewScanner(stdout)
		first := true
		for sc.Scan() {
			if first {
				first = false
				lineCh <- strings.TrimSpace(sc.Text())
				continue
			}
			if strings.TrimSpace(sc.Text()) == "WIN" {
				winCh <- struct{}{}
			}
		}
		if first {
			lineCh <- "ERR 采集进程提前退出"
		}
	}()
	fail := func(err error) (*captureBrowser, error) {
		cmd.Process.Kill()
		cmd.Wait()
		os.RemoveAll(ud)
		return nil, err
	}
	var line string
	select {
	case line = <-lineCh:
	case <-ctx.Done():
		return fail(ctx.Err())
	case <-time.After(40 * time.Second):
		return fail(fmt.Errorf("采集进程启动超时"))
	}
	f := strings.Fields(line)
	if len(f) != 3 || f[0] != "READY" {
		return fail(fmt.Errorf("采集进程启动失败：%s", line))
	}
	d, err := dialCDP(ctx, "ws://127.0.0.1:"+f[1]+f[2])
	if err != nil {
		return fail(err)
	}
	b := &captureBrowser{cmd: cmd, d: d, userData: ud, stdin: stdin, winCh: winCh}
	d.global = b.onBrowserEvent
	// 浏览器级自动接管：新 target（包括弹窗、跨站 iframe）先暂停，由任务套上拦截规则再放行。
	if _, err := d.call("", "Target.setAutoAttach", map[string]any{"autoAttach": true, "waitForDebuggerOnStart": true, "flatten": true}); err != nil {
		b.kill()
		return nil, err
	}
	// 网页不得触发下载。
	if _, err := d.call("", "Browser.setDownloadBehavior", map[string]any{"behavior": "deny"}); err != nil {
		b.kill()
		return nil, err
	}
	if err := b.makePool(poolSize); err != nil {
		b.kill()
		return nil, err
	}
	return b, nil
}

func (b *captureBrowser) alive() bool {
	select {
	case <-b.d.closed:
		return false
	default:
		return true
	}
}

func (b *captureBrowser) stop() {
	b.stdin.Close()
	done := make(chan struct{})
	go func() { b.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		b.cmd.Process.Kill()
		<-done
	}
	removeUserDataDir(b.userData)
}

func (b *captureBrowser) kill() {
	b.cmd.Process.Kill()
	b.cmd.Wait()
	removeUserDataDir(b.userData)
}

// removeUserDataDir 删除采集浏览器的用户数据目录。Windows 上 WebView2 的子进程在宿主退出后还会持有目录一小会儿，
// 单次 RemoveAll 会静默失败并把目录留在 %TEMP%，所以按短间隔重试到上限；仍失败则留痕而不是吞掉。
// 与进程生死无关的清理失败（权限等）同样会走到留痕分支。
func removeUserDataDir(dir string) {
	deadline := time.Now().Add(userDataRemoveTimeout)
	for {
		err := os.RemoveAll(dir)
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			logAppErrorf("main", "采集浏览器", "用户数据目录未能删除：%s：%v", dir, err)
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

const userDataRemoveTimeout = 5 * time.Second

func (b *captureBrowser) targets() ([]captureTarget, error) {
	res, err := b.d.call("", "Target.getTargets", nil)
	if err != nil {
		return nil, err
	}
	var r struct {
		TargetInfos []captureTarget `json:"targetInfos"`
	}
	json.Unmarshal(res, &r)
	return r.TargetInfos, nil
}

func (b *captureBrowser) onBrowserEvent(m cdpMsg) {
	if m.Method != "Target.attachedToTarget" {
		return
	}
	var e struct {
		SessionID          string `json:"sessionId"`
		WaitingForDebugger bool   `json:"waitingForDebugger"`
		TargetInfo         struct {
			OpenerID string `json:"openerId"`
		} `json:"targetInfo"`
	}
	json.Unmarshal(m.Params, &e)
	if e.TargetInfo.OpenerID != "" {
		if v, ok := b.tasks.Load(e.TargetInfo.OpenerID); ok {
			if t, ok := v.(interface{ adopt(cdpMsg) }); ok {
				t.adopt(m)
				return
			}
		}
	}
	// 不属于任何任务的 target（采集进程自带窗口等）：直接放行。
	if e.WaitingForDebugger {
		go b.d.call(e.SessionID, "Runtime.runIfWaitingForDebugger", nil)
	}
}

// ---------- 页池 ----------

type capturePool struct {
	b        *captureBrowser
	ch       chan string
	mu       sync.Mutex
	uses     map[string]int // 仍在池中的窗口已服务次数；窗口被替换时删除，避免无界增长
	maxUse   int            // 每个窗口最多服务多少个任务，0 表示不限
	replaced int            // 累计被替换（不健康或用满）的窗口数，用于诊断窗口复用是否失效
}

// newWindow 让采集进程新建一个隐藏窗口，并返回它的 target。
func (b *captureBrowser) newWindow() (string, error) {
	b.winMu.Lock() // 并发新建会让「比较前后 target 差异」认错窗口，必须串行
	defer b.winMu.Unlock()
	before, err := b.targets()
	if err != nil {
		return "", err
	}
	known := map[string]bool{}
	for _, t := range before {
		known[t.TargetID] = true
	}
	if _, err := fmt.Fprintln(b.stdin, "NEW"); err != nil {
		return "", err
	}
	select {
	case <-b.winCh:
	case <-time.After(15 * time.Second):
		return "", fmt.Errorf("新建窗口超时")
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		ts, err := b.targets()
		if err != nil {
			return "", err
		}
		for _, t := range ts {
			if t.Type == "page" && !known[t.TargetID] {
				return t.TargetID, nil
			}
		}
		time.Sleep(30 * time.Millisecond)
	}
	return "", fmt.Errorf("没有出现新窗口的 target")
}

func (b *captureBrowser) makePool(n int) error {
	p := &capturePool{b: b, ch: make(chan string, n+8), uses: map[string]int{}, maxUse: 50}
	for i := 0; i < n; i++ {
		tid, err := b.newWindow()
		if err != nil {
			return err
		}
		p.ch <- tid
	}
	b.pool = p
	return nil
}

func (p *capturePool) acquire(ctx context.Context) (string, error) {
	select {
	case tid := <-p.ch:
		return tid, nil
	case <-ctx.Done():
		return "", ctx.Err()
	case <-p.b.d.closed:
		return "", errClosed
	}
}

// release 归还窗口；unhealthy 或用满次数时关闭它并换一个新的。
func (p *capturePool) release(tid string, unhealthy bool) {
	p.mu.Lock()
	p.uses[tid]++
	worn := p.maxUse > 0 && p.uses[tid] >= p.maxUse
	if !unhealthy && !worn {
		p.mu.Unlock()
		p.ch <- tid
		return
	}
	delete(p.uses, tid)
	p.replaced++
	p.mu.Unlock()
	p.b.d.callT("", "Target.closeTarget", map[string]any{"targetId": tid}, 5*time.Second)
	nt, err := p.b.newWindow()
	if err != nil {
		logAppErrorf("main", "采集浏览器", "重建窗口失败：%v", err)
		return
	}
	p.ch <- nt
}

func removeStaleDevToolsPort(dir string) {
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && d.Name() == "DevToolsActivePort" {
			_ = os.Remove(p)
		}
		return nil
	})
}
