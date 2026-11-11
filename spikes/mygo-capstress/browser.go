package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
)

// ---------- 采集进程（子进程）：托管一个隐藏的 WebView2，把调试端口告诉父进程 ----------

func findPortFile(dir string, wait time.Duration) string {
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

func runChild(userData string) {
	mygo.App.WhenReady(func() {
		w := mygo.NewWindow(mygo.WindowOptions{
			Title: "capture-child", Width: 900, Height: 600, URL: "about:blank", Hidden: true,
			Page: mygo.PageOptions{BackgroundThrottling: mygo.ThrottlingOff},
		})
		if os.Getenv("CAPDENYOPEN") != "" {
			// 宿主层拒绝一切 window.open / target=_blank；不设处理器时 MyGo 会把 http(s) 交给系统默认浏览器。
			w.Page().SetWindowOpenHandler(func(mygo.WindowOpenRequest) *mygo.WindowOptions { return nil })
		}
		go func() {
			f := findPortFile(userData, 15*time.Second)
			raw, _ := os.ReadFile(f)
			lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
			if len(lines) < 2 {
				fmt.Println("ERR no-port")
				mygo.App.Quit()
				return
			}
			fmt.Printf("READY %s %s\n", strings.TrimSpace(lines[0]), strings.TrimSpace(lines[1]))
			io.Copy(io.Discard, os.Stdin) // 父进程退出或关闭管道即结束
			mygo.App.Quit()
		}()
	})
	if err := mygo.App.Run(); err != nil {
		fmt.Println("ERR", err)
	}
}

// ---------- 父进程侧 ----------

type browser struct {
	tasks    sync.Map // targetID → *task，用来把弹窗归到打开它的任务
	cmd      *exec.Cmd
	d        *cdp
	userData string
	stdin    io.WriteCloser
	started  time.Time
}

// 兜底：除本地回环外的任何主机（含 IP 字面量）都解析失败。127.0.0.2 是测试里的“内网”，必须排除在外，\n// 这样它是否被访问只取决于 Fetch 拦截规则——否则探针会被这条兜底规则误导成“没漏”。
const hostRules = `--host-resolver-rules="MAP * ~NOTFOUND, EXCLUDE 127.0.0.1, EXCLUDE 127.0.0.2, EXCLUDE localhost"`

var extraBrowserArgs string // 额外的浏览器参数（场景里用来对比缓解措施）

func startBrowser() (*browser, error) {
	ud, _ := os.MkdirTemp("", "guizhi-capstress-child-")
	cmd := exec.Command(os.Args[0], "-child", ud)
	cmd.Env = append(os.Environ(),
		"WEBVIEW2_USER_DATA_FOLDER="+ud,
		"WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS=--remote-debugging-port=0 "+hostRules+" "+extraBrowserArgs)
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	cmd.Stderr = os.Stderr
	t0 := time.Now()
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	lineCh := make(chan string, 1)
	go func() {
		buf := make([]byte, 256)
		n, _ := stdout.Read(buf)
		lineCh <- strings.TrimSpace(string(buf[:n]))
		io.Copy(io.Discard, stdout)
	}()
	var line string
	select {
	case line = <-lineCh:
	case <-time.After(40 * time.Second):
		cmd.Process.Kill()
		return nil, fmt.Errorf("采集进程启动超时")
	}
	f := strings.Fields(line)
	if len(f) != 3 || f[0] != "READY" {
		cmd.Process.Kill()
		return nil, fmt.Errorf("采集进程启动失败：%s", line)
	}
	d, err := dialCDP(context.Background(), "ws://127.0.0.1:"+f[1]+f[2])
	if err != nil {
		cmd.Process.Kill()
		return nil, err
	}
	b := &browser{cmd: cmd, d: d, userData: ud, stdin: stdin, started: t0}
	d.global = b.onBrowserEvent
	// 浏览器级自动接管：新 target（包括 window.open 弹窗，它们不会出现在打开者的页面会话里）都先暂停，
	// 由我们套上拦截规则再放行。
	if _, err := d.call("", "Target.setAutoAttach", map[string]any{"autoAttach": true, "waitForDebuggerOnStart": true, "flatten": true}); err != nil {
		cmd.Process.Kill()
		return nil, err
	}
	// 触发一次 getTargets 确认可用。
	if _, err := d.call("", "Target.getTargets", nil); err != nil {
		cmd.Process.Kill()
		return nil, err
	}
	return b, nil
}

func (b *browser) pid() int { return b.cmd.Process.Pid }

func (b *browser) stop() {
	b.stdin.Close()
	done := make(chan struct{})
	go func() { b.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		b.cmd.Process.Kill()
		<-done
	}
	os.RemoveAll(b.userData)
}

func (b *browser) kill() {
	b.cmd.Process.Kill()
	b.cmd.Wait()
}

type targetInfo struct {
	TargetID string `json:"targetId"`
	Type     string `json:"type"`
	URL      string `json:"url"`
}

func (b *browser) targets() ([]targetInfo, error) {
	res, err := b.d.call("", "Target.getTargets", nil)
	if err != nil {
		return nil, err
	}
	var r struct {
		TargetInfos []targetInfo `json:"targetInfos"`
	}
	json.Unmarshal(res, &r)
	return r.TargetInfos, nil
}

func (b *browser) pageCount() int {
	ts, err := b.targets()
	if err != nil {
		return -1
	}
	n := 0
	for _, t := range ts {
		if t.Type == "page" {
			n++
		}
	}
	return n
}

// treeStats 返回以 pid 为根的整棵进程树的工作集（MB）与进程数。
func treeStats(pid int) (float64, int) {
	ps := `$all=Get-CimInstance Win32_Process;$ids=@{};$ids[` + strconv.Itoa(pid) + `]=1;$ch=$true;while($ch){$ch=$false;foreach($p in $all){if($ids.ContainsKey([int]$p.ParentProcessId) -and -not $ids.ContainsKey([int]$p.ProcessId)){$ids[[int]$p.ProcessId]=1;$ch=$true}}};$m=0;$n=0;foreach($p in $all){if($ids.ContainsKey([int]$p.ProcessId)){$m+=$p.WorkingSetSize;$n++}};"$([math]::Round($m/1MB,1)) $n"`
	out, err := exec.Command("powershell", "-NoProfile", "-Command", ps).Output()
	if err != nil {
		return -1, -1
	}
	f := strings.Fields(string(out))
	if len(f) != 2 {
		return -1, -1
	}
	mb, _ := strconv.ParseFloat(f[0], 64)
	n, _ := strconv.Atoi(f[1])
	return mb, n
}

func (b *browser) onBrowserEvent(m cdpMsg) {
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
			if t := v.(*task); t.o.autoAttach {
				t.adopt(m)
				return
			}
		}
	}
	// 不是任何任务的弹窗（例如我们自己创建的 target）：直接放行。
	if e.WaitingForDebugger {
		go b.d.call(e.SessionID, "Runtime.runIfWaitingForDebugger", nil)
	}
}
