package main

// 打包运行时的路径推断：没有 -db 时视为正式启动，其余路径按 Electron 版的规则推断；
// 开发/测试仍可显式传 -dist -db -userdata -config -appdata（各项显式值优先，保持原有用法不变）。

import (
	"os"
	"path/filepath"
)

type startupPaths struct {
	Dist, DB, UserData, AppData, Config string
}

// resolveStartupPaths 对应 index.ts 的 resolveInitialUserDataPath 接线。
// exePath 为可执行文件路径；env 读环境变量（APPDATA）。dist 找不到时返回空串，由调用方决定是否致命。
func resolveStartupPaths(in startupPaths, exePath string, packaged bool, appDataDefault func() string) startupPaths {
	out := in
	if out.DB != "" {
		return out // 显式指定了数据库：沿用旧的开发用法，不做任何推断
	}
	if out.AppData == "" {
		out.AppData = appDataDefault()
	}
	if out.UserData == "" {
		out.UserData = resolveInitialUserDataPath(dataPathOptions{
			AppDataPath:         out.AppData,
			DefaultUserDataPath: historicalDefaultUserDataPath(out.AppData),
			ExePath:             exePath,
			Platform:            "win32",
			IsPackaged:          packaged,
		}, defaultDataPathDeps)
	}
	paths := Paths{UserData: out.UserData}
	out.DB = paths.DatabasePath()
	if out.Config == "" {
		out.Config = paths.ConfigDir()
	}
	return out
}

// findDistDir 在可执行文件旁找渲染产物：<exe>/dist，其次 <exe>/../dist（开发时 exe 在 apps/desktop-go）。
func findDistDir(exePath string) string {
	dir := filepath.Dir(exePath)
	for _, c := range []string{filepath.Join(dir, "dist"), filepath.Join(dir, "..", "dist")} {
		if st, err := os.Stat(filepath.Join(c, "renderer")); err == nil && st.IsDir() {
			return filepath.Clean(c)
		}
	}
	return ""
}

func defaultAppDataDir() string {
	if v := os.Getenv("APPDATA"); v != "" {
		return v
	}
	if d, err := os.UserConfigDir(); err == nil {
		return d
	}
	return ""
}
