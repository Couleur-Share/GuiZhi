package main

// Viva 路径的开机自启（登录项）与「启动即隐藏」判定（台账 B-024）。
//
// 登录项的读写不能走 mygo.App.SetOpenAtLogin / WasOpenedAtLogin：Viva 路径下 mygo 的事件循环不运行，它们会 panic 或永久等待。
// 注册表键与启动命令与旧版（MyGo 壳）完全一致，升级后旧登记原样生效、再保存时自然刷新到当前安装路径。

import (
	"slices"
	"strings"
)

// loginArg 是登录项启动命令带的参数（与 mygo 的 loginArg 相同）：系统登录时拉起 = 带着它启动。
// mygo 包的 init() 在 main() 之前就会把它从 os.Args 摘掉，所以判定要读操作系统的原始命令行（osCommandLineArgs）。
const loginArg = "--mygo-opened-at-login"

// wasOpenedAtLoginFromArgs 报告命令行里是否带有登录项参数（纯函数，参数含或不含 argv[0] 均可）。
func wasOpenedAtLoginFromArgs(args []string) bool { return slices.Contains(args, loginArg) }

// wasOpenedAtLogin 报告本进程是否由系统登录项拉起（读操作系统原始命令行）。
func wasOpenedAtLogin() bool { return wasOpenedAtLoginFromArgs(osCommandLineArgs()) }

// vivaLaunchVisibility 是 Viva 外壳的启动可见性：只有登录项拉起且设置了「启动时最小化」（或带 --hidden）才驻留托盘。
func vivaLaunchVisibility(osArgs []string, minimizeOnLaunch, shouldMaximize bool) launchVisibility {
	return resolveLaunchVisibility(osArgs, wasOpenedAtLoginFromArgs(osArgs), minimizeOnLaunch, shouldMaximize)
}

// loginCommandLine 是写进 Run 键的启动命令：带引号的可执行文件路径加登录项参数。
func loginCommandLine(exePath string) string {
	return `"` + strings.TrimSpace(exePath) + `" ` + loginArg
}
