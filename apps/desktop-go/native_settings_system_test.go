package main

// 设置页三个分区（快捷键、网络与 FlareSolverr、关于）的主包适配器集成测试：
// 真实的 IPC 处理器 + 临时数据目录（不碰 %APPDATA%\GuiZhi）+ 本地 httptest（不访问真实网络）。

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/settings"
)

func newSystemBridge() *Bridge {
	return &Bridge{handlers: map[string]handler{}, sends: map[string]func([]json.RawMessage){}, seen: map[string]int{}, missing: map[string]int{}}
}

func readJSONFile(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("%s 不是 JSON 对象：%v", path, err)
	}
	return out
}

// 快捷键：读写经 shortcuts:* 处理器落到 config/shortcuts.json 与 shortcut-mode.json，清空（空串）要保留，写成功后广播给界面。
func TestSettingsAdapterShortcutsRoundTrip(t *testing.T) {
	ud := t.TempDir()
	l := &Library{configDir: filepath.Join(ud, "config"), paths: Paths{UserData: ud}}
	b := newSystemBridge()
	l.registerShortcutsIPC(b)
	hub := newPushHub()
	l.setPush(hub.Dispatch)
	var updated int
	hub.Subscribe(chShortcutsUpdated, func(any) { updated++ })
	l.startShortcuts(true) // true：不注册系统级快捷键（不抢用户正在运行的归知的 Alt+Shift+P）
	svc := newNativeServices(b, hub).Shortcuts
	ctx := context.Background()

	cfg, err := svc.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.Accelerators, services.DefaultShortcuts()) || !reflect.DeepEqual(cfg.Modes, services.DefaultShortcutModes()) {
		t.Fatalf("没有配置文件时应是默认值：%+v", cfg)
	}

	next := cfg.Accelerators
	next[services.ShortcutSearch] = "CommandOrControl+K"
	next[services.ShortcutNewItem] = "" // 用户清掉了这一项
	if err := svc.Save(ctx, next); err != nil {
		t.Fatalf("保存：%v", err)
	}
	file := readJSONFile(t, filepath.Join(l.configDir, "shortcuts.json"))
	if file["search"] != "CommandOrControl+K" || file["newItem"] != "" || file["showApp"] != "Alt+Shift+P" {
		t.Errorf("配置文件应保存整张表（空串保留）：%v", file)
	}
	raw, _ := os.ReadFile(filepath.Join(l.configDir, "shortcuts.json"))
	if iShow, iNew, iSearch, iSet := strings.Index(string(raw), "showApp"), strings.Index(string(raw), "newItem"),
		strings.Index(string(raw), "search"), strings.Index(string(raw), "settings"); !(iShow < iNew && iNew < iSearch && iSearch < iSet) {
		t.Errorf("键顺序应与渲染层一致（showApp、newItem、search、settings）：\n%s", raw)
	}
	if updated != 1 {
		t.Errorf("保存成功应广播一次 shortcuts:updated：%d", updated)
	}
	if again, err := svc.Get(ctx); err != nil || again.Accelerators["search"] != "CommandOrControl+K" || again.Accelerators["newItem"] != "" {
		t.Errorf("再读应是刚保存的：%+v %v", again, err)
	}

	modes := cfg.Modes
	modes[services.ShortcutSearch] = services.ShortcutModeGlobal
	if err := svc.SetModes(ctx, modes); err != nil {
		t.Fatalf("保存模式：%v", err)
	}
	if file := readJSONFile(t, filepath.Join(l.configDir, "shortcut-mode.json")); file["search"] != "global" || file["showApp"] != "global" || file["newItem"] != "local" {
		t.Errorf("模式文件：%v", file)
	}
	if got, err := svc.Get(ctx); err != nil || got.Modes["search"] != "global" {
		t.Errorf("再读模式：%+v %v", got, err)
	}
}

// 主进程写不进配置文件时 shortcuts:set 返回 false：适配器必须报错，界面据此回滚并提示，而不是弹一句「已保存」。
func TestSettingsAdapterShortcutsSaveRefused(t *testing.T) {
	ud := t.TempDir()
	l := &Library{configDir: filepath.Join(ud, "config"), paths: Paths{UserData: ud}}
	b := newSystemBridge()
	l.registerShortcutsIPC(b)
	l.startShortcuts(true)
	if err := os.MkdirAll(filepath.Join(l.configDir, "shortcuts.json"), 0o755); err != nil { // 目标路径是目录：写文件必败
		t.Fatal(err)
	}
	svc := newNativeServices(b, newPushHub()).Shortcuts
	err := svc.Save(context.Background(), services.DefaultShortcuts())
	if !errors.Is(err, services.ErrShortcutNotSaved) {
		t.Fatalf("写不进文件应返回 ErrShortcutNotSaved：%v", err)
	}
}

// 配置文件被手改成别的类型时，那一项当作没有（界面用默认值兜底），而不是整体读取失败。
func TestSettingsAdapterShortcutsIgnoresNonStringEntries(t *testing.T) {
	b := newSystemBridge()
	b.handlers[chShortcutsGet] = func(context.Context, []json.RawMessage) (any, error) {
		return map[string]any{"showApp": "Alt+P", "search": 42, "settings": nil}, nil
	}
	b.handlers[chShortcutsGetMode] = func(context.Context, []json.RawMessage) (any, error) {
		return map[string]any{"showApp": "local", "search": true}, nil
	}
	cfg, err := newNativeServices(b, nil).Shortcuts.Get(context.Background())
	if err != nil || !reflect.DeepEqual(cfg.Accelerators, map[string]string{"showApp": "Alt+P"}) || !reflect.DeepEqual(cfg.Modes, map[string]string{"showApp": "local"}) {
		t.Fatalf("只留字符串项：%+v %v", cfg, err)
	}
	b.handlers[chShortcutsGet] = func(context.Context, []json.RawMessage) (any, error) { return nil, errors.New("处理器出错") }
	if _, err := newNativeServices(b, nil).Shortcuts.Get(context.Background()); err == nil || !strings.Contains(err.Error(), "处理器出错") {
		t.Fatalf("处理器出错应带原因返回：%v", err)
	}
}

// FlareSolverr：连通、各类失败的原因原样带回，设置无效在 Go 侧校验（与渲染层同一份规则）。
func TestSettingsAdapterSolverCheck(t *testing.T) {
	l := &Library{paths: Paths{UserData: t.TempDir()}, configDir: t.TempDir()}
	b := newSystemBridge()
	l.registerWebSnapshot(b)
	svc := newNativeServices(b, newPushHub()).Solver
	ctx := context.Background()

	var mu sync.Mutex
	var bodies []string
	serve := func(status int, body string) (port int, closeFn func()) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			buf := make([]byte, 256)
			n, _ := r.Body.Read(buf)
			mu.Lock()
			bodies = append(bodies, string(buf[:n]))
			mu.Unlock()
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		}))
		return srv.Listener.Addr().(*net.TCPAddr).Port, srv.Close
	}
	local := func(port int) settings.FlareSolverr {
		return settings.FlareSolverr{Enabled: false, Connection: "local", Port: port} // 未启用也能测试（renderer：未启用时仍可手动验证）
	}

	port, closeOK := serve(200, `{"status":"ok","sessions":[]}`)
	defer closeOK()
	if err := svc.Check(ctx, local(port)); err != nil {
		t.Fatalf("连通应返回 nil：%v", err)
	}
	if len(bodies) != 1 || bodies[0] != `{"cmd":"sessions.list"}` {
		t.Errorf("应只发一次 sessions.list：%q", bodies)
	}

	for _, c := range []struct {
		status int
		body   string
		want   string
	}{
		{200, `{"status":"error","message":"Timeout after 60s"}`, "FlareSolverr 验证超时（60 秒），请稍后重试"},
		{500, `{"status":"error","message":"boom with cookie=abc"}`, "FlareSolverr 未能完成请求，请检查服务状态或稍后重试"},
		{200, `not json`, "FlareSolverr 返回了无效响应"},
	} {
		port, closeFn := serve(c.status, c.body)
		err := svc.Check(ctx, local(port))
		closeFn()
		if err == nil || err.Error() != c.want {
			t.Errorf("%s → %v，期望 %q", c.body, err, c.want)
		}
	}

	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	closedPort := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	if err := svc.Check(ctx, local(closedPort)); err == nil || err.Error() != "无法连接 FlareSolverr，请检查服务是否启动" {
		t.Errorf("端口没有服务：%v", err)
	}
	if err := svc.Check(ctx, local(0)); err == nil || err.Error() != "FlareSolverr 设置无效，请填写有效端口和 SSH 主机别名" {
		t.Errorf("端口无效：%v", err)
	}
	if err := svc.Check(ctx, settings.FlareSolverr{Enabled: true, Connection: "ssh", Port: 8191}); err == nil || err.Error() != "请填写已有 SSH 配置中的主机别名" {
		t.Errorf("SSH 缺主机别名：%v", err)
	}
}

// 更新：检查、下载、安装的结果与失败原因，以及 updater:status 推送（含进度）经 hub 到订阅者。
func TestSettingsAdapterUpdaterFlow(t *testing.T) {
	f := newUpdaterFixture(t, "0.29.0")
	b := newSystemBridge()
	f.u.register(b)
	hub := newPushHub()
	inner := f.u.d.push
	f.u.d.push = func(ch string, p any) { inner(ch, p); hub.Dispatch(ch, p) } // 夹具原有的记录保留，同时走 hub（生产里是 Library.pushMain → hub）
	svc := newNativeServices(b, hub).Updater
	ctx := context.Background()

	var mu sync.Mutex
	var got []services.UpdateStatus
	cancel := svc.OnStatus(func(s services.UpdateStatus) { mu.Lock(); got = append(got, s); mu.Unlock() })
	states := func() []string {
		mu.Lock()
		defer mu.Unlock()
		var out []string
		for _, s := range got {
			out = append(out, s.State)
		}
		return out
	}

	// 先下载：没有检查过，失败原因带回
	if err := svc.Download(ctx, services.UpdateCheckOptions{}); err == nil || !strings.Contains(err.Error(), "Please check update first") {
		t.Fatalf("没检查就下载：%v", err)
	}
	// 没下载就安装：失败原因带回，不退出
	if err := svc.Install(ctx); err == nil || !strings.Contains(err.Error(), "No update filepath") {
		t.Fatalf("没下载就安装：%v", err)
	}
	mu.Lock()
	got = nil
	mu.Unlock()

	res, err := svc.Check(ctx, services.UpdateCheckOptions{Channel: settings.ChannelStable})
	if err != nil || !res.Available || res.Version != "0.30.0" {
		t.Fatalf("检查：%+v %v", res, err)
	}
	if s := strings.Join(states(), ","); s != "checking,available" {
		t.Errorf("检查的状态推送：%s", s)
	}
	mu.Lock()
	if got[1].Version != "0.30.0" {
		t.Errorf("available 应带版本号：%+v", got[1])
	}
	got = nil
	mu.Unlock()

	if err := svc.Download(ctx, services.UpdateCheckOptions{Channel: settings.ChannelStable}); err != nil {
		t.Fatalf("下载：%v", err)
	}
	mu.Lock()
	last := got[len(got)-1]
	var sawProgress bool
	for _, s := range got {
		if s.State == services.UpdateStateDownloading && s.Percent > 0 {
			sawProgress = true
		}
	}
	mu.Unlock()
	if last.State != services.UpdateStateDownloaded || last.Version != "0.30.0" || !sawProgress {
		t.Errorf("下载应推送进度并以 downloaded 结束：last=%+v progress=%v", last, sawProgress)
	}

	if err := svc.Install(ctx); err != nil {
		t.Fatalf("安装：%v", err)
	}
	if len(f.spawned) != 1 {
		t.Errorf("应启动一次安装包：%v", f.spawned)
	}
	select {
	case <-f.quit:
	case <-time.After(2 * time.Second):
		t.Error("安装后应请求退出应用")
	}

	if err := svc.OpenReleases(); err != nil || len(f.opened) != 1 || !strings.HasSuffix(f.opened[0], "/releases") {
		t.Errorf("打开发布页：%v %v", err, f.opened)
	}

	// 取消订阅后不再收到；坏载荷被忽略
	cancel()
	cancel()
	n := len(states())
	hub.Dispatch("updater:status", map[string]any{"status": "error", "error": "x"})
	hub.Dispatch("updater:status", "不是对象")
	if len(states()) != n {
		t.Error("取消订阅后不应再收到推送")
	}
	svc.OnStatus(func(services.UpdateStatus) { t.Error("坏载荷不应触达订阅者") })
	hub.Dispatch("updater:status", "不是对象")
	hub.Dispatch("updater:status", map[string]any{"foo": 1})
	updaterAdapter{b: b}.OnStatus(func(services.UpdateStatus) {})() // 没有 hub：什么也不订阅
}

// 开发模式下检查被禁用：原因带回，界面用 toast 展示。
func TestSettingsAdapterUpdaterCheckFailure(t *testing.T) {
	f := newUpdaterFixture(t, "0.29.0")
	f.u.d.isDev = func() bool { return true }
	b := newSystemBridge()
	f.u.register(b)
	svc := newNativeServices(b, newPushHub()).Updater
	if _, err := svc.Check(context.Background(), services.UpdateCheckOptions{}); err == nil || !strings.Contains(err.Error(), "development mode") {
		t.Fatalf("开发模式：%v", err)
	}
	// 回复没有原因时补兜底原因
	if err := (updaterReply{}).err("fallback"); err == nil || err.Error() != "fallback" {
		t.Errorf("兜底原因：%v", err)
	}
	if err := (updaterReply{Success: true}).err("x"); err != nil {
		t.Errorf("成功不报错：%v", err)
	}
}

// 检查更新的参数：镜像与通道照传；通道只认 preview，其余按稳定版。
func TestSettingsAdapterUpdaterOptions(t *testing.T) {
	if o := updaterOptions(services.UpdateCheckOptions{UseMirror: true, Channel: settings.ChannelPreview}); o["useMirror"] != true || o["channel"] != "preview" || o["trigger"] != "manual" {
		t.Errorf("preview：%v", o)
	}
	if o := updaterOptions(services.UpdateCheckOptions{Channel: "nightly"}); o["channel"] != "stable" || o["useMirror"] != false {
		t.Errorf("未知通道按稳定版：%v", o)
	}
}
