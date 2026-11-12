package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 复现真实缺陷：GUI 子系统的 GuiZhi.exe 在数据库损坏时 log.Fatalf 直接退出，
// 用户双击后什么也看不到（Electron 版会 dialog.showErrorBox）。
// 要求：原因必须落盘到 logs/error.log；交互启动时必须展示原因并给出恢复指引；自动化实例不得弹窗阻塞。

func TestReportStartupFailureShowsDialogWithRecoveryHint(t *testing.T) {
	ud := t.TempDir()
	var gotTitle, gotText string
	reportStartupFailure(ud, errors.New("database disk image is malformed (11)"), true, func(title, text string) {
		gotTitle, gotText = title, text
	})
	if !strings.Contains(gotTitle, "启动错误") {
		t.Fatalf("标题应包含「启动错误」，实际：%q", gotTitle)
	}
	for _, want := range []string{
		"database disk image is malformed (11)",
		filepath.Join(ud, "backups"), // 告诉用户去哪里找自动备份
		filepath.Join(ud, "logs"),    // 告诉用户去哪里看日志
	} {
		if !strings.Contains(gotText, want) {
			t.Errorf("对话框正文缺少 %q，正文：\n%s", want, gotText)
		}
	}
}

func TestReportStartupFailureWritesErrorLog(t *testing.T) {
	ud := t.TempDir()
	reportStartupFailure(ud, errors.New("数据库初始化失败：x"), false, nil)
	data, err := os.ReadFile(filepath.Join(ud, "logs", "error.log"))
	if err != nil {
		t.Fatalf("启动失败必须写入 logs/error.log：%v", err)
	}
	line := string(data)
	if !strings.Contains(line, `"scope":"startup"`) || !strings.Contains(line, "数据库初始化失败") {
		t.Fatalf("日志内容不符：%s", line)
	}
}

func TestReportStartupFailureNonInteractiveNeverShowsDialog(t *testing.T) {
	called := false
	reportStartupFailure(t.TempDir(), errors.New("boom"), false, func(string, string) { called = true })
	if called {
		t.Fatal("自动化实例（-shot/-bench/-aitest）不得弹出模态对话框，否则无人值守的验证会永久挂起")
	}
}

func TestReportStartupFailureToleratesMissingUserData(t *testing.T) {
	reportStartupFailure("", errors.New("boom"), true, func(title, text string) {
		if strings.Contains(text, string(filepath.Separator)+"backups") {
			t.Errorf("没有 userData 时不应给出虚假的备份目录：%s", text)
		}
	})
}
// 窗口创建失败（典型：WebView2 运行时缺失，或注册表登记的版本目录已不存在）时 mygo 会 panic。
// 沙盒实测：panic 发生在 GUI 子系统进程里，既没有 logs/error.log 也没有对话框，用户只看到进程消失。
func TestCreateWindowGuardedTurnsPanicIntoActionableError(t *testing.T) {
	err := createWindowGuarded(func() {
		panic("mygo: cannot create window: mygo: the WebView2 Runtime registered at C:\\x\\157.0.1 is missing or incomplete; reinstall it from https://go.microsoft.com/fwlink/p/?LinkId=2124703")
	})
	if err == nil {
		t.Fatal("panic 必须转成错误，不能继续向上传播")
	}
	msg := err.Error()
	for _, want := range []string{"is missing or incomplete", "WebView2", "go.microsoft.com/fwlink/p/?LinkId=2124703"} {
		if !strings.Contains(msg, want) {
			t.Errorf("错误说明缺少 %q：%s", want, msg)
		}
	}
	if !strings.Contains(msg, "修复") && !strings.Contains(msg, "重新安装") {
		t.Errorf("WebView2 故障应给出修复指引：%s", msg)
	}
}

func TestCreateWindowGuardedPassesThroughOnSuccessAndNonStringPanics(t *testing.T) {
	if err := createWindowGuarded(func() {}); err != nil {
		t.Fatalf("正常创建不应报错：%v", err)
	}
	err := createWindowGuarded(func() { panic(errors.New("boom")) })
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error 类型的 panic 也应转成带原因的错误：%v", err)
	}
	err = createWindowGuarded(func() { panic(42) })
	if err == nil || !strings.Contains(err.Error(), "42") {
		t.Fatalf("其它类型的 panic 也应转成带原因的错误：%v", err)
	}
}
