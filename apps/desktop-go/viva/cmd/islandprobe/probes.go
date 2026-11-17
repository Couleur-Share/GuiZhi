package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"guizhi-mygo-shell/viva/island"
)

// pageServer 按路径返回内存里的页面（路径可带或不带前导斜杠），MIME 按扩展名定。
func pageServer(pages map[string]string) func(path string) (string, []byte, bool) {
	return func(path string) (string, []byte, bool) {
		name := strings.TrimPrefix(path, "/")
		if name == "" {
			name = "index.html"
		}
		body, ok := pages[name]
		if !ok {
			return "", nil, false
		}
		mime := "text/html; charset=utf-8"
		switch filepath.Ext(name) {
		case ".js":
			mime = "application/javascript; charset=utf-8"
		case ".json":
			mime = "application/json; charset=utf-8"
		case ".svg":
			mime = "image/svg+xml"
		}
		return mime, []byte(body), true
	}
}

// msgBox 收集页面发来的原始消息文本。
type msgBox struct{ ch chan string }

func newMsgBox() *msgBox { return &msgBox{ch: make(chan string, 8192)} }

func (m *msgBox) put(text string) {
	select {
	case m.ch <- text:
	default: // 满了就丢：探针只关心有没有到达，不关心洪水里丢了几条
	}
}

// wait 等一条 {"t": typ} 的 JSON 消息，其余消息（含非 JSON）丢弃。超时返回 false。
func (m *msgBox) wait(typ string, timeout time.Duration) (map[string]any, bool) {
	deadline := time.After(timeout)
	for {
		select {
		case text := <-m.ch:
			var obj map[string]any
			if json.Unmarshal([]byte(text), &obj) == nil && obj["t"] == typ {
				return obj, true
			}
		case <-deadline:
			return nil, false
		}
	}
}

// waitRaw 等一条以 prefix 开头的原始消息，返回它的字节数；超时返回 -1。
func (m *msgBox) waitRaw(prefix string, timeout time.Duration) int {
	deadline := time.After(timeout)
	for {
		select {
		case text := <-m.ch:
			if strings.HasPrefix(text, prefix) {
				return len(text)
			}
		case <-deadline:
			return -1
		}
	}
}

// securityProbe 打开敌意页面，记下它每一项越界尝试的结果。
//
//	prod：岛的默认响应头（island.DefaultCSP：脚本只许自有 scheme 的文件），敌意脚本改成外链 probe.js；
//	inline：探针的内联脚本 + 经 Config.CSP 覆盖的 probeCSP（只放行内联脚本，其余照旧关闭）——
//	        用来看内层框架里有没有 chrome.webview、消息能不能到 Go（prod 里框架的内联脚本被策略挡住，看不到）。
func securityProbe(e *env, mode string) map[string]any {
	res := map[string]any{"mode": mode}
	e.hits.reset()
	box := newMsgBox()
	var mu sync.Mutex
	var denied []string
	seen := map[string]int{}
	onMessage := func(text string) {
		box.put(text)
		if len(text) >= 4096 {
			return
		}
		var obj map[string]any
		if json.Unmarshal([]byte(text), &obj) == nil {
			if typ, ok := obj["t"].(string); ok {
				mu.Lock()
				seen[typ]++
				mu.Unlock()
			}
		}
	}

	const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="2" height="2"><rect width="2" height="2" fill="red"/></svg>`
	js := strings.NewReplacer("__LOOP__", e.loop).Replace(securityJS)
	pages := map[string]string{"data.json": `{"own":"ok"}`, "pic.svg": svg}
	cfg := island.Config{
		OnMessage: onMessage,
		OnDenied: func(url string, mainFrame bool) {
			mu.Lock()
			denied = append(denied, fmt.Sprintf("%s (main=%t)", url, mainFrame))
			mu.Unlock()
		},
	}
	switch mode {
	case "prod":
		pages["index.html"] = securityShellHTML
		pages["probe.js"] = js
	case "inline":
		pages["index.html"] = strings.NewReplacer("__LOOP__", e.loop).Replace(securityHTML)
		cfg.CSP = probeCSP
	case "bare":
		pages["index.html"] = strings.NewReplacer("__LOOP__", e.loop).Replace(securityHTML)
	default:
		res["error"] = "未知模式 " + mode
		return res
	}
	cfg.Serve = pageServer(pages)
	if mode == "bare" {
		view, err := e.openBare(cfg)
		if err != nil {
			res["openError"] = err.Error()
			return res
		}
		defer e.closeBare(view)
	} else {
		isl, err := e.open(cfg, true, "")
		if err != nil {
			res["openError"] = err.Error()
			return res
		}
		defer e.closeIsland(isl)
	}
	if _, ok := box.wait("ready", 20*time.Second); !ok {
		res["ready"] = false
		return res
	}
	// R5 的证据：岛开着时 TEMP 与 LOCALAPPDATA 里出现了哪些目录。
	res["tempDirsWhileOpen"] = listDirs(os.TempDir())
	res["localAppDataDirsWhileOpen"] = listDirs(os.Getenv("LOCALAPPDATA"))
	done, ok := box.wait("done", 120*time.Second)
	res["done"] = ok
	if ok {
		res["page"] = done["out"]
	}
	time.Sleep(1500 * time.Millisecond) // 让尾部请求落地
	res["serverHits"] = e.hits.snapshot()
	res["serverHitDetails"] = e.hits.details()
	mu.Lock()
	res["deniedNavigations"] = append([]string(nil), denied...)
	seenCopy := map[string]int{}
	for typ, n := range seen {
		seenCopy[typ] = n
	}
	res["messageTypesSeen"] = seenCopy
	mu.Unlock()

	// 下载：策略应当拒绝，所以下载目录里不该出现文件；万一出现就记下并删掉。
	download := filepath.Join(os.Getenv("USERPROFILE"), "Downloads", "gz-island-probe-download.txt")
	if _, statErr := os.Stat(download); statErr == nil {
		res["downloadFileCreated"] = true
		_ = os.Remove(download)
	} else {
		res["downloadFileCreated"] = false
	}
	return res
}

// listDirs 列出 dir 下的一级目录名。
func listDirs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []string{"读取失败：" + err.Error()}
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	return names
}

// ephemeralProbe 在一个岛里写 localStorage 与 IndexedDB，关掉后在另一个岛里读：应当读不到。
func ephemeralProbe(e *env) map[string]any {
	res := map[string]any{}
	run := func(html string) (any, bool) {
		box := newMsgBox()
		isl, err := e.open(island.Config{Serve: pageServer(map[string]string{"index.html": html}), CSP: probeCSP, OnMessage: box.put}, true, "")
		if err != nil {
			return err.Error(), false
		}
		defer e.closeIsland(isl)
		done, ok := box.wait("done", 30*time.Second)
		if !ok {
			return "超时", false
		}
		return done["out"], true
	}
	res["write"], _ = run(ephemeralWriteHTML)
	res["read"], _ = run(ephemeralReadHTML)
	return res
}

// msgSizeProbe 测页面→Go 与 Go→页面的单条消息能走多大。
func msgSizeProbe(e *env) map[string]any {
	res := map[string]any{}
	box := newMsgBox()
	isl, err := e.open(island.Config{Serve: pageServer(map[string]string{"index.html": msgHTML}), CSP: probeCSP, OnMessage: box.put}, true, "")
	if err != nil {
		res["openError"] = err.Error()
		return res
	}
	defer e.closeIsland(isl)
	if _, ok := box.wait("ready", 20*time.Second); !ok {
		res["ready"] = false
		return res
	}
	sizes := []int{64 << 10, 1 << 20, 4 << 20, 16 << 20, 64 << 20}
	pageToGo := map[string]any{}
	goToPage := map[string]any{}
	for _, n := range sizes {
		label := fmt.Sprintf("%dKB", n>>10)
		start := time.Now()
		e.onUI(func() { isl.View().PostMessage(fmt.Sprintf("send:%d", n)) })
		got := box.waitRaw(fmt.Sprintf("big:%d:", n), 40*time.Second)
		if got < 0 {
			pageToGo[label] = "超时（40s 内没收到）"
			break
		}
		pageToGo[label] = map[string]any{"receivedBytes": got, "ms": time.Since(start).Milliseconds()}
	}
	for _, n := range sizes {
		label := fmt.Sprintf("%dKB", n>>10)
		start := time.Now()
		payload := strings.Repeat("z", n)
		e.onUI(func() { isl.View().PostMessage(payload) })
		got, ok := box.wait("recv", 40*time.Second)
		if !ok {
			goToPage[label] = "超时（40s 内页面没回报）"
			break
		}
		goToPage[label] = map[string]any{"pageSawChars": got["n"], "ms": time.Since(start).Milliseconds()}
	}
	res["pageToGo"] = pageToGo
	res["goToPage"] = goToPage
	return res
}

// readingProbe 在岛里放一篇中文长文，程序化来回滚动 6 秒，记帧间隔分布。
func readingProbe(e *env) map[string]any {
	res := map[string]any{}
	for _, size := range []int{50 << 10, 200 << 10} {
		label := fmt.Sprintf("%dKB", size>>10)
		art := article(size)
		box := newMsgBox()
		isl, err := e.open(island.Config{Serve: pageServer(map[string]string{"index.html": readingHTML(art)}), CSP: probeCSP, OnMessage: box.put}, true, "")
		if err != nil {
			res[label] = map[string]any{"openError": err.Error()}
			continue
		}
		entry := map[string]any{"articleBytes": len(art)}
		if ready, ok := box.wait("ready", 30*time.Second); ok {
			entry["layout"] = map[string]any{"pageHeightPx": ready["height"], "domNodes": ready["nodes"]}
			time.Sleep(1 * time.Second)
			e.onUI(func() { isl.View().PostMessage("go") })
			if done, ok := box.wait("done", 40*time.Second); ok {
				entry["scroll"] = done
			} else {
				entry["scroll"] = "超时"
			}
		} else {
			entry["ready"] = false
		}
		e.closeIsland(isl)
		res[label] = entry
	}
	return res
}

// throttleScenario 是隐藏页探针的一个场景。
type throttleScenario struct {
	Name       string
	Place      bool   // 岛是否摆进主窗口（没摆 = Viva 眼里隐藏）
	HideWindow bool   // 页面就绪后把主窗口收起来（SW_HIDE，等价于收托盘）
	Flags      string // 只在 Open 期间追加进浏览器参数
}

// 节流开关：Chromium 的三个后台限速开关，是 MyGo 的 ThrottlingOff 在同一个引擎上的对应物。
const noThrottleFlags = "--disable-background-timer-throttling --disable-renderer-backgrounding --disable-backgrounding-occluded-windows"

// throttleProbe 比较「可见」「岛没摆」「窗口收起」三种状态下页面计时器与渲染的节奏，以及加开关后的变化。
func throttleProbe(e *env, seconds int) map[string]any {
	scenarios := []throttleScenario{
		{Name: "visible（摆在可见窗口里，基线）", Place: true},
		{Name: "undeclared（岛开着但没摆进窗口）", Place: false},
		{Name: "window-hidden（窗口收起，等价收托盘）", Place: true, HideWindow: true},
		{Name: "undeclared+flags（没摆，开三个后台限速开关）", Place: false, Flags: noThrottleFlags},
		{Name: "window-hidden+flags（窗口收起，开三个开关）", Place: true, HideWindow: true, Flags: noThrottleFlags},
	}
	res := map[string]any{"secondsPerScenario": seconds}
	for _, sc := range scenarios {
		fmt.Fprintf(os.Stderr, "   场景 %s\n", sc.Name)
		entry := map[string]any{"place": sc.Place, "hideWindow": sc.HideWindow, "flags": sc.Flags}
		box := newMsgBox()
		isl, err := e.open(island.Config{Serve: pageServer(map[string]string{"index.html": throttleHTML}), CSP: probeCSP, OnMessage: box.put}, sc.Place, sc.Flags)
		if err != nil {
			entry["openError"] = err.Error()
			res[sc.Name] = entry
			continue
		}
		if ready, ok := box.wait("ready", 30*time.Second); ok {
			entry["visibilityAtReady"] = ready["visibility"]
			if sc.HideWindow {
				e.onUI(func() { e.win.Hide() })
				time.Sleep(1500 * time.Millisecond)
			}
			e.onUI(func() { isl.View().PostMessage(fmt.Sprint(seconds * 1000)) })
			if done, ok := box.wait("done", time.Duration(seconds+40)*time.Second); ok {
				entry["result"] = done
			} else {
				entry["result"] = "超时（页面没回报）"
			}
			if sc.HideWindow {
				e.onUI(func() { e.win.Show() })
			}
		} else {
			entry["ready"] = false
		}
		e.closeIsland(isl)
		res[sc.Name] = entry
	}
	return res
}
