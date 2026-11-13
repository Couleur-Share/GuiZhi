package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveStartupPaths(t *testing.T) {
	appData := t.TempDir()
	exe := filepath.Join(t.TempDir(), "guizhi.exe")
	fixed := func() string { return appData }
	// 显式 -db：原样返回，不做推断
	got := resolveStartupPaths(startupPaths{DB: "x/knowledge.db", Dist: "d"}, exe, true, fixed)
	if got.DB != "x/knowledge.db" || got.UserData != "" || got.Config != "" {
		t.Errorf("显式 db 不应推断其余路径: %+v", got)
	}
	// 全新安装：落到历史默认目录 <appData>/GuiZhi
	got = resolveStartupPaths(startupPaths{}, exe, true, fixed)
	want := filepath.Join(appData, "GuiZhi")
	if got.UserData != want || got.DB != filepath.Join(want, "data", "knowledge.db") || got.Config != filepath.Join(want, "config") || got.AppData != appData {
		t.Errorf("默认路径不符: %+v", got)
	}
	// 用户配置过自定义数据目录：优先
	custom := filepath.Join(t.TempDir(), "mydata")
	if err := os.MkdirAll(filepath.Join(appData, "GuiZhi"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeConfiguredDataPath(appData, custom); err != nil {
		t.Fatal(err)
	}
	got = resolveStartupPaths(startupPaths{}, exe, true, fixed)
	if got.UserData != custom || got.DB != filepath.Join(custom, "data", "knowledge.db") {
		t.Errorf("自定义数据目录应优先: %+v", got)
	}
	// 显式 -userdata 仍可覆盖
	got = resolveStartupPaths(startupPaths{UserData: "U"}, exe, true, fixed)
	if got.UserData != "U" || got.DB != filepath.Join("U", "data", "knowledge.db") {
		t.Errorf("显式 userdata: %+v", got)
	}
}

func TestFindDistDir(t *testing.T) {
	root := t.TempDir()
	exe := filepath.Join(root, "bin", "guizhi.exe")
	if findDistDir(exe) != "" {
		t.Errorf("没有 dist 时应返回空")
	}
	os.MkdirAll(filepath.Join(root, "dist", "renderer"), 0o755)
	if got := findDistDir(exe); got != filepath.Join(root, "dist") {
		t.Errorf("应找到上一级的 dist: %q", got)
	}
	os.MkdirAll(filepath.Join(root, "bin", "dist", "renderer"), 0o755)
	if got := findDistDir(exe); got != filepath.Join(root, "bin", "dist") {
		t.Errorf("exe 旁的 dist 优先: %q", got)
	}
}
