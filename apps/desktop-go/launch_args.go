package main

// 拉起参数（不是命令行开关）：
//   --updated    NSIS 安装器在更新完成页点「完成」时，经 launchLink 带这个参数拉起新版本
//   --force-run  应用内更新器启动安装包时的参数（偶尔会被原样透传）
//   --hidden     Electron 版写入开机启动项（注册表 Run）的参数，升级后旧登记仍会带着它
// 这三个由 Electron 版沿用下来，Go 标准库 flag 不认识 "-updated" 等会直接 exit(2)；
// 窗口 GUI 子系统下没有任何可见提示——表现为「更新后点完成应用没起来」「开机自启不显示」。
// 所以在 flag.Parse 之前把它们摘掉；--hidden 另由 resolveLaunchVisibility 读取原始 os.Args。
var launchOnlyArgs = map[string]bool{
	"--updated":   true,
	"--force-run": true,
	"--hidden":    true,
}

// stripLaunchArgs 返回去掉拉起参数后的参数列表（不修改入参）。
func stripLaunchArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		if launchOnlyArgs[a] {
			continue
		}
		out = append(out, a)
	}
	return out
}
