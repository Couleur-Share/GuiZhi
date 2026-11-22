package main

// 设置页「采集与转写」「MCP 设置」两个分区的主包适配器集成测试：
// 真实的 IPC 处理器 + 临时数据目录与临时用户主目录（不碰 %APPDATA%\GuiZhi，也不碰真实的 ~/.cursor）+ 注入的 HTTP 传输层（不访问真实网络）。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/settings"
)

// isolateHome 把用户主目录指到临时目录：一键写入 Cursor 配置的落点是 ~/.cursor/mcp.json，测试绝不能写到真实主目录。
func isolateHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	if got, err := os.UserHomeDir(); err != nil || got != home {
		t.Fatalf("用户主目录没有被隔离：%q %v", got, err)
	}
	return home
}

type mcpEnv struct {
	lib *Library
	svc services.MCP
	cfg string // config 目录
}

func newMCPEnv(t *testing.T) *mcpEnv {
	t.Helper()
	ud := t.TempDir()
	l := &Library{configDir: filepath.Join(ud, "config"), paths: Paths{UserData: ud}}
	b := newSystemBridge()
	l.registerMcp(b)
	return &mcpEnv{lib: l, svc: newNativeServices(b, newPushHub()).MCP, cfg: l.configDir}
}

// MCP 启动信息：命令是本程序自己的 --mcp 模式，环境变量为空，不需要 Node；接入片段由这份信息生成。
func TestSettingsCaptureMCPAdapterConfig(t *testing.T) {
	e := newMCPEnv(t)
	cfg, err := e.svc.Config(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	exe, _ := os.Executable()
	if !cfg.Available || cfg.Command != exe || cfg.ServerPath != exe || len(cfg.Args) != 1 || cfg.Args[0] != "--mcp" || cfg.Env == nil || len(cfg.Env) != 0 {
		t.Fatalf("启动信息：%+v", cfg)
	}
	if snippet := services.BuildCursorConfig(cfg); !strings.Contains(snippet, `"--mcp"`) || !strings.Contains(snippet, `"guizhi"`) {
		t.Errorf("Cursor 片段应带服务器名与 --mcp：%s", snippet)
	}
}

// 可访问范围：没有文件是「全部可见」；保存立即落到 config/mcp.json（MCP server 每次工具调用都重读它）；
// 坏形状（空列表写成 null、重复与空 ID）由处理器规整，界面拿到的是规整后的值。
func TestSettingsCaptureMCPAdapterScope(t *testing.T) {
	e := newMCPEnv(t)
	ctx := context.Background()

	got, err := e.svc.Scope(ctx)
	if err != nil || got.Mode != services.MCPScopeAll || !got.AllowUncategorized || got.AllowedCollectionIDs == nil || len(got.AllowedCollectionIDs) != 0 {
		t.Fatalf("没有配置文件时应是默认范围：%+v %v", got, err)
	}

	saved, err := e.svc.SetScope(ctx, services.MCPScope{Mode: services.MCPScopeSelected, AllowedCollectionIDs: []string{"c1", "c2", "c1", ""}, AllowUncategorized: false})
	if err != nil {
		t.Fatal(err)
	}
	if saved.Mode != "selected" || strings.Join(saved.AllowedCollectionIDs, ",") != "c1,c2" || saved.AllowUncategorized {
		t.Errorf("保存返回的应是规整后的值（去重、去空 ID）：%+v", saved)
	}
	file := readJSONFile(t, filepath.Join(e.cfg, "mcp.json"))
	ids, _ := file["allowedCollectionIds"].([]any)
	if file["mode"] != "selected" || len(ids) != 2 || file["allowUncategorized"] != false || file["version"] != float64(1) {
		t.Errorf("配置文件：%v", file)
	}
	if again, err := e.svc.Scope(ctx); err != nil || strings.Join(again.AllowedCollectionIDs, ",") != "c1,c2" || again.Mode != "selected" {
		t.Errorf("再读应是刚保存的：%+v %v", again, err)
	}

	// 选了「指定合集」但一个都没勾：保存 nil 列表要落成 []，不是 null
	empty, err := e.svc.SetScope(ctx, services.MCPScope{Mode: services.MCPScopeSelected})
	if err != nil || empty.AllowedCollectionIDs == nil || !empty.IsEmpty() {
		t.Fatalf("空范围：%+v %v", empty, err)
	}
	raw, _ := os.ReadFile(filepath.Join(e.cfg, "mcp.json"))
	if !strings.Contains(string(raw), `"allowedCollectionIds": []`) {
		t.Errorf("空列表应写成 []：\n%s", raw)
	}

	// 文件被手改坏：回到「全部可见」，而不是「全部不可见」
	if err := os.WriteFile(filepath.Join(e.cfg, "mcp.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if bad, err := e.svc.Scope(ctx); err != nil || bad.Mode != services.MCPScopeAll {
		t.Errorf("损坏的配置文件应退回全部可见：%+v %v", bad, err)
	}

	// 配置目录写不进去（路径被一个文件占了）：必须报错，界面据此回滚并提示
	blocked := newMCPEnv(t)
	if err := os.WriteFile(blocked.cfg, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := blocked.svc.SetScope(ctx, services.DefaultMCPScope()); err == nil {
		t.Error("写不进配置目录应返回错误")
	}
}

// 一键写入 Cursor：落到（临时）~/.cursor/mcp.json，合并而不覆盖——已有的其它服务器与注释保留，覆盖前留一份备份；
// 文件不是合法 JSON 时拒绝改动并说明路径；不支持的客户端带原因报错。
func TestSettingsCaptureMCPAdapterInstall(t *testing.T) {
	home := isolateHome(t)
	e := newMCPEnv(t)
	ctx := context.Background()
	path := filepath.Join(home, ".cursor", "mcp.json")

	res, err := e.svc.Install(ctx, services.MCPClientCursor)
	if err != nil {
		t.Fatal(err)
	}
	if res.FilePath != path || res.Replaced {
		t.Fatalf("首次写入应新建文件且不是更新：%+v", res)
	}
	servers, _ := readJSONFile(t, path)["mcpServers"].(map[string]any)
	entry, _ := servers["guizhi"].(map[string]any)
	if exe, _ := os.Executable(); entry["command"] != exe {
		t.Fatalf("写入的启动命令：%v", entry)
	}

	// 用户手写的内容：另一个服务器与注释都要保留
	user := "{\n  // 我的其它 MCP 服务器\n  \"mcpServers\": {\n    \"other\": { \"command\": \"node\" },\n    \"guizhi\": { \"command\": \"old\" }\n  }\n}\n"
	if err := os.WriteFile(path, []byte(user), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err = e.svc.Install(ctx, services.MCPClientCursor)
	if err != nil || !res.Replaced {
		t.Fatalf("已有归知条目时应是更新：%+v %v", res, err)
	}
	merged, _ := os.ReadFile(path)
	if !strings.Contains(string(merged), "// 我的其它 MCP 服务器") || !strings.Contains(string(merged), `"other"`) || strings.Contains(string(merged), `"old"`) {
		t.Errorf("合并后应保留注释与其它服务器、替换归知条目：\n%s", merged)
	}
	if backup, err := os.ReadFile(path + ".guizhi-backup"); err != nil || string(backup) != user {
		t.Errorf("覆盖前应留一份原文备份：%v", err)
	}

	// 文件坏了就住手：不覆盖，错误里带路径
	broken := "{ broken"
	if err := os.WriteFile(path, []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = e.svc.Install(ctx, services.MCPClientCursor)
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("不合法的 JSON 应拒绝并带上路径：%v", err)
	}
	if after, _ := os.ReadFile(path); string(after) != broken {
		t.Errorf("拒绝时不能改动原文件：%q", after)
	}

	if _, err := e.svc.Install(ctx, services.MCPClientCodex); err == nil {
		t.Error("Codex 不支持一键写入，应返回错误")
	}
}

// 机密：经适配器拿到的启动信息，环境变量值在格式化输出里一律脱敏（接入片段需要原值，只在复制时用）。
func TestSettingsCaptureMCPAdapterConfigRedactsEnv(t *testing.T) {
	b := newSystemBridge()
	b.handlers["mcp:config"] = func(context.Context, []json.RawMessage) (any, error) {
		return map[string]any{"available": true, "serverPath": "x", "command": "x", "args": nil, "env": map[string]string{"TOKEN": "sk-secret-value"}}, nil
	}
	cfg, err := newNativeServices(b, nil).MCP.Config(context.Background())
	if err != nil || cfg.Env["TOKEN"] != "sk-secret-value" || cfg.Args == nil {
		t.Fatalf("环境变量原值应保留给接入片段，args 应规整成空列表：%+v %v", cfg, err)
	}
	if out := fmt.Sprintf("%v %+v %#v", cfg, cfg, cfg); strings.Contains(out, "sk-secret-value") || !strings.Contains(out, "TOKEN=***") {
		t.Errorf("格式化输出应脱敏：%s", out)
	}
	if snippet := services.BuildCodexAddCommand(cfg); !strings.Contains(snippet, "--env TOKEN=sk-secret-value") {
		t.Errorf("复制出去的命令需要原值：%s", snippet)
	}
}

// ── 采集工具 ──

type toolsEnv struct {
	*settingsEnv
	svc services.CaptureTools
	set services.Settings
	hub *pushHub
	b   *Bridge
}

func newToolsEnv(t *testing.T) *toolsEnv {
	t.Helper()
	t.Setenv("PATH", t.TempDir()) // 系统 PATH 里没有 yt-dlp / ffmpeg：探测结果只取决于临时数据目录
	e := newSettingsEnv(t)
	b := newSystemBridge()
	e.lib.registerManagedTools(b)
	e.lib.registerFunasr(b)
	hub := newPushHub()
	e.lib.setPush(hub.Dispatch)
	e.lib.toolsHTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		h := http.Header{}
		if strings.HasSuffix(r.URL.Path, "/releases/latest") && r.Method == http.MethodGet {
			h.Set("Location", "https://github.com/yt-dlp/yt-dlp/releases/tag/2026.08.01")
			return &http.Response{StatusCode: 302, Header: h, Body: http.NoBody, Request: r}, nil
		}
		return &http.Response{StatusCode: 404, Header: h, Body: http.NoBody, Request: r}, nil
	})}
	return &toolsEnv{settingsEnv: e, svc: newNativeServices(b, hub).CaptureTools, set: e.ns, hub: hub, b: b}
}

// 引擎探测：没装时不是已安装、支持应用内安装、内置版落点在临时数据目录；force 绕过主进程缓存；
// 检查更新走注入的传输层，只有内置版才有当前版本；移除没装的内置版不报错。
func TestSettingsCaptureMCPAdapterToolStatus(t *testing.T) {
	e := newToolsEnv(t)
	ctx := context.Background()

	for _, tool := range []services.ManagedTool{services.ToolYtDlp, services.ToolFfmpeg} {
		st, err := e.svc.ToolStatus(ctx, tool, true)
		if err != nil {
			t.Fatalf("%s：%v", tool, err)
		}
		if st.Installed || st.Source != "" || !st.InstallSupported || !strings.HasPrefix(st.ManagedPath, e.paths.UserData) {
			t.Errorf("%s 没装时的状态：%+v", tool, st)
		}
		if err := e.svc.RemoveTool(ctx, tool); err != nil {
			t.Errorf("%s 移除没装的内置版不应报错：%v", tool, err)
		}
	}

	check, err := e.svc.CheckToolUpdate(ctx, services.ToolYtDlp)
	if err != nil || check.Latest != "2026.08.01" || check.Current != "" || check.UpdateAvailable {
		t.Errorf("没装内置版时没有当前版本，也不谈更新：%+v %v", check, err)
	}

	if _, err := e.svc.ToolStatus(ctx, "bogus", false); err == nil {
		t.Error("未知引擎应报错")
	}
}

// 安装失败的原因原样带回（不是「失败」两个字）：下载源都 404 时，错误里有处理器汇总的原因，且不留半截文件。
func TestSettingsCaptureMCPAdapterInstallFailure(t *testing.T) {
	e := newToolsEnv(t)
	_, err := e.svc.InstallTool(context.Background(), services.ToolYtDlp)
	if err == nil || strings.TrimSpace(err.Error()) == "" || err.Error() == "install failed" {
		t.Fatalf("安装失败应带处理器给出的原因：%v", err)
	}
	if st, err := e.svc.ToolStatus(context.Background(), services.ToolYtDlp, true); err != nil || st.Installed {
		t.Errorf("安装失败后不能显示已安装：%+v %v", st, err)
	}

	// 处理器返回 success:false 但没给原因时补一条兜底，不让界面拿到既失败又没有原因的结果
	b := newSystemBridge()
	b.handlers["funasr:install"] = func(context.Context, []json.RawMessage) (any, error) { return map[string]any{"success": false}, nil }
	b.handlers["funasr:update"] = func(_ context.Context, a []json.RawMessage) (any, error) {
		return map[string]any{"success": false, "error": "disk full"}, nil
	}
	svc := newNativeServices(b, nil).CaptureTools
	if _, err := svc.InstallFunasr(context.Background()); err == nil || err.Error() == "" {
		t.Errorf("没给原因应有兜底：%v", err)
	}
	if _, err := svc.UpdateFunasr(context.Background(), "9.9.9"); err == nil || err.Error() != "disk full" {
		t.Errorf("处理器的原因应原样带回：%v", err)
	}
}

// 选择文件 → 登记 → 才能存进设置：这是「可执行文件路径不能手输」的安全边界。
// 没经过选择器的路径被设置拒绝；选择器取消返回空串（不是错误）；选中并保存后，探测结果走自定义路径（路径无效时不是已安装）。
func TestSettingsCaptureMCPAdapterPickBinaryGate(t *testing.T) {
	e := newToolsEnv(t)
	ctx := context.Background()
	picked := filepath.Join(t.TempDir(), "yt-dlp.exe")
	e.lib.pickExecutable = func(string) (string, error) { return picked, nil }

	if _, err := e.set.Update(func(v *settings.Values) { v.App.YtDlpPath = picked }); err == nil {
		t.Fatal("没经过选择器的路径应被设置拒绝")
	}

	got, err := e.svc.PickToolBinary(ctx, services.ToolYtDlp)
	if err != nil || got != picked {
		t.Fatalf("选择：%q %v", got, err)
	}
	if _, err := e.set.Update(func(v *settings.Values) { v.App.YtDlpPath = got }); err != nil {
		t.Fatalf("选择器返回过的路径应能存进设置：%v", err)
	}
	if v, _ := e.set.Snapshot(); v.App.YtDlpPath != picked {
		t.Errorf("设置里的路径：%q", v.App.YtDlpPath)
	}
	// 同一个路径对 ffmpeg 同样生效（登记是按路径，不分引擎）；但没选过的另一个路径仍然被拒绝
	if _, err := e.set.Update(func(v *settings.Values) { v.App.FfmpegPath = filepath.Join(t.TempDir(), "other.exe") }); err == nil {
		t.Error("没选过的路径应被拒绝")
	}
	st, err := e.svc.ToolStatus(ctx, services.ToolYtDlp, true)
	if err != nil || st.Installed {
		t.Errorf("自定义路径指向不存在的文件时不是已安装：%+v %v", st, err)
	}

	// 取消：处理器回 null，适配器给空串
	e.lib.pickExecutable = func(string) (string, error) { return "", nil }
	if got, err := e.svc.PickToolBinary(ctx, services.ToolFfmpeg); err != nil || got != "" {
		t.Errorf("取消应是空串且不报错：%q %v", got, err)
	}
	// 选择器本身出错：原因带回
	e.lib.pickExecutable = func(string) (string, error) { return "", errors.New("dialog crashed") }
	if _, err := e.svc.PickToolBinary(ctx, services.ToolFfmpeg); err == nil || !strings.Contains(err.Error(), "dialog crashed") {
		t.Errorf("选择器出错应带原因：%v", err)
	}
}

// 本地转写引擎：没装时的状态（临时目录里没有运行时）、卸载没装的引擎的结果；
// 推送：下载进度与安装进度经 pushHub 解码成 DTO，取消订阅后不再收到，坏载荷被忽略。
func TestSettingsCaptureMCPAdapterFunasrStatusAndProgress(t *testing.T) {
	e := newToolsEnv(t)
	ctx := context.Background()

	st, err := e.svc.FunasrStatus(ctx, true)
	if err != nil || st.Installed || st.Running || !st.InstallSupported || st.Dir == "" {
		t.Fatalf("没装本地转写引擎时的状态：%+v %v", st, err)
	}

	var tools []services.ToolProgress
	var ffs int
	cancelTool := e.svc.OnToolProgress(services.ToolYtDlp, func(p services.ToolProgress) { tools = append(tools, p) })
	e.svc.OnToolProgress(services.ToolFfmpeg, func(services.ToolProgress) { ffs++ })
	total := int64(1000)
	e.lib.pushMain(chYtDlpProgress, toolDownloadProgress{Transferred: 250, Total: &total})
	e.lib.pushMain(chYtDlpProgress, toolDownloadProgress{Transferred: 300}) // 没有总量
	e.lib.pushMain(chYtDlpProgress, "not an object")
	if len(tools) != 2 || tools[0].Transferred != 250 || tools[0].Total != 1000 || tools[1].Total != 0 || ffs != 0 {
		t.Errorf("二进制引擎进度：%+v（ffmpeg 订阅收到 %d）", tools, ffs)
	}
	cancelTool()
	cancelTool()
	e.lib.pushMain(chYtDlpProgress, toolDownloadProgress{Transferred: 999})
	if len(tools) != 2 {
		t.Error("取消订阅后不应再收到")
	}
	e.lib.pushMain(chFfmpegProgress, toolDownloadProgress{Transferred: 1})
	if ffs != 1 {
		t.Errorf("ffmpeg 的进度走自己的频道：%d", ffs)
	}

	var steps []services.FunasrProgress
	cancelFn := e.svc.OnFunasrProgress(func(p services.FunasrProgress) { steps = append(steps, p) })
	pct := 40
	e.lib.pushMain(chFunasrProgress, funasrInstallProgress{Phase: "deps", Percent: &pct, Detail: "pip ..."})
	e.lib.pushMain(chFunasrProgress, funasrInstallProgress{Phase: "verify"})
	e.lib.pushMain(chFunasrProgress, map[string]any{"detail": "no phase"}) // 缺阶段的载荷无效
	cancelFn()
	e.lib.pushMain(chFunasrProgress, funasrInstallProgress{Phase: "rollback"})
	if len(steps) != 2 || steps[0].Phase != "deps" || steps[0].Percent == nil || *steps[0].Percent != 40 || steps[0].Detail != "pip ..." ||
		steps[1].Phase != "verify" || steps[1].Percent != nil {
		t.Errorf("安装进度：%+v", steps)
	}
}

// ── 平台账号 ──

// 平台账号：状态来自真实的会话管理（临时目录），退出与清除落到磁盘；
// 登录请求的形状是 { platform, forceRelogin }；取消没有进行中的登录返回 false；未知平台带原因报错。
func TestSettingsCaptureMCPAdapterPlatform(t *testing.T) {
	ud := t.TempDir()
	l := &Library{paths: Paths{UserData: ud}, configDir: filepath.Join(ud, "config")}
	l.platformCap = newPlatformSessions(l, ud)
	b := newSystemBridge()
	l.registerPlatformCapture(b)
	var loginArgs []map[string]any
	realLogin := b.handlers["platformCapture:login"]
	b.handlers["platformCapture:login"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		// 不真的拉起浏览器：只记录请求的形状，回一份已登录的状态
		var arg map[string]any
		if len(a) == 1 && json.Unmarshal(a[0], &arg) == nil {
			loginArgs = append(loginArgs, arg)
			return map[string]any{"platform": arg["platform"], "browser": "embedded", "browserVersion": "126.0", "available": true, "loggedIn": true, "busy": false}, nil
		}
		return realLogin(ctx, a)
	}
	svc := newNativeServices(b, nil).CaptureTools
	ctx := context.Background()

	list, err := svc.PlatformStatuses(ctx)
	if err != nil || len(list) < 3 {
		t.Fatalf("状态：%+v %v", list, err)
	}
	byName := map[string]services.PlatformStatus{}
	for _, s := range list {
		byName[s.Platform] = s
	}
	for _, p := range []string{services.PlatformXiaohongshu, services.PlatformDouyin, services.PlatformLinuxdo} {
		if s, ok := byName[p]; !ok || s.LoggedIn || s.Busy || s.Browser != "embedded" {
			t.Errorf("%s 的初始状态：%+v", p, s)
		}
	}

	st, err := svc.PlatformLogin(ctx, services.PlatformDouyin, true)
	if err != nil || !st.LoggedIn || st.Platform != "douyin" || st.BrowserVersion != "126.0" {
		t.Fatalf("登录：%+v %v", st, err)
	}
	if len(loginArgs) != 1 || loginArgs[0]["platform"] != "douyin" || loginArgs[0]["forceRelogin"] != true {
		t.Errorf("登录请求的形状：%v", loginArgs)
	}

	// 登录态标记与会话目录：退出清掉该平台，清除全部删掉整个 browser-capture 目录
	profile := l.platformCap.profileDir("douyin")
	if err := os.MkdirAll(profile, 0o755); err != nil {
		t.Fatal(err)
	}
	l.platformCap.writeLoginState("douyin", true)
	if list, _ := svc.PlatformStatuses(ctx); !statusOf(list, "douyin").LoggedIn {
		t.Fatal("写入登录态后状态应是已登录")
	}
	if err := svc.PlatformLogout(ctx, services.PlatformDouyin); err != nil {
		t.Fatal(err)
	}
	if list, _ := svc.PlatformStatuses(ctx); statusOf(list, "douyin").LoggedIn {
		t.Error("退出后应是未登录")
	}
	if _, err := os.Stat(profile); !os.IsNotExist(err) {
		t.Errorf("退出应删掉该平台的会话目录：%v", err)
	}

	if err := os.MkdirAll(l.platformCap.profileDir("linuxdo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := svc.PlatformClearAll(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(l.platformCap.baseDir); !os.IsNotExist(err) {
		t.Errorf("清除全部应删掉 browser-capture 目录：%v", err)
	}

	if ok, err := svc.PlatformCancelLogin(ctx, services.PlatformDouyin); err != nil || ok {
		t.Errorf("没有进行中的登录时取消应是 false：%v %v", ok, err)
	}
	if err := svc.PlatformLogout(ctx, "not-a-platform"); err == nil || err.Error() == "" {
		t.Errorf("未知平台应带原因报错：%v", err)
	}
}

func statusOf(list []services.PlatformStatus, platform string) services.PlatformStatus {
	for _, s := range list {
		if s.Platform == platform {
			return s
		}
	}
	return services.PlatformStatus{}
}

// 机密：平台登录失败的原因经适配器原样给界面（界面进可复制的详情），但 DTO 本身不含 Cookie、令牌之类的字段。
func TestSettingsCaptureMCPAdapterPlatformStatusHasNoCredentials(t *testing.T) {
	for _, forbidden := range []string{"cookie", "token", "password", "secret"} {
		raw, _ := json.Marshal(services.PlatformStatus{})
		if strings.Contains(strings.ToLower(string(raw)), forbidden) {
			t.Errorf("PlatformStatus 不应有 %s 相关字段：%s", forbidden, raw)
		}
	}
}
