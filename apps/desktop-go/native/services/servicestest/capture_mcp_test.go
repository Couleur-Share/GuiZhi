package servicestest

import (
	"context"
	"errors"
	"testing"
	"time"

	"guizhi-mygo-shell/native/services"
)

// Capture 的默认状态：两个二进制引擎与本地转写引擎已装好，三个平台可用但未登录；
// 安装与移除会改变探测结果，探测的 force 与各动作的次数都被记下。
func TestCaptureFakeTools(t *testing.T) {
	var f Capture
	ctx := context.Background()

	st, err := f.ToolStatus(ctx, services.ToolYtDlp, false)
	if err != nil || !st.Installed || st.Source != services.ToolSourceManaged || !st.InstallSupported {
		t.Fatalf("默认应是已装好的内置版：%+v %v", st, err)
	}
	if _, err := f.ToolStatus(ctx, services.ToolYtDlp, true); err != nil {
		t.Fatal(err)
	}
	if got := f.StatusCalls(services.ToolYtDlp); len(got) != 2 || got[0] || !got[1] {
		t.Errorf("探测的 force 应按序记下：%v", got)
	}

	if err := f.RemoveTool(ctx, services.ToolFfmpeg); err != nil {
		t.Fatal(err)
	}
	if st, _ := f.ToolStatus(ctx, services.ToolFfmpeg, true); st.Installed || st.Source != "" {
		t.Errorf("移除后应是未安装：%+v", st)
	}
	f.SetInstallVersion(services.ToolFfmpeg, "N-1-20260801")
	if v, err := f.InstallTool(ctx, services.ToolFfmpeg); err != nil || v != "N-1-20260801" {
		t.Fatalf("安装：%q %v", v, err)
	}
	if st, _ := f.ToolStatus(ctx, services.ToolFfmpeg, true); !st.Installed || st.Version != "N-1-20260801" || st.Source != services.ToolSourceManaged {
		t.Errorf("安装后应是内置版：%+v", st)
	}
	boom := errors.New("boom")
	f.FailInstall(services.ToolFfmpeg, boom)
	if _, err := f.InstallTool(ctx, services.ToolFfmpeg); !errors.Is(err, boom) {
		t.Errorf("注入的安装失败应原样返回：%v", err)
	}
	if f.Installs(services.ToolFfmpeg) != 2 || f.Removes(services.ToolFfmpeg) != 1 {
		t.Errorf("次数：安装 %d 移除 %d", f.Installs(services.ToolFfmpeg), f.Removes(services.ToolFfmpeg))
	}

	f.SetPick(services.ToolYtDlp, `C:\bin\yt-dlp.exe`)
	if p, err := f.PickToolBinary(ctx, services.ToolYtDlp); err != nil || p != `C:\bin\yt-dlp.exe` || f.Picks(services.ToolYtDlp) != 1 {
		t.Errorf("选择文件：%q %v", p, err)
	}
}

// 进度订阅：取消后不再收到，取消函数可重复调用；两个引擎各自独立。
func TestCaptureFakeProgress(t *testing.T) {
	var f Capture
	var got []services.ToolProgress
	cancel := f.OnToolProgress(services.ToolYtDlp, func(p services.ToolProgress) { got = append(got, p) })
	var other int
	f.OnToolProgress(services.ToolFfmpeg, func(services.ToolProgress) { other++ })
	f.EmitToolProgress(services.ToolYtDlp, services.ToolProgress{Transferred: 5, Total: 10})
	cancel()
	cancel()
	f.EmitToolProgress(services.ToolYtDlp, services.ToolProgress{Transferred: 9, Total: 10})
	if len(got) != 1 || got[0].Transferred != 5 || other != 0 || f.ToolSubscribers(services.ToolYtDlp) != 0 || f.ToolSubscribers(services.ToolFfmpeg) != 1 {
		t.Errorf("订阅：got=%v other=%d", got, other)
	}

	var fn []services.FunasrProgress
	c2 := f.OnFunasrProgress(func(p services.FunasrProgress) { fn = append(fn, p) })
	pct := 42
	f.EmitFunasrProgress(services.FunasrProgress{Phase: services.FunasrPhaseDeps, Percent: &pct})
	c2()
	f.EmitFunasrProgress(services.FunasrProgress{Phase: services.FunasrPhaseVerify})
	if len(fn) != 1 || fn[0].Phase != "deps" || *fn[0].Percent != 42 || f.FunasrSubscribers() != 0 {
		t.Errorf("转写引擎订阅：%+v", fn)
	}
}

// 本地转写引擎：安装、更新、卸载改变状态，失败注入后不改状态；更新记下目标版本。
func TestCaptureFakeFunasr(t *testing.T) {
	var f Capture
	ctx := context.Background()
	if err := f.UninstallFunasr(ctx); err != nil {
		t.Fatal(err)
	}
	if st, _ := f.FunasrStatus(ctx, true); st.Installed || st.Version != "" {
		t.Errorf("卸载后应是未安装：%+v", st)
	}
	if v, err := f.InstallFunasr(ctx); err != nil || v != "1.2.0" {
		t.Fatalf("安装：%q %v", v, err)
	}
	f.SetFunasrUpdateResult(services.FunasrUpdateResult{Version: "1.3.0", Warning: "stale backup"})
	if res, err := f.UpdateFunasr(ctx, "1.3.0"); err != nil || res.Warning != "stale backup" {
		t.Fatalf("更新：%+v %v", res, err)
	}
	if st, _ := f.FunasrStatus(ctx, false); st.Version != "1.3.0" {
		t.Errorf("更新后版本：%+v", st)
	}
	boom := errors.New("boom")
	f.FailFunasrUpdate(boom)
	if _, err := f.UpdateFunasr(ctx, "1.4.0"); !errors.Is(err, boom) {
		t.Errorf("更新失败：%v", err)
	}
	if st, _ := f.FunasrStatus(ctx, false); st.Version != "1.3.0" {
		t.Errorf("更新失败不能改版本：%+v", st)
	}
	if got := f.FunasrUpdates(); len(got) != 2 || got[0] != "1.3.0" || got[1] != "1.4.0" {
		t.Errorf("更新目标版本：%v", got)
	}
	if f.FunasrInstalls() != 1 || f.FunasrUninstalls() != 1 {
		t.Errorf("次数：安装 %d 卸载 %d", f.FunasrInstalls(), f.FunasrUninstalls())
	}
}

// 平台账号：登录期间 busy，成功后已登录；失败不改状态；退出与清除；未知平台报错。
func TestCaptureFakeAccounts(t *testing.T) {
	var f Capture
	f.LoginGate = make(chan struct{})
	ctx := context.Background()
	done := make(chan error, 1)
	go func() {
		_, err := f.PlatformLogin(ctx, services.PlatformDouyin, true)
		done <- err
	}()
	// 等登录动作进入阻塞（busy 为真）
	for i := 0; i < 400; i++ {
		list, _ := f.PlatformStatuses(ctx)
		if len(list) == 3 && list[1].Busy {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	list, _ := f.PlatformStatuses(ctx)
	if !list[1].Busy || list[1].Platform != services.PlatformDouyin {
		t.Fatalf("登录期间账号应是忙的：%+v", list)
	}
	close(f.LoginGate)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	list, _ = f.PlatformStatuses(ctx)
	if list[1].Busy || !list[1].LoggedIn {
		t.Errorf("登录成功后应已登录且不忙：%+v", list[1])
	}
	if got := f.Logins(); len(got) != 1 || got[0] != (LoginCall{Platform: "douyin", Force: true}) {
		t.Errorf("登录调用：%v", got)
	}

	if err := f.PlatformLogout(ctx, services.PlatformDouyin); err != nil {
		t.Fatal(err)
	}
	if list, _ = f.PlatformStatuses(ctx); list[1].LoggedIn {
		t.Error("退出后应是未登录")
	}
	boom := errors.New("boom")
	f.FailLogin(boom)
	if _, err := f.PlatformLogin(ctx, services.PlatformLinuxdo, false); !errors.Is(err, boom) {
		t.Errorf("登录失败：%v", err)
	}
	if list, _ = f.PlatformStatuses(ctx); list[2].LoggedIn || list[2].Busy {
		t.Errorf("登录失败不能改登录态且不再忙：%+v", list[2])
	}
	if _, err := f.PlatformLogin(ctx, "nope", false); err == nil {
		t.Error("未知平台应报错")
	}
	if ok, _ := f.PlatformCancelLogin(ctx, services.PlatformLinuxdo); !ok {
		t.Error("默认应能取消")
	}
	f.RefuseCancel(true)
	if ok, _ := f.PlatformCancelLogin(ctx, services.PlatformLinuxdo); ok {
		t.Error("RefuseCancel 后应返回 false")
	}
	if err := f.PlatformClearAll(ctx); err != nil || f.Clears() != 1 {
		t.Errorf("清除：%v %d", err, f.Clears())
	}
}

// MCP 假实现：保存范围返回副本、失败注入不改状态、一键写入记下客户端。
func TestMCPFake(t *testing.T) {
	var f MCPFake
	ctx := context.Background()
	cfg, err := f.Config(ctx)
	if err != nil || !cfg.Available || cfg.Command == "" || f.Configs() != 1 {
		t.Fatalf("默认配置：%+v %v", cfg, err)
	}
	if s, _ := f.Scope(ctx); s.Mode != services.MCPScopeAll || !s.AllowUncategorized {
		t.Errorf("默认范围：%+v", s)
	}

	in := services.MCPScope{Mode: services.MCPScopeSelected, AllowedCollectionIDs: []string{"a"}}
	out, err := f.SetScope(ctx, in)
	if err != nil || out.Mode != "selected" {
		t.Fatalf("保存范围：%+v %v", out, err)
	}
	in.AllowedCollectionIDs[0] = "mutated" // 保存后改调用方的切片，不能影响已保存的值
	if got := f.CurrentScope(); got.AllowedCollectionIDs[0] != "a" {
		t.Errorf("保存的范围应是副本：%+v", got)
	}
	boom := errors.New("boom")
	f.FailSetScope(boom)
	if _, err := f.SetScope(ctx, services.DefaultMCPScope()); !errors.Is(err, boom) || f.CurrentScope().Mode != "selected" {
		t.Errorf("保存失败不能改状态：%v %+v", err, f.CurrentScope())
	}
	if got := f.ScopeSets(); len(got) != 1 {
		t.Errorf("只记成功的保存：%v", got)
	}

	f.SetInstallResult(services.MCPInstallResult{FilePath: "x.json", Replaced: true})
	if res, err := f.Install(ctx, services.MCPClientCursor); err != nil || !res.Replaced || len(f.Installs()) != 1 {
		t.Errorf("一键写入：%+v %v", res, err)
	}
}
