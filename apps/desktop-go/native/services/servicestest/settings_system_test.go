package servicestest

import (
	"context"
	"errors"
	"testing"

	"guizhi-mygo-shell/native/services"
	"guizhi-mygo-shell/native/settings"
)

func TestShortcutsFakeKeepsConfigAndRecordsSaves(t *testing.T) {
	var f Shortcuts
	ctx := context.Background()
	cfg, err := f.Get(ctx)
	if err != nil || cfg.Accelerators[services.ShortcutShowApp] != "Alt+Shift+P" || cfg.Modes[services.ShortcutShowApp] != services.ShortcutModeGlobal {
		t.Fatalf("零值应是默认快捷键：%+v %v", cfg, err)
	}

	next := cfg.Accelerators
	next[services.ShortcutSearch] = "CommandOrControl+K"
	if err := f.Save(ctx, next); err != nil {
		t.Fatal(err)
	}
	next[services.ShortcutSearch] = "改了入参也不该影响已保存的"
	if got := f.Current().Accelerators[services.ShortcutSearch]; got != "CommandOrControl+K" {
		t.Errorf("保存应复制入参：%q", got)
	}
	if len(f.Saves()) != 1 {
		t.Errorf("应记录一次保存：%d", len(f.Saves()))
	}

	boom := errors.New("磁盘已满")
	f.FailSave(boom)
	before := f.Current()
	if err := f.Save(ctx, map[string]string{}); !errors.Is(err, boom) || f.Current().Accelerators[services.ShortcutSearch] != before.Accelerators[services.ShortcutSearch] {
		t.Errorf("失败不能改配置：%v", err)
	}
	f.FailSave(nil)

	if err := f.SetModes(ctx, map[string]string{services.ShortcutShowApp: services.ShortcutModeLocal}); err != nil || len(f.ModeSets()) != 1 {
		t.Errorf("模式保存：%v %d", err, len(f.ModeSets()))
	}
	f.FailGet(boom)
	if _, err := f.Get(ctx); !errors.Is(err, boom) {
		t.Errorf("读取应返回注入的错误：%v", err)
	}
}

func TestSolverAndUpdaterFakes(t *testing.T) {
	ctx := context.Background()
	var s Solver
	cfg := settings.FlareSolverr{Enabled: true, Connection: "local", Port: 8191}
	if err := s.Check(ctx, cfg); err != nil || len(s.Calls()) != 1 || s.Calls()[0] != cfg {
		t.Fatalf("零值应连通并记录配置：%v %+v", err, s.Calls())
	}
	boom := errors.New("无法连接 FlareSolverr")
	s.FailWith(boom)
	if err := s.Check(ctx, cfg); !errors.Is(err, boom) {
		t.Fatalf("应返回注入的错误：%v", err)
	}

	var u Updater
	u.SetResult(services.UpdateCheckResult{Available: true, Version: "1.2.3"})
	res, err := u.Check(ctx, services.UpdateCheckOptions{UseMirror: true, Channel: settings.ChannelPreview})
	if err != nil || !res.Available || res.Version != "1.2.3" || len(u.Checks()) != 1 || !u.Checks()[0].UseMirror {
		t.Fatalf("检查：%+v %v %+v", res, err, u.Checks())
	}
	var seen []services.UpdateStatus
	cancel := u.OnStatus(func(st services.UpdateStatus) { seen = append(seen, st) })
	u.Emit(services.UpdateStatus{State: services.UpdateStateDownloading, Percent: 42})
	cancel()
	cancel() // 可重复取消
	u.Emit(services.UpdateStatus{State: services.UpdateStateDownloaded})
	if len(seen) != 1 || seen[0].Percent != 42 || u.Subscribers() != 0 {
		t.Fatalf("取消后不再收到推送：%+v 订阅者 %d", seen, u.Subscribers())
	}
	u.FailDownload(boom)
	if err := u.Download(ctx, services.UpdateCheckOptions{}); !errors.Is(err, boom) {
		t.Fatalf("下载应返回注入的错误：%v", err)
	}
	if err := u.Install(ctx); err != nil || u.Installs() != 1 {
		t.Fatalf("安装：%v %d", err, u.Installs())
	}
	if err := u.OpenReleases(); err != nil || u.ReleaseOpens() != 1 {
		t.Fatalf("发布页：%v %d", err, u.ReleaseOpens())
	}
}
