package main

// SQLite 原生库（MyGo 0.9 起是原生模块 Sqlitecore.dll）：随壳分发，放在 GuiZhi.exe 同目录，
// 构建与打包见 scripts/build-shell.ps1、scripts/build-installer.ps1。
// 本文件只做一件事：库缺失时把 MyGo 那句看不出原因的报错换成用户能照着做的说明。
// The SQLite native module ships beside GuiZhi.exe; this file only turns "library missing" into an actionable message.

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/egoist/mygo/ffi"
)

const nativeSQLiteFile = "Sqlitecore.dll"

// explainSQLiteOpenError 在打开数据库失败、且原生库确实不在位时，给错误加上可操作的原因；
// 库在位时原样返回，不掩盖真正的数据库错误。
// 背景：非 mygo build 打包的程序里，MyGo 找不到库会报 “ffi: Sqlitecore has no build for windows-amd64”，
// 用户看不出是文件缺失。常见成因是安全软件把 Sqlitecore.dll 当成未知 DLL 隔离了。
func explainSQLiteOpenError(err error) error {
	if err == nil {
		return nil
	}
	exeDir := ""
	if exe, exeErr := os.Executable(); exeErr == nil {
		exeDir = filepath.Dir(exe)
	}
	if !nativeSQLiteMissing(os.Getenv(ffi.EnvVar("Sqlitecore")), exeDir) {
		return err
	}
	return fmt.Errorf("找不到 SQLite 原生库 %s（应与 GuiZhi.exe 放在同一目录）：安装不完整，或文件被安全软件隔离；"+
		"请重新安装，或从安全软件的隔离区还原它。原始错误：%w", nativeSQLiteFile, err)
}

// nativeSQLiteMissing 判断 MyGo 会去找的两个位置（环境变量指定的文件、可执行文件同目录）里是否都没有库。
// 只覆盖随发布目录分发的场景；开发期 go run/go test 走缓存等其他位置，不经过这里。
func nativeSQLiteMissing(envPath, exeDir string) bool {
	if envPath != "" {
		if info, err := os.Stat(envPath); err == nil && info.Mode().IsRegular() {
			return false
		}
	}
	if exeDir != "" {
		if info, err := os.Stat(filepath.Join(exeDir, nativeSQLiteFile)); err == nil && info.Mode().IsRegular() {
			return false
		}
	}
	return true
}
