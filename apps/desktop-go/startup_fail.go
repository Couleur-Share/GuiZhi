package main

// 启动失败的可见反馈。对应 Electron 版 index.ts 启动 catch 里的 dialog.showErrorBox：
// GUI 子系统的 GuiZhi.exe 没有控制台，log.Fatalf 只会让进程静默消失，用户无从得知原因也无从恢复。
// 做法：原因总是写入 logs/error.log；交互启动时再弹出系统对话框，并指出备份与日志目录。

import (
	"fmt"
	"log"
	"os"
	"strings"
)

// startupErrorTitle 与 Electron 版的对话框标题一致（中英双语）。
const startupErrorTitle = "Startup Error / 启动错误"

// reportStartupFailure 记录启动失败原因。userData 为空时只打日志、不给出目录指引；
// interactive 为 false（-shot/-bench/-aitest 等自动化实例）时绝不弹模态框，避免无人值守的验证被挂起。
func reportStartupFailure(userData string, err error, interactive bool, show func(title, text string)) {
	log.Printf("启动失败：%v", err)
	if userData == "" {
		if interactive && show != nil {
			show(startupErrorTitle, fmt.Sprintf("归知启动时出错：\n\n%v", err))
		}
		return
	}
	paths := Paths{UserData: userData}
	paths.logAppError("startup", "应用启动失败", err.Error(), nil)
	if !interactive || show == nil {
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "归知启动时出错：\n\n%v\n\n", err)
	fmt.Fprintf(&b, "如果是数据库损坏，可以从自动备份恢复，备份目录：\n%s\n\n", paths.BackupsDir())
	fmt.Fprintf(&b, "详细日志：\n%s", paths.LogsDir())
	show(startupErrorTitle, b.String())
}

// webView2DownloadURL 与 mygo 报错里给出的微软官方常青引导程序地址一致。
const webView2DownloadURL = "https://go.microsoft.com/fwlink/p/?LinkId=2124703"

// createWindowGuarded 执行窗口创建，把 mygo 的 panic 转成带原因的错误。
// mygo 在窗口无法创建时直接 panic（典型：WebView2 运行时缺失，或注册表登记的版本目录已不存在），
// GUI 子系统进程里 panic 的输出无处可去，用户只会看到进程消失，所以必须在这里接住并走 failStartup。
func createWindowGuarded(create func()) (err error) {
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		msg := fmt.Sprint(r)
		if strings.Contains(msg, "WebView2") {
			err = fmt.Errorf("无法创建窗口：%s\n\n这通常是 Microsoft Edge WebView2 运行时缺失或损坏。请重新安装或修复它后再启动归知：\n%s", msg, webView2DownloadURL)
			return
		}
		err = fmt.Errorf("无法创建窗口：%s", msg)
	}()
	create()
	return nil
}

// failStartup 报告启动失败并以非零状态退出（替代 log.Fatalf）。
func failStartup(userData string, err error, interactive bool) {
	reportStartupFailure(userData, err, interactive, showStartupErrorDialog)
	os.Exit(1)
}
